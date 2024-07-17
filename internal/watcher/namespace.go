package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	mc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

type NamespaceWatcher struct {
	typedcorev1.NamespaceInterface
	record.EventRecorderLogger

	Logrus         logrus.FieldLogger
	Client         kubernetes.Interface
	CmcClient      mc.Interface
	Mutex          *sync.Mutex
	CrdWatchers    map[string]Watcher
	metrics        *nwMetrics
	RootNamespace  string
	Behavior       configuration.Behavior
	CleanupTimeout time.Duration
	WatcherTimeout time.Duration
	Running        bool
}

// Close implements Watcher.
func (n *NamespaceWatcher) Close() error {
	n.metrics.unregister()
	return nil
}

// Metrics for the NamespaceWatcher component
type nwMetrics struct {
	// Total number of events handled
	addedEvents    prometheus.Counter
	modifiedEvents prometheus.Counter
	deletedEvents  prometheus.Counter

	// Total number of restarts handled
	restarts prometheus.Counter

	// Total number of CMCs spawned
	cmcSpawned prometheus.Counter

	// Total number of *active* CMCs
	cmcActive prometheus.Gauge

	// How long it took to handle an event
	eventDuration prometheus.Histogram
}

func (nw *nwMetrics) unregister() {
	prometheus.Unregister(nw.addedEvents)
	prometheus.Unregister(nw.modifiedEvents)
	prometheus.Unregister(nw.deletedEvents)
	prometheus.Unregister(nw.restarts)
	prometheus.Unregister(nw.cmcSpawned)
	prometheus.Unregister(nw.cmcActive)
	prometheus.Unregister(nw.eventDuration)
}

var _ = (Watcher)((*NamespaceWatcher)(nil))

func newNwMetrics(rootNamespace, behavior string) *nwMetrics {
	return &nwMetrics{
		addedEvents: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "events",
			Subsystem:   "nswatcher",
			Help:        "Total number of events handled",
			ConstLabels: map[string]string{"event_type": "add", "root_namespace": rootNamespace, "behavior": behavior},
		}),
		modifiedEvents: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "events",
			Subsystem:   "nswatcher",
			Help:        "Total number of events handled",
			ConstLabels: map[string]string{"event_type": "modify", "root_namespace": rootNamespace, "behavior": behavior},
		}),
		deletedEvents: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "events",
			Subsystem:   "nswatcher",
			Help:        "Total number of events handled",
			ConstLabels: map[string]string{"event_type": "delete", "root_namespace": rootNamespace, "behavior": behavior},
		}),

		restarts: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "restarts",
			Subsystem:   "nswatcher",
			Help:        "Total number of restarts handled",
			ConstLabels: map[string]string{"root_namespace": rootNamespace, "behavior": behavior},
		}),

		cmcSpawned: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Name:        "cmc_spawned",
			Subsystem:   "nswatcher",
			Help:        "Total number of CMCs spawned",
			ConstLabels: map[string]string{"root_namespace": rootNamespace, "behavior": behavior},
		}),

		cmcActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace:   "chaos_monkey",
			Name:        "cmc_active",
			Subsystem:   "nswatcher",
			Help:        "Current active CMC Watchers",
			ConstLabels: map[string]string{"root_namespace": rootNamespace, "behavior": behavior},
		}),

		eventDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace:   "chaos_monkey",
			Name:        "event_duration",
			Subsystem:   "nswatcher",
			Help:        "How long it took to handle an event (calculated in microseconds)",
			ConstLabels: map[string]string{"root_namespace": rootNamespace, "behavior": behavior},
			Buckets:     []float64{0, 5, 10, 20, 50, 100, 500, 1000, 1500, 2000},
		}),
	}
}

func NewNamespaceWatcher(clientset kubernetes.Interface, cmcClientset mc.Interface, recorder record.EventRecorderLogger, rootNamespace string, behavior configuration.Behavior) *NamespaceWatcher {
	logrus.Infof("Creating new namespace watcher for namespace %s", rootNamespace)

	if clientset == nil {
		panic("Clientset cannot be nil")
	}

	// Build my own recorder
	if recorder == nil {
		broadcaster := record.NewBroadcaster()
		broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events("")})
		recorder = broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "chaos-monkey"})
	}

	conf := configuration.FromEnvironment()

	return &NamespaceWatcher{
		NamespaceInterface:  clientset.CoreV1().Namespaces(),
		EventRecorderLogger: recorder,

		Logrus:         logrus.WithFields(logrus.Fields{"component": "NamespaceWatcher", "rootNamespace": rootNamespace}),
		CrdWatchers:    map[string]Watcher{},
		Mutex:          &sync.Mutex{},
		metrics:        newNwMetrics(rootNamespace, string(behavior)),
		CleanupTimeout: 1 * time.Minute,
		RootNamespace:  rootNamespace,
		Behavior:       behavior,
		Running:        false,
		Client:         clientset,
		CmcClient:      cmcClientset,
		WatcherTimeout: conf.Timeouts.Namespace,
	}
}

