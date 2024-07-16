package watcher

import (
	"context"
	"errors"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

type PodWatcher struct {
	corev1.PodInterface
	record.EventRecorderLogger

	Logrus        logrus.FieldLogger
	Mutex         *sync.Mutex
	ForceStopChan chan interface{}
	metrics       *pwMetrics
	LabelSelector string
	Namespace     string
	PodList       []*apicorev1.Pod
	Timeout       time.Duration
	WatchTimeout  time.Duration
	Enabled       bool
	Running       bool
}

// Close implements ConfigurableWatcher.
func (p *PodWatcher) Close() error {
	p.metrics.unregister()
	return nil
}

type pwMetrics struct {
	// General statistics about the pods
	podsAdded   prometheus.Counter
	podsRemoved prometheus.Counter
	podsActive  prometheus.Gauge
	podsKilled  prometheus.Counter

	// Number of restarts
	restarts prometheus.Counter
}

func (pw *pwMetrics) unregister() {
	prometheus.Unregister(pw.podsAdded)
	prometheus.Unregister(pw.podsRemoved)
	prometheus.Unregister(pw.podsActive)
	prometheus.Unregister(pw.podsKilled)
	prometheus.Unregister(pw.restarts)
}

func newPwMetrics(namespace, combinedLabelSelector string) *pwMetrics {
	mkCounterOpts := func(name, help string) prometheus.CounterOpts {
		return prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Subsystem:   "podwatcher",
			Name:        name,
			Help:        help,
			ConstLabels: map[string]string{"namespace": namespace, "label_selector": combinedLabelSelector},
		}
	}

	return &pwMetrics{
		podsAdded:   promauto.NewCounter(mkCounterOpts("pods_added", "Total number of pods added")),
		podsRemoved: promauto.NewCounter(mkCounterOpts("pods_removed", "Total number of pods removed")),
		podsKilled:  promauto.NewCounter(mkCounterOpts("pods_killed", "Total number of pods killed")),
		restarts:    promauto.NewCounter(mkCounterOpts("restarts", "Total number of restarts")),
		podsActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace:   "chaos_monkey",
			Subsystem:   "podwatcher",
			Name:        "pods_active",
			Help:        "Current number of pods being targeted",
			ConstLabels: map[string]string{"namespace": namespace, "label_selector": combinedLabelSelector},
		}),
	}
}

var _ = (ConfigurableWatcher)((*PodWatcher)(nil))

func NewPodWatcher(clientset kubernetes.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) ConfigurableWatcher {
	logrus.Infof("Creating pod watcher in namespace %s for label selector %s", namespace, labelSelector)

	if recorder == nil {
		logrus.Debugf("No recorder provided, using default")
		bc := record.NewBroadcaster()
		bc.StartRecordingToSink(&corev1.EventSinkImpl{
			Interface: clientset.CoreV1().Events(namespace),
		})
		recorder = bc.NewRecorder(scheme.Scheme, apicorev1.EventSource{Component: "chaos-monkey"})
	}

	combinedSelector := strings.Join(labelSelector, ",")

	conf := configuration.FromEnvironment()

	return &PodWatcher{
		PodInterface:        clientset.CoreV1().Pods(namespace),
		EventRecorderLogger: recorder,

		Logrus:        logrus.WithFields(logrus.Fields{"component": "PodWatcher", "namespace": namespace, "labelSelector": combinedSelector}),
		Mutex:         &sync.Mutex{},
		Namespace:     namespace,
		LabelSelector: combinedSelector,
		PodList:       []*apicorev1.Pod{},
		Timeout:       30 * time.Second,
		WatchTimeout:  conf.Timeouts.Pod,
		ForceStopChan: make(chan interface{}),
		metrics:       newPwMetrics(namespace, combinedSelector),
		Enabled:       true,
		Running:       false,
	}
}

// SetTimeout implements ConfigurableWatcher.
func (p *PodWatcher) SetTimeout(v time.Duration) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Logrus.Debugf("Setting new timeout: %s, old timeout: %s", v, p.Timeout)
	p.Timeout = v
}

// SetEnabled implements ConfigurableWatcher.
func (p *PodWatcher) SetEnabled(v bool) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Enabled = v
}

// SetMaxReplicas implements ConfigurableWatcher.
func (p *PodWatcher) SetMaxReplicas(v int) {
	// No op
}

// SetMinReplicas implements ConfigurableWatcher.
func (p *PodWatcher) SetMinReplicas(v int) {
	// No op
}

// IsRunning implements Watcher.
func (p *PodWatcher) IsRunning() bool {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	return p.Running
}

