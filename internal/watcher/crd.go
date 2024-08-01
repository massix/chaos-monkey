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
	cmv1 "github.com/massix/chaos-monkey/internal/apis/clientset/versioned/typed/apis/v1"
	v1 "github.com/massix/chaos-monkey/internal/apis/v1"
	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	apiappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

type WatcherConfiguration struct {
	Configuration *v1.ChaosMonkeyConfiguration
	Watcher       ConfigurableWatcher
}

type CrdWatcher struct {
	appsv1.DeploymentInterface
	record.EventRecorderLogger

	V1                 cmv1.ChaosMonkeyConfigurationInterface
	Logrus             logrus.FieldLogger
	Client             kubernetes.Interface
	MetricsClient      metricsv.Interface
	metrics            *crdMetrics
	Mutex              *sync.Mutex
	DeploymentWatchers map[string]*WatcherConfiguration
	ForceStopChan      chan interface{}
	Namespace          string
	CleanupTimeout     time.Duration
	WatcherTimeout     time.Duration
	Running            bool
}

type watchInterface interface {
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

type crdMetrics struct {
	// Total number of events handled
	addedEvents    prometheus.Counter
	modifiedEvents prometheus.Counter
	deletedEvents  prometheus.Counter

	// Total number of restarts
	restarts prometheus.Counter

	// Metrics for PodWatchers
	pwSpawned prometheus.Counter
	pwActive  prometheus.Gauge

	// Metrics for DeploymentWatchers
	dwSpawned prometheus.Counter
	dwActive  prometheus.Gauge

	// Metrics for AntiHPAWatchers
	ahSpawned prometheus.Counter
	ahActive  prometheus.Gauge

	// How long it takes to handle an event
	eventDuration prometheus.Histogram
}

func (crd *crdMetrics) unregister() {
	prometheus.Unregister(crd.addedEvents)
	prometheus.Unregister(crd.modifiedEvents)
	prometheus.Unregister(crd.deletedEvents)
	prometheus.Unregister(crd.restarts)
	prometheus.Unregister(crd.pwSpawned)
	prometheus.Unregister(crd.pwActive)
	prometheus.Unregister(crd.dwSpawned)
	prometheus.Unregister(crd.dwActive)
	prometheus.Unregister(crd.ahSpawned)
	prometheus.Unregister(crd.ahActive)
	prometheus.Unregister(crd.eventDuration)
}

var _ = (Watcher)((*CrdWatcher)(nil))

func newCrdMetrics(namespace string) *crdMetrics {
	return &crdMetrics{
		addedEvents: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "events",
			Subsystem:   "crdwatcher",
			Help:        "Total number of events handled",
			ConstLabels: map[string]string{"namespace": namespace, "event_type": "add"},
		}),
		modifiedEvents: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "events",
			Subsystem:   "crdwatcher",
			Help:        "Total number of events handled",
			ConstLabels: map[string]string{"namespace": namespace, "event_type": "modify"},
		}),
		deletedEvents: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "events",
			Subsystem:   "crdwatcher",
			Help:        "Total number of events handled",
			ConstLabels: map[string]string{"namespace": namespace, "event_type": "delete"},
		}),

		restarts: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "restarts",
			Subsystem:   "crdwatcher",
			Help:        "Total number of restarts",
			ConstLabels: map[string]string{"namespace": namespace},
		}),

		pwSpawned: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "pw_spawned",
			Subsystem:   "crdwatcher",
			Help:        "Total number of PodWatchers spawned",
			ConstLabels: map[string]string{"namespace": namespace},
		}),
		pwActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace:   "chaos_monkey",
			Name:        "pw_active",
			Subsystem:   "crdwatcher",
			Help:        "Total number of PodWatchers active",
			ConstLabels: map[string]string{"namespace": namespace},
		}),

		dwSpawned: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "dw_spawned",
			Subsystem:   "crdwatcher",
			Help:        "Total number of DeploymentWatchers spawned",
			ConstLabels: map[string]string{"namespace": namespace},
		}),
		dwActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace:   "chaos_monkey",
			Name:        "dw_active",
			Subsystem:   "crdwatcher",
			Help:        "Total number of DeploymentWatchers active",
			ConstLabels: map[string]string{"namespace": namespace},
		}),

		ahSpawned: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "ah_spawned",
			Subsystem:   "crdwatcher",
			Help:        "Total number of AntiHPAWatchers active",
			ConstLabels: map[string]string{"namespace": namespace},
		}),
		ahActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace:   "chaos_monkey",
			Name:        "ah_active",
			Subsystem:   "crdwatcher",
			Help:        "Total number of AntiHPAWatchers active",
			ConstLabels: map[string]string{"namespace": namespace},
		}),

		eventDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace:   "chaos_monkey",
			Name:        "event_duration",
			Subsystem:   "crdwatcher",
			Help:        "How long it took to handle an event (calculated in microseconds)",
			ConstLabels: map[string]string{"namespace": namespace},
			Buckets:     []float64{0, 5, 10, 20, 50, 100, 500, 1000, 1500, 2000},
		}),
	}
}

