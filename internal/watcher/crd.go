package watcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	typedcmc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/massix/chaos-monkey/internal/apis/clientset/versioned/scheme"
	cmv1alpha1 "github.com/massix/chaos-monkey/internal/apis/clientset/versioned/typed/apis/v1alpha1"
	"github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	"github.com/sirupsen/logrus"
	apiappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

type CrdWatcher struct {
	cmv1alpha1.ChaosMonkeyConfigurationInterface
	appsv1.DeploymentInterface
	record.EventRecorderLogger

	Client             kubernetes.Interface
	Mutex              *sync.Mutex
	DeploymentWatchers map[string]ConfigurableWatcher
	ForceStopChan      chan interface{}
	Namespace          string
	CleanupTimeout     time.Duration
	WatcherTimeout     time.Duration
	Running            bool
}

var _ = (Watcher)((*CrdWatcher)(nil))

func NewCrdWatcher(clientset kubernetes.Interface, cmcClientset typedcmc.Interface, recorder record.EventRecorderLogger, namespace string) Watcher {
	// Build my own recorder here
	if recorder == nil {
		logrus.Debug("No recorder provided, using default")
		broadcaster := record.NewBroadcaster()
		broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events(namespace)})
		recorder = broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "chaos-monkey"})
	}

	return &CrdWatcher{
		ChaosMonkeyConfigurationInterface: cmcClientset.ChaosMonkeyConfigurationV1alpha1().ChaosMonkeyConfigurations(namespace),
		DeploymentInterface:               clientset.AppsV1().Deployments(namespace),
		EventRecorderLogger:               recorder,

		Client:             clientset,
		Mutex:              &sync.Mutex{},
		DeploymentWatchers: map[string]ConfigurableWatcher{},
		ForceStopChan:      make(chan interface{}),
		Namespace:          namespace,
		CleanupTimeout:     15 * time.Minute,
		WatcherTimeout:     24 * time.Hour,
		Running:            false,
	}
}

// IsRunning implements Watcher.
func (c *CrdWatcher) IsRunning() bool {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	return c.Running
}