// IsRunning implements Watcher.
func (n *NamespaceWatcher) IsRunning() bool {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	return n.Running
}

func (n *NamespaceWatcher) IsNamespaceAllowed(namespace *corev1.Namespace) bool {
	label, ok := namespace.ObjectMeta.Labels[configuration.NamespaceLabel]

	// We allow all and there is no label
	if n.Behavior == configuration.BehaviorAllowAll && !ok {
		return true
	}

	// We deny all and there is no label
	if n.Behavior == configuration.BehaviorDenyAll && !ok {
		return false
	}

	// We deny all by default, the label is a whitelist (only if its value is "true")
	if n.Behavior == configuration.BehaviorDenyAll {
		return label == "true"
	}

	// We allow all by default, everything will let it through, except for "false"
	if n.Behavior == configuration.BehaviorAllowAll {
		return label != "false"
	}

	// We should never arrive here
	return false
}

// Start implements Watcher.
func (n *NamespaceWatcher) Start(ctx context.Context) error {
	var err error
	var wg sync.WaitGroup

	defer n.Close()

	n.Logrus.Infof("Starting watcher, timeout: %s, behavior: %s", n.WatcherTimeout, n.Behavior)

	timeoutSeconds := int64(n.WatcherTimeout.Seconds())
	w, err := n.Watch(ctx, v1.ListOptions{
		Watch:          true,
		TimeoutSeconds: &timeoutSeconds,
	})
	if err != nil {
		return err
	}

	defer w.Stop()

	n.setRunning(true)

	for n.IsRunning() {
		select {
		case evt, ok := <-w.ResultChan():
			if !ok {
				n.Logrus.Warn("Watcher chan closed, resetting everything")
				w, err = n.restartWatch(ctx, &wg)
				if err != nil {
					n.Logrus.Errorf("Error while restarting watcher: %s", err)
					_ = n.Stop()
				}

				n.metrics.restarts.Inc()

				// Reset the number of active CMCs
				n.metrics.cmcActive.Set(0)
				break
			}

			requestStart := time.Now().UnixMicro()

			ns := evt.Object.(*corev1.Namespace)

			switch evt.Type {

			case "", watch.Error:
				n.Logrus.Errorf("Received empty event or error from watcher: %+v", evt)
				err = errors.New("Empty event or error from namespace watcher")
				_ = n.Stop()

			case watch.Added:
				if !n.IsNamespaceAllowed(ns) {
					logrus.Infof("Not creating watcher for %s", ns.Name)
					continue
				}

				n.Logrus.Infof("Adding watcher for namespace %s", ns.Name)
				if err := n.addWatcher(ns.Name); err != nil {
					logrus.Errorf("Error while trying to add CRD watcher: %s", err)
					continue
				}

				n.Logrus.Debug("All is good! Sending event.")
				n.startCrdWatcher(ctx, ns.Name, &wg)
				n.Eventf(ns, "Normal", "Added", "CRD Watcher added for %s", ns.Name)
				n.metrics.addedEvents.Inc()

			case watch.Modified:
				n.Logrus.Infof("Eventually modifying watcher for %s", ns.Name)
				if n.IsNamespaceAllowed(ns) {
					if err := n.addWatcher(ns.Name); err != nil {
						n.Logrus.Warnf("Error while trying to add CRD watcher: %s", err)
					} else {
						n.Logrus.Infof("Starting newly created watcher for %s", ns.Name)
						n.startCrdWatcher(ctx, ns.Name, &wg)
					}
				} else {
					if err := n.removeWatcher(ns.Name); err != nil {
						n.Logrus.Warnf("Error while trying to remove CRD watcher: %s", err)
					}
				}

				n.metrics.modifiedEvents.Inc()

			case watch.Deleted:
				n.Logrus.Infof("Deleting watcher for namespace %s", ns.Name)
				if err := n.removeWatcher(ns.Name); err != nil {
					n.Logrus.Warnf("Error while trying to remove CRD watcher: %s", err)
				}

				n.Logrus.Debug("All is good! Sending event.")
				n.metrics.deletedEvents.Inc()
				n.Eventf(ns, "Normal", "Deleted", "CRD Watcher deleted for %s", ns.Name)
			}

			requestEnd := time.Now().UnixMicro()

			n.metrics.eventDuration.Observe(float64(requestEnd - requestStart))

		case <-ctx.Done():
			n.Logrus.Info("Context cancelled")
			_ = n.Stop()

		case <-time.After(n.CleanupTimeout):
			n.Logrus.Debug("Cleaning up...")
			n.cleanUp()
		}
	}

	n.Logrus.Info("Namespace watcher stopped, cleaning up...")
	n.Mutex.Lock()

	for ns, crd := range n.CrdWatchers {
		n.Logrus.Infof("Stopping watcher for namespace %s", ns)
		if err := crd.Stop(); err != nil {
			n.Logrus.Warnf("Error while trying to stop CRD watcher: %s", err)
		}

		delete(n.CrdWatchers, ns)
	}

	n.Mutex.Unlock()

	n.Logrus.Info("Waiting for all CRD Watchers to finish")
	wg.Wait()
	n.Logrus.Debug("Unregistering Prometheus metrics")
	return err
}