func NewCrdWatcher(clientset kubernetes.Interface, cmcClientset typedcmc.Interface, metricsClient metricsv.Interface, recorder record.EventRecorderLogger, namespace string) *CrdWatcher {
	// Build my own recorder here
	if recorder == nil {
		logrus.Debug("No recorder provided, using default")
		broadcaster := record.NewBroadcaster()
		broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events(namespace)})
		recorder = broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "chaos-monkey"})
	}

	conf := configuration.FromEnvironment()

	return &CrdWatcher{
		V1:                  cmcClientset.ChaosMonkeyConfigurationV1().ChaosMonkeyConfigurations(namespace),
		DeploymentInterface: clientset.AppsV1().Deployments(namespace),
		EventRecorderLogger: recorder,

		Logrus:             logrus.WithFields(logrus.Fields{"component": "CRDWatcher", "namespace": namespace}),
		Client:             clientset,
		MetricsClient:      metricsClient,
		Mutex:              &sync.Mutex{},
		DeploymentWatchers: map[string]*WatcherConfiguration{},
		ForceStopChan:      make(chan interface{}),
		Namespace:          namespace,
		CleanupTimeout:     15 * time.Minute,
		WatcherTimeout:     conf.Timeouts.Crd,
		Running:            false,
		metrics:            newCrdMetrics(namespace),
	}
}

// Close implements io.Closer
func (c *CrdWatcher) Close() error {
	c.metrics.unregister()
	return nil
}

// IsRunning implements Watcher.
func (c *CrdWatcher) IsRunning() bool {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	return c.Running
}

// If this function returns an error, it means that we cannot recover
func (c *CrdWatcher) handleEvent(ctx context.Context, evt watch.Event, wg *sync.WaitGroup) error {
	var cmc *v1.ChaosMonkeyConfiguration

	switch object := evt.Object.(type) {
	case *v1.ChaosMonkeyConfiguration:
		cmc = object
	default:
		return fmt.Errorf("unknown object type %T", evt.Object)
	}

	startTime := time.Now().UnixMicro()
	defer func() {
		endTime := time.Now().UnixMicro()
		c.metrics.eventDuration.Observe(float64(endTime - startTime))
	}()

	c.Logrus.Debugf("Received %s event for %+v", evt.Type, cmc)

	switch evt.Type {
	case "", watch.Error:
		c.Logrus.Errorf("Received empty error or event from CRD watcher: %+v", evt)
		c.setRunning(false)
		return errors.New("Empty event or error from CRD watcher")

	case watch.Added:
		c.Logrus.Infof("Received ADDED event for %s, for deployment %s", cmc.Name, cmc.Spec.Deployment.Name)

		// Check if the target deployment exists
		dep, err := c.DeploymentInterface.Get(ctx, cmc.Spec.Deployment.Name, metav1.GetOptions{})
		if err != nil {
			c.Logrus.Errorf("Error while trying to get deployment: %s", err)

			// Recoverable error
			return nil
		}

		c.Logrus.Infof("Adding watcher for deployment %s", dep.Name)

		// Add a new watcher
		if err = c.addWatcher(cmc, dep); err != nil {
			c.Logrus.Errorf("Error while trying to add watcher: %s", err)

			// Recoverable error
			return nil
		}

		// Start it
		if err := c.startWatcher(ctx, dep.Name, wg); err != nil {
			c.Logrus.Errorf("Error while trying to start watcher: %s", err)
		}

		c.Logrus.Debug("All is good! Publishing event.")
		c.EventRecorderLogger.Eventf(cmc, "Normal", "Started", "Watcher started for deployment %s", dep.Name)
		c.metrics.addedEvents.Inc()

	case watch.Modified:
		c.Logrus.Infof("Received MODIFIED event for %s, for deployment %s", cmc.Name, cmc.Spec.Deployment.Name)

		if err := c.modifyWatcher(ctx, cmc, wg); err != nil {
			c.Logrus.Errorf("Error while trying to modify watcher: %s", err)
		}

		c.Logrus.Debug("All is good! Publishing event.")
		c.EventRecorderLogger.Eventf(cmc, "Normal", "Modified", "Watcher modified for deployment %s", cmc.Spec.Deployment.Name)
		c.metrics.modifiedEvents.Inc()

	case watch.Deleted:
		c.Logrus.Infof("Received DELETED event for %s, for deployment %s", cmc.Name, cmc.Spec.Deployment.Name)

		if err := c.deleteWatcher(cmc); err != nil {
			c.Logrus.Errorf("Error while trying to delete watcher: %s", err)
		}

		c.Logrus.Debug("All is good! Publishing event.")
		c.EventRecorderLogger.Eventf(cmc, "Normal", "Deleted", "Watcher deleted for deployment %s", cmc.Spec.Deployment.Name)
		c.metrics.deletedEvents.Inc()
	}

	return nil
}