// Start implements Watcher.
func (p *PodWatcher) Start(ctx context.Context) error {
	var err error
	p.Logrus.Info("Starting pod watcher")

	defer p.Close()

	watchTimeout := int64(p.WatchTimeout.Seconds())
	w, err := p.Watch(ctx, metav1.ListOptions{
		Watch:          true,
		LabelSelector:  p.LabelSelector,
		TimeoutSeconds: &watchTimeout,
	})
	if err != nil {
		return err
	}

	eventFormat := "Pod may be targeted by next Chaos Monkey in %s"

	defer w.Stop()

	p.setRunning(true)
	timer := time.NewTimer(p.getTimeout())

	for p.IsRunning() {
		select {
		case evt, ok := <-w.ResultChan():
			if !ok {
				p.Logrus.Warn("Watcher timeout")
				if w, err = p.restartWatch(ctx); err != nil {
					p.Logrus.Error(err)
					p.setRunning(false)
				}

				p.metrics.restarts.Inc()
				break
			}

			p.Logrus.Debugf("Pod Watcher received event: %s", evt.Type)
			pod := evt.Object.(*apicorev1.Pod)

			switch evt.Type {
			case watch.Error:
				p.Logrus.Errorf("Received empty event or error from pod watcher: %+v", evt)
				err = errors.New("Empty event or error from pod watcher")
				p.setRunning(false)
			case watch.Added:
				p.Logrus.Infof("Adding pod to list: %s", pod.Name)
				p.addPodToList(pod)
				p.Eventf(pod, apicorev1.EventTypeNormal, "ChaosMonkeyTarget", eventFormat, p.getTimeout())
				p.Logrus.Debug("Pod added, event sent!")
			case watch.Deleted:
				p.Logrus.Infof("Removing pod from list: %s", pod.Name)
				p.removePodFromList(pod)
			case watch.Modified:
				p.Logrus.Debugf("Ignoring modification of pod %s", pod.Name)
			}
		case <-ctx.Done():
			p.Logrus.Info("Pod Watcher context done")
			p.setRunning(false)
		case <-timer.C:
			if !p.isEnabled() {
				p.Logrus.Debug("CRD not enabled, refusing to disrupt pods")
				timer.Reset(p.getTimeout())
				continue
			}

			p.Logrus.Info("Disrupting random pod")
			if randomPod, err := p.getRandomPod(); err != nil {
				p.Logrus.Warnf("Warning: %s", err)
			} else {
				p.Logrus.Infof("Disrupting pod %s", randomPod.Name)
				gracePeriod := int64(0)
				if err := p.Delete(ctx, randomPod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}); err != nil {
					p.Logrus.Errorf("Could not disrupt pod: %s", err)
				} else {
					p.Event(
						randomPod,
						apicorev1.EventTypeNormal,
						"ChaosMonkeyTarget",
						"Pod got killed by Chaos Monkey",
					)
					p.Logrus.Debug("Pod disrupted, event sent!")
					p.metrics.podsKilled.Inc()
				}
			}

			p.Mutex.Lock()
			// Send an event to all the Pods, they could be the next target!
			for _, pod := range p.PodList {
				p.Eventf(pod, apicorev1.EventTypeNormal, "ChaosMonkeyTarget", eventFormat, p.Timeout)
			}
			p.Mutex.Unlock()
		case <-p.ForceStopChan:
			p.Logrus.Info("Force stopped watcher")
		}

		timer.Reset(p.getTimeout())
	}

	p.Logrus.Info("Pod watcher finished")
	return err
}

// Stop implements Watcher.
func (p *PodWatcher) Stop() error {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Logrus.Infof("Stopping pod watcher")

	p.Running = false

	select {
	case p.ForceStopChan <- nil:
	default:
		p.Logrus.Warn("Could not write in channel")
	}

	p.metrics.unregister()

	return nil
}

func (p *PodWatcher) isEnabled() bool {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	return p.Enabled
}

func (p *PodWatcher) setRunning(v bool) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Running = v
}

func (p *PodWatcher) getTimeout() time.Duration {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	return p.Timeout
}

func (p *PodWatcher) addPodToList(pod *apicorev1.Pod) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Logrus.Debugf("Current pod list size: %d", len(p.PodList))
	p.PodList = append(p.PodList, pod)
	p.Logrus.Debugf("Final pod list size: %d", len(p.PodList))
	p.metrics.podsAdded.Inc()
	p.metrics.podsActive.Set(float64(len(p.PodList)))
}

func (p *PodWatcher) removePodFromList(pod *apicorev1.Pod) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Logrus.Debugf("Current pod list size: %d", len(p.PodList))
	p.PodList = slices.DeleteFunc(p.PodList, func(v *apicorev1.Pod) bool {
		p.Logrus.Debugf("Checking pod %s against %s: %+v", v.Name, pod.Name, v.Name == pod.Name)
		return v.Name == pod.Name
	})
	p.Logrus.Debugf("Final pod list size: %d", len(p.PodList))
	p.metrics.podsRemoved.Inc()
	p.metrics.podsActive.Set(float64(len(p.PodList)))
}

func (p *PodWatcher) getRandomPod() (*apicorev1.Pod, error) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if len(p.PodList) == 0 {
		return nil, errors.New("No pods found")
	}

	return p.PodList[rand.Intn(len(p.PodList))], nil
}

func (p *PodWatcher) restartWatch(ctx context.Context) (watch.Interface, error) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Logrus.Info("Resetting pod watcher")

	// Remove all the recorded pods
	p.PodList = []*apicorev1.Pod{}
	p.Logrus.Debug("Cleaned pod list")

	timeoutSeconds := int64(p.WatchTimeout.Seconds())
	return p.Watch(ctx, metav1.ListOptions{
		Watch:          true,
		LabelSelector:  p.LabelSelector,
		TimeoutSeconds: &timeoutSeconds,
	})
}