// Start implements Watcher.
func (c *CrdWatcher) Start(ctx context.Context) error {
	logrus.Infof("Starting CRD watcher in namespace %s", c.Namespace)
	var err error
	var wg sync.WaitGroup

	watchTimeout := int64(c.WatcherTimeout.Seconds())
	w, err := c.ChaosMonkeyConfigurationInterface.Watch(ctx, metav1.ListOptions{
		Watch:          true,
		TimeoutSeconds: &watchTimeout,
	})
	if err != nil {
		return err
	}

	defer w.Stop()

	c.setRunning(true)

	for c.IsRunning() {
		select {
		case evt, ok := <-w.ResultChan():
			if !ok {
				logrus.Warnf("Watch for %s timed out", c.Namespace)
				w, err = c.restartWatch(ctx, &wg)
				if err != nil {
					logrus.Errorf("Error while restarting watchers: %s", err)
					c.setRunning(false)
				}

				break
			}

			cmc := evt.Object.(*v1alpha1.ChaosMonkeyConfiguration)

			switch evt.Type {
			case "", watch.Error:
				logrus.Errorf("Received empty error or event from CRD watcher: %+v", evt)
				c.setRunning(false)
				err = errors.New("Empty event or error from CRD watcher")

			case watch.Added:
				logrus.Infof("Received ADDED event for %s, for deployment %s", cmc.Name, cmc.Spec.DeploymentName)

				// Check if the target deployment exists
				dep, err := c.DeploymentInterface.Get(ctx, cmc.Spec.DeploymentName, metav1.GetOptions{})
				if err != nil {
					logrus.Errorf("Error while trying to get deployment: %s", err)
					continue
				}

				logrus.Infof("Adding watcher for deployment %s", dep.Name)

				// Add a new watcher
				if err = c.addWatcher(cmc, dep); err != nil {
					logrus.Errorf("Error while trying to add watcher: %s", err)
					continue
				}

				// Start it
				if err := c.startWatcher(ctx, dep.Name, &wg); err != nil {
					logrus.Errorf("Error while trying to start watcher: %s", err)
				}

				logrus.Debug("All is good! Publishing event.")
				c.EventRecorderLogger.Eventf(cmc, "Normal", "Started", "Watcher started for deployment %s", dep.Name)

			case watch.Modified:
				logrus.Infof("Received MODIFIED event for %s, for deployment %s", cmc.Name, cmc.Spec.DeploymentName)

				if err := c.modifyWatcher(cmc); err != nil {
					logrus.Errorf("Error while trying to modify watcher: %s", err)
				}

				logrus.Debug("All is good! Publishing event.")
				c.EventRecorderLogger.Eventf(cmc, "Normal", "Modified", "Watcher modified for deployment %s", cmc.Spec.DeploymentName)

			case watch.Deleted:
				logrus.Infof("Received DELETED event for %s, for deployment %s", cmc.Name, cmc.Spec.DeploymentName)

				if err := c.deleteWatcher(cmc); err != nil {
					logrus.Errorf("Error while trying to delete watcher: %s", err)
				}

				logrus.Debug("All is good! Publishing event.")
				c.EventRecorderLogger.Eventf(cmc, "Normal", "Deleted", "Watcher deleted for deployment %s", cmc.Spec.DeploymentName)
			}

		case <-ctx.Done():
			logrus.Infof("Watcher context done")
			c.setRunning(false)

		case <-time.After(c.CleanupTimeout):
			logrus.Debug("Garbage collecting Chaos Monkeys")
			c.cleanUp()

		case <-c.ForceStopChan:
			// This is here just to wake up early from the loop
			logrus.Info("Force stopping CRD Watcher")
			c.setRunning(false)
		}
	}

	logrus.Infof("Watcher stopped, waiting for monkeys to get back home")

	// Stop all the remaining watchers
	c.Mutex.Lock()
	for dep, watcher := range c.DeploymentWatchers {
		logrus.Infof("Stopping watcher for deployment %s", dep)
		if err := watcher.Stop(); err != nil {
			logrus.Warnf("Error while stopping watcher: %s", err)
		}
		delete(c.DeploymentWatchers, dep)
	}
	c.Mutex.Unlock()

	wg.Wait()
	return err
}

// Stop implements Watcher.
func (c *CrdWatcher) Stop() error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	c.Running = false

	logrus.Debugf("Stopping CRD watcher for %s", c.Namespace)
	logrus.Debug("Force stopping")

	select {
	case c.ForceStopChan <- nil:
	default:
		logrus.Warn("Could not write to ForceStopChannel")
	}

	close(c.ForceStopChan)
	return nil
}

// Internal methods
func (c *CrdWatcher) setRunning(v bool) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	c.Running = v
}

func (c *CrdWatcher) addWatcher(cmc *v1alpha1.ChaosMonkeyConfiguration, dep *apiappsv1.Deployment) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	// If we already have a watcher for that deployment, then it is an error!
	if _, ok := c.DeploymentWatchers[dep.Name]; ok {
		return errors.New("Watcher for " + dep.Name + " already exists")
	}

	parsedDuration, err := time.ParseDuration(cmc.Spec.Timeout)
	if err != nil {
		logrus.Warnf("Error while parsing timeout: %s, defaulting to 5 minutes", err)
		parsedDuration = time.Duration(5 * time.Minute)
	}

	var newWatcher ConfigurableWatcher

	if cmc.Spec.PodMode {
		logrus.Debug("Creating new pod watcher")
		if dep.Spec.Selector == nil || len(dep.Spec.Selector.MatchLabels) == 0 {
			return fmt.Errorf("No selector labels found for deployment %s", dep.Name)
		}

		var combinedLabelSelector []string
		for label, value := range dep.Spec.Selector.MatchLabels {
			combinedLabelSelector = append(combinedLabelSelector, fmt.Sprintf("%s=%s", label, value))
		}

		logrus.Debugf("Configuring watcher with %+v", cmc.Spec)
		newWatcher = DefaultPodFactory(c.Client, nil, dep.Namespace, strings.Join(combinedLabelSelector, ","))
	} else {
		logrus.Debug("Creating new deployment watcher")
		newWatcher = DefaultDeploymentFactory(c.Client, nil, dep)
	}

	// Configure it
	logrus.Debugf("Configuring watcher with %+v", cmc.Spec)
	newWatcher.SetEnabled(cmc.Spec.Enabled)
	newWatcher.SetMinReplicas(cmc.Spec.MinReplicas)
	newWatcher.SetMaxReplicas(cmc.Spec.MaxReplicas)
	newWatcher.SetTimeout(parsedDuration)

	logrus.Debug("Adding watcher to map")
	c.DeploymentWatchers[dep.Name] = newWatcher

	return nil
}