// Start implements Watcher.
func (c *CrdWatcher) Start(ctx context.Context) error {
	defer c.Close()
	c.Logrus.Info("Starting CRD watcher")
	var err error
	var wg sync.WaitGroup

	watchTimeout := int64(c.WatcherTimeout.Seconds())
	wv1, err := c.V1.Watch(ctx, metav1.ListOptions{
		Watch:          true,
		TimeoutSeconds: &watchTimeout,
	})
	if err != nil {
		return err
	}

	c.setRunning(true)

	for c.IsRunning() {
		select {
		case evt, ok := <-wv1.ResultChan():
			if !ok {
				c.Logrus.Warn("Watch timed out")
				wv1, err = c.restartWatch(ctx, c.V1, &wg)
				if err != nil {
					c.Logrus.Errorf("Error while restarting watcher: %s", err)
					c.setRunning(false)
				}

				c.metrics.restarts.Inc()
				break
			}

			if err := c.handleEvent(ctx, evt, &wg); err != nil {
				c.Logrus.Error(err)
				return err
			}
		case <-ctx.Done():
			c.Logrus.Info("Watcher context done")
			c.setRunning(false)

		case <-time.After(c.CleanupTimeout):
			c.Logrus.Debug("Garbage collecting Chaos Monkeys")
			c.cleanUp()

		case <-c.ForceStopChan:
			// This is here just to wake up early from the loop
			c.Logrus.Info("Force stopping CRD Watcher")
			c.setRunning(false)
		}
	}

	c.Logrus.Info("Watcher stopped, waiting for monkeys to get back home")

	// Stop all the remaining watchers
	c.Mutex.Lock()
	for dep, wc := range c.DeploymentWatchers {
		c.Logrus.Infof("Stopping watcher for deployment %s", dep)
		if err := wc.Watcher.Stop(); err != nil {
			c.Logrus.Warnf("Error while stopping watcher: %s", err)
		}
		delete(c.DeploymentWatchers, dep)
	}
	c.Mutex.Unlock()

	wg.Wait()

	c.Logrus.Debug("Unregistering Prometheus metrics")
	return err
}