// Stop implements Watcher.
func (n *NamespaceWatcher) Stop() error {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	n.Logrus.Debugf("Stopping namespace watcher for %s", n.RootNamespace)

	n.Running = false
	return nil
}

// Internal methods
func (n *NamespaceWatcher) addWatcher(namespace string) error {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	if _, ok := n.CrdWatchers[namespace]; ok {
		return fmt.Errorf("Watcher for namespace %s already exists", namespace)
	}

	n.CrdWatchers[namespace] = DefaultCrdFactory(n.Client, n.CmcClient, nil, namespace)

	return nil
}

func (n *NamespaceWatcher) removeWatcher(namespace string) error {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	var err error

	if w, ok := n.CrdWatchers[namespace]; ok {
		err = w.Stop()
		delete(n.CrdWatchers, namespace)
		n.metrics.cmcActive.Dec()
	} else {
		err = fmt.Errorf("Watcher for namespace %s does not exist", namespace)
	}

	return err
}

func (n *NamespaceWatcher) cleanUp() {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	for ns, w := range n.CrdWatchers {
		if !w.IsRunning() {
			n.Logrus.Infof("Cleaning up watcher for namespace %s", ns)
			if err := w.Stop(); err != nil {
				n.Logrus.Warnf("Error while trying to remove CRD watcher: %s", err)
			}

			delete(n.CrdWatchers, ns)
		}
	}
}

func (n *NamespaceWatcher) setRunning(v bool) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	n.Running = v
}

func (n *NamespaceWatcher) startCrdWatcher(ctx context.Context, namespace string, wg *sync.WaitGroup) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	wg.Add(1)
	watcher := n.CrdWatchers[namespace]

	go func() {
		defer wg.Done()

		n.Logrus.Debugf("Starting CRD Watcher for namespace %s", namespace)

		if watcher == nil {
			n.Logrus.Warnf("No watcher found for namespace %s", namespace)
			return
		}

		if err := watcher.Start(ctx); err != nil {
			n.Logrus.Errorf("Error while starting CRD Watcher for namespace %s", namespace)
		}
	}()

	n.metrics.cmcActive.Inc()
	n.metrics.cmcSpawned.Inc()
}

func (n *NamespaceWatcher) restartWatch(ctx context.Context, wg *sync.WaitGroup) (watch.Interface, error) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	n.Logrus.Info("Preparing for restart: cleaning all the CRD Watchers")

	// Stop all the running watches
	for k, w := range n.CrdWatchers {
		n.Logrus.Debugf("Cleaning %s", k)
		if err := w.Stop(); err != nil {
			logrus.Warnf("Error while stopping CRD Watcher for %s: %s", k, err)
		}

		delete(n.CrdWatchers, k)
	}

	// Wait for all the CRD Watchers to be stopped
	n.Logrus.Info("Waiting for all CRD Watchers to finish...")
	wg.Wait()

	n.Logrus.Infof("Restarting watcher with timeout: %s", n.WatcherTimeout)

	// Now recreate a new watch interface
	timeoutSeconds := int64(n.WatcherTimeout.Seconds())
	return n.Watch(ctx, v1.ListOptions{
		Watch:          true,
		TimeoutSeconds: &timeoutSeconds,
	})
}
