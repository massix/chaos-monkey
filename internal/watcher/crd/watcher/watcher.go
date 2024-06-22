package watcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/massix/chaos-monkey/internal/apis/clientset/versioned/scheme"
	cmtyped "github.com/massix/chaos-monkey/internal/apis/clientset/versioned/typed/apis/v1alpha1"
	"github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	common "github.com/massix/chaos-monkey/internal/watcher"
	deploymentfactory "github.com/massix/chaos-monkey/internal/watcher/deployment/factory"
	deploymentwatcher "github.com/massix/chaos-monkey/internal/watcher/deployment/watcher"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
)

type Interface interface {
	common.Watcher
}

type CRDWatcher struct {
	cmtyped.ChaosMonkeyConfigurationInterface
	appsv1.DeploymentInterface

	factory   deploymentfactory.Interface
	namespace string
	running   bool
	bc        record.EventRecorderLogger

	chaosMonkeys map[string]deploymentwatcher.Interface
}

func (c *CRDWatcher) Stop() error {
	c.running = false
	return nil
}

func (c *CRDWatcher) IsRunning() bool {
	return c.running
}

// Start implements Watcher.
func (c *CRDWatcher) Start(ctx context.Context) error {
	var err error
	var wg sync.WaitGroup

	cw, err := c.ChaosMonkeyConfigurationInterface.Watch(ctx, metav1.ListOptions{
		Watch: true,
	})
	if err != nil {
		return err
	}

	c.running = true
	for c.running {
		select {
		case evt := <-cw.ResultChan():
			cmc := evt.Object.(*v1alpha1.ChaosMonkeyConfiguration)

			switch evt.Type {
			case "", watch.Error:
				logrus.Errorf("Received empty or error event from watcher: %+v", evt)
				c.running = false
				err = fmt.Errorf("Received empty or error event from watcher: %+v", evt)

			case watch.Added:
				logrus.Infof("Received ADDED event for %s, for deployment %s", cmc.Name, cmc.Spec.DeploymentName)

				// Check that a deployment with that name exists
				dep, err := c.DeploymentInterface.Get(ctx, cmc.Spec.DeploymentName, metav1.GetOptions{})
				if err != nil {
					logrus.Warnf("Error while trying to get deployment: %s", err)
					continue
				}

				// If we already have a watcher for that deployment, then it is an error!
				if _, ok := c.chaosMonkeys[cmc.Name]; ok {
					logrus.Warnf("Watcher for %s (deployment %s) already exists", cmc.Name, dep.Name)
					continue
				}

				logrus.Infof("Found deployment %s, starting watcher", dep.Name)
				var parsedDuration time.Duration
				if parsedDuration, err = time.ParseDuration(cmc.Spec.Timeout); err != nil {
					logrus.Warnf("Error while parsing timeout: %s", err)
					parsedDuration = time.Duration(30 * time.Second)
				}

				dc := c.factory.New(dep, cmc.Spec.Enabled, cmc.Spec.MinReplicas, cmc.Spec.MaxReplicas, parsedDuration)
				c.chaosMonkeys[cmc.Name] = dc
				wg.Add(1)
				go func() {
					if err := dc.Start(ctx); err != nil {
						logrus.Warnf("Error while starting chaos monkey: %s", err)
					}
					wg.Done()
				}()

				c.bc.Event(cmc, corev1.EventTypeNormal, "Started", fmt.Sprintf("Started chaos monkey %s", cmc.Name))

			case watch.Modified:
				logrus.Infof("Received MODIFIED event for %s, for deployment %s", cmc.Name, cmc.Spec.DeploymentName)
				if w, ok := c.chaosMonkeys[cmc.Name]; ok {
					logrus.Infof("Updating %s, for deployment %s", cmc.Name, cmc.Spec.DeploymentName)
					var parsedDuration time.Duration

					if parsedDuration, err = time.ParseDuration(cmc.Spec.Timeout); err != nil {
						logrus.Warnf("Error while parsing timeout: %s", err)
					} else {
						w.SetTimeout(parsedDuration)
					}

					w.SetMaxReplicas(cmc.Spec.MaxReplicas)
					w.SetMinReplicas(cmc.Spec.MinReplicas)
					w.SetEnabled(cmc.Spec.Enabled)
					logrus.Infof("Enabled=%+v MinReplicas=%d MaxReplicas=%d Timeout=%s", cmc.Spec.Enabled, cmc.Spec.MinReplicas, cmc.Spec.MaxReplicas, parsedDuration)

					c.bc.Eventf(cmc, corev1.EventTypeNormal, "Updated", fmt.Sprintf("Updated chaos monkey %s", cmc.Name))
				} else {
					logrus.Warnf("Chaos Monkey for %s (deployment %s) does not exist", cmc.Name, cmc.Spec.DeploymentName)
				}

			case watch.Deleted:
				logrus.Infof("Received delete event for %s, for deployment %s", cmc.Name, cmc.Spec.DeploymentName)
				if w, ok := c.chaosMonkeys[cmc.Name]; ok {
					logrus.Infof("Removing watcher for %s", cmc.Name)
					_ = w.Stop()
					delete(c.chaosMonkeys, cmc.Name)
				}

				c.bc.Eventf(cmc, corev1.EventTypeNormal, "Deleted", "Deleted chaos monkey %s", cmc.Name)
			}
		case <-ctx.Done():
			logrus.Info("Shutting down CRD watcher")
			c.running = false
		case <-time.After(30 * time.Second):
			logrus.Debug("Garbage collecting Chaos Monkeys")
			for k, v := range c.chaosMonkeys {
				if !v.IsRunning() {
					logrus.Infof("Removing chaos monkey %s", k)
					delete(c.chaosMonkeys, k)
				}
			}
		}
	}

	logrus.Info("Stopping all the chaos monkeys")
	for k, v := range c.chaosMonkeys {
		logrus.Infof("Stopping chaos monkey %s", k)
		_ = v.Stop()
	}

	c.running = false
	logrus.Info("Waiting for all the chaos monkeys to get back home")
	wg.Wait()

	logrus.Info("Shutting down CRD watcher")
	return err
}

func NewCrdWatcher(forNamespace string, factory deploymentfactory.Interface) common.Watcher {
	logrus.Infof("Creating new CRD watcher for %s", forNamespace)
	cfg, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}

	client := kubernetes.NewForConfigOrDie(cfg)
	cmcClient := cmtyped.NewForConfigOrDie(cfg)

	cmcset := cmcClient.ChaosMonkeyConfigurations(forNamespace)
	clientset := client.AppsV1().Deployments(forNamespace)

	bc := record.NewBroadcaster()
	bc.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: client.CoreV1().Events(""),
	})
	recorder := bc.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "chaos-monkey"})

	return &CRDWatcher{
		ChaosMonkeyConfigurationInterface: cmcset,

		DeploymentInterface: clientset,

		factory: factory,

		namespace:    forNamespace,
		running:      false,
		chaosMonkeys: map[string]deploymentwatcher.Interface{},
		bc:           recorder,
	}
}