// Stop implements Watcher.
func (c *CrdWatcher) Stop() error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	c.Running = false

	c.Logrus.Debug("Stopping CRD watcher")
	c.Logrus.Debug("Force stopping")

	select {
	case c.ForceStopChan <- nil:
	default:
		c.Logrus.Warn("Could not write to ForceStopChannel")
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

func (c *CrdWatcher) addWatcher(cmc *v1.ChaosMonkeyConfiguration, dep *apiappsv1.Deployment) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	// If we already have a watcher for that deployment, then it is an error!
	if _, ok := c.DeploymentWatchers[dep.Name]; ok {
		return errors.New("Watcher for " + dep.Name + " already exists")
	}

	var newWatcher ConfigurableWatcher
	var combinedLabelSelector []string
	for label, value := range dep.Spec.Selector.MatchLabels {
		combinedLabelSelector = append(combinedLabelSelector, fmt.Sprintf("%s=%s", label, value))
	}

	switch cmc.Spec.ScalingMode {
	case v1.ScalingModeKillPod:
		c.Logrus.Debug("Creating new pod watcher")
		if dep.Spec.Selector == nil || len(dep.Spec.Selector.MatchLabels) == 0 {
			return fmt.Errorf("No selector labels found for deployment %s", dep.Name)
		}

		c.Logrus.Debugf("Configuring watcher with %+v", cmc.Spec)
		newWatcher = DefaultPodFactory(c.Client, nil, dep.Namespace, strings.Join(combinedLabelSelector, ","))
		c.metrics.pwSpawned.Inc()
	case v1.ScalingModeRandomScale:
		c.Logrus.Debug("Creating new deployment watcher")
		newWatcher = DefaultDeploymentFactory(c.Client, nil, dep)
		c.metrics.dwSpawned.Inc()
	case v1.ScalingModeAntiPressure:
		c.Logrus.Debug("Creating new AntiHPDA watcher")
		c.metrics.ahSpawned.Inc()
		newWatcher = DefaultAntiHPAFactory(c.MetricsClient, c.Client.CoreV1().Pods(cmc.Namespace), dep.Namespace, strings.Join(combinedLabelSelector, ","))
	default:
		return fmt.Errorf("Unhandled scaling mode: %s", cmc.Spec.ScalingMode)
	}

	// Configure it
	c.Logrus.Debugf("Configuring watcher with %+v", cmc.Spec)
	newWatcher.SetEnabled(cmc.Spec.Enabled)
	newWatcher.SetMinReplicas(cmc.Spec.MinReplicas)
	newWatcher.SetMaxReplicas(cmc.Spec.MaxReplicas)
	newWatcher.SetTimeout(cmc.Spec.Timeout)

	c.Logrus.Debug("Adding watcher to map")
	c.DeploymentWatchers[dep.Name] = &WatcherConfiguration{
		Configuration: cmc,
		Watcher:       newWatcher,
	}

	return nil
}

func (c *CrdWatcher) startWatcher(ctx context.Context, forDeployment string, wg *sync.WaitGroup) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	wc, ok := c.DeploymentWatchers[forDeployment]
	if !ok {
		return fmt.Errorf("Watcher for deployment %s does not exist", forDeployment)
	}

	var activeMetric prometheus.Gauge

	switch wc.Watcher.(type) {
	case *PodWatcher:
		activeMetric = c.metrics.pwActive
	case *DeploymentWatcher:
		activeMetric = c.metrics.dwActive
	case *AntiHPAWatcher:
		activeMetric = c.metrics.ahActive
	}

	if activeMetric != nil {
		activeMetric.Inc()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		c.Logrus.Debugf("Starting watcher for %s", forDeployment)
		if err := wc.Watcher.Start(ctx); err != nil {
			c.Logrus.Errorf("Error while starting watcher: %s", err)
		}

		if activeMetric != nil {
			activeMetric.Dec()
		}
	}()

	return nil
}