func (c *CrdWatcher) startWatcher(ctx context.Context, forDeployment string, wg *sync.WaitGroup) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	watcher, ok := c.DeploymentWatchers[forDeployment]
	if !ok {
		return fmt.Errorf("Watcher for deployment %s does not exist", forDeployment)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		logrus.Debugf("Starting watcher for %s", forDeployment)
		if err := watcher.Start(ctx); err != nil {
			logrus.Errorf("Error while starting watcher: %s", err)
		}
	}()

	return nil
}

func (c *CrdWatcher) modifyWatcher(cmc *v1alpha1.ChaosMonkeyConfiguration) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	watcher, ok := c.DeploymentWatchers[cmc.Spec.DeploymentName]
	if !ok {
		return fmt.Errorf("Watcher for deployment %s does not exist", cmc.Spec.DeploymentName)
	}

	logrus.Debugf("Reconfiguring watcher with %+v", cmc.Spec)
	watcher.SetEnabled(cmc.Spec.Enabled)
	watcher.SetMinReplicas(cmc.Spec.MinReplicas)
	watcher.SetMaxReplicas(cmc.Spec.MaxReplicas)

	parsedDuration, err := time.ParseDuration(cmc.Spec.Timeout)
	if err != nil {
		logrus.Warnf("Error while parsing timeout: %s, not modifying it", err)
	} else {
		watcher.SetTimeout(parsedDuration)
	}

	return nil
}

func (c *CrdWatcher) deleteWatcher(cmc *v1alpha1.ChaosMonkeyConfiguration) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	logrus.Infof("Deleting watcher for %s", cmc.Spec.DeploymentName)

	if watcher, ok := c.DeploymentWatchers[cmc.Spec.DeploymentName]; ok {
		if err := watcher.Stop(); err != nil {
			logrus.Warnf("Error while stopping watcher: %s", err)
		}
		delete(c.DeploymentWatchers, cmc.Spec.DeploymentName)
	} else {
		return fmt.Errorf("Watcher for deployment %s does not exist", cmc.Spec.DeploymentName)
	}

	return nil
}

func (c *CrdWatcher) cleanUp() {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	for name, watcher := range c.DeploymentWatchers {
		if !watcher.IsRunning() {
			logrus.Infof("Removing watcher for %s", name)
			delete(c.DeploymentWatchers, name)
		}
	}
}

func (c *CrdWatcher) restartWatch(ctx context.Context, wg *sync.WaitGroup) (watch.Interface, error) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	logrus.Infof("Restarting CRD Watcher for %s", c.Namespace)

	logrus.Debug("Cleaning existing watchers")
	for key, w := range c.DeploymentWatchers {
		logrus.Debugf("Stopping watcher for %s", key)
		if err := w.Stop(); err != nil {
			logrus.Warnf("Error while stopping watcher for %s: %s", key, err)
		}

		delete(c.DeploymentWatchers, key)
	}

	logrus.Info("Waiting for monkeys to get back home")
	wg.Wait()

	timeoutSeconds := int64(c.WatcherTimeout.Seconds())
	return c.ChaosMonkeyConfigurationInterface.Watch(ctx, metav1.ListOptions{
		Watch:          true,
		TimeoutSeconds: &timeoutSeconds,
	})
}