func (c *CrdWatcher) modifyWatcher(ctx context.Context, cmc *v1.ChaosMonkeyConfiguration, wg *sync.WaitGroup) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	wc, ok := c.DeploymentWatchers[cmc.Spec.Deployment.Name]
	if !ok {
		return fmt.Errorf("Watcher for deployment %s does not exist", cmc.Spec.Deployment.Name)
	}

	c.Logrus.Debugf("Reconfiguring watcher with %+v", cmc.Spec)

	if wc.Configuration.Spec.ScalingMode != cmc.Spec.ScalingMode {
		c.Logrus.Infof("CMC %s changed its pod mode, recreating the watcher from scratch", cmc.Name)

		c.Logrus.Debugf("Stopping watcher %s", cmc.Name)
		if err := wc.Watcher.Stop(); err != nil {
			return err
		}

		delete(c.DeploymentWatchers, cmc.Spec.Deployment.Name)

		// Get the deployment
		dep, err := c.DeploymentInterface.Get(context.Background(), cmc.Spec.Deployment.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		allLabels := []string{}
		for key, val := range dep.Spec.Selector.MatchLabels {
			allLabels = append(allLabels, fmt.Sprintf("%s=%s", key, val))
		}

		var newWatcher ConfigurableWatcher
		switch cmc.Spec.ScalingMode {
		case v1.ScalingModeKillPod:
			c.Logrus.Debug("Creating new Pod watcher")
			newWatcher = DefaultPodFactory(c.Client, nil, dep.Namespace, allLabels...)
			c.metrics.pwSpawned.Inc()
		case v1.ScalingModeRandomScale:
			c.Logrus.Debug("Creating new Deployment watcher")
			newWatcher = DefaultDeploymentFactory(c.Client, nil, dep)
			c.metrics.dwSpawned.Inc()
		case v1.ScalingModeAntiPressure:
			c.Logrus.Debug("Creating new AntiHPA watcher")
			newWatcher = DefaultAntiHPAFactory(c.MetricsClient, c.Client.CoreV1().Pods(cmc.Namespace), dep.Namespace, strings.Join(allLabels, ","))
			c.metrics.ahSpawned.Inc()
		default:
			return fmt.Errorf("Unhandled scaling mode: %s", cmc.Spec.ScalingMode)
		}

		// Configure the watcher
		newWatcher.SetEnabled(cmc.Spec.Enabled)
		newWatcher.SetMinReplicas(cmc.Spec.MinReplicas)
		newWatcher.SetMaxReplicas(cmc.Spec.MaxReplicas)
		newWatcher.SetTimeout(cmc.Spec.Timeout)

		// Start the watcher
		c.Logrus.Info("Starting the newly created watcher")

		var activeMetric prometheus.Gauge
		switch newWatcher.(type) {
		case *PodWatcher:
			activeMetric = c.metrics.pwActive
		case *DeploymentWatcher:
			activeMetric = c.metrics.dwActive
		case *AntiHPAWatcher:
			activeMetric = c.metrics.ahActive
		}

		if activeMetric != nil {
			activeMetric.Inc()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Logrus.Debugf("Starting watcher for %s", cmc.Spec.Deployment.Name)
			if err := newWatcher.Start(ctx); err != nil {
				c.Logrus.Errorf("Error while starting watcher: %s", err)
			}

			if activeMetric != nil {
				activeMetric.Dec()
			}
		}()

		// Put it into the map
		c.DeploymentWatchers[cmc.Spec.Deployment.Name] = &WatcherConfiguration{
			Configuration: cmc,
			Watcher:       newWatcher,
		}
	} else {
		c.Logrus.Debug("Not recreating a new watcher")
		wc.Watcher.SetEnabled(cmc.Spec.Enabled)
		wc.Watcher.SetMinReplicas(cmc.Spec.MinReplicas)
		wc.Watcher.SetMaxReplicas(cmc.Spec.MaxReplicas)
		wc.Watcher.SetTimeout(cmc.Spec.Timeout)
	}

	return nil
}

func (c *CrdWatcher) deleteWatcher(cmc *v1.ChaosMonkeyConfiguration) error {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	c.Logrus.Infof("Deleting watcher for %s", cmc.Spec.Deployment.Name)

	if wc, ok := c.DeploymentWatchers[cmc.Spec.Deployment.Name]; ok {
		if err := wc.Watcher.Stop(); err != nil {
			c.Logrus.Warnf("Error while stopping watcher: %s", err)
		}
		delete(c.DeploymentWatchers, cmc.Spec.Deployment.Name)
	} else {
		return fmt.Errorf("Watcher for deployment %s does not exist", cmc.Spec.Deployment.Name)
	}

	return nil
}

func (c *CrdWatcher) cleanUp() {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	for name, wc := range c.DeploymentWatchers {
		if !wc.Watcher.IsRunning() {
			c.Logrus.Infof("Removing watcher for %s", name)
			delete(c.DeploymentWatchers, name)
		}
	}
}

func (c *CrdWatcher) restartWatch(ctx context.Context, wi watchInterface, wg *sync.WaitGroup) (watch.Interface, error) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	c.Logrus.Info("Restarting CRD Watcher")

	c.Logrus.Debug("Cleaning existing watchers")
	for key, wc := range c.DeploymentWatchers {
		c.Logrus.Debugf("Stopping watcher for %s", key)
		if err := wc.Watcher.Stop(); err != nil {
			c.Logrus.Warnf("Error while stopping watcher for %s: %s", key, err)
		}

		delete(c.DeploymentWatchers, key)
	}

	c.Logrus.Info("Waiting for monkeys to get back home")
	wg.Wait()

	timeoutSeconds := int64(c.WatcherTimeout.Seconds())
	return wi.Watch(ctx, metav1.ListOptions{
		Watch:          true,
		TimeoutSeconds: &timeoutSeconds,
	})
}
