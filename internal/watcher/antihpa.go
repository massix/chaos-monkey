package watcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	mv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	typedmv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

type antiHPAmetrics struct {
	// Number of runs
	runs prometheus.Counter

	// Number of pods killed
	podsKilled prometheus.Counter

	// Total value of CPU of pods killed
	totalCpu prometheus.Gauge
}

func (a *antiHPAmetrics) unregister() {
	prometheus.Unregister(a.runs)
	prometheus.Unregister(a.podsKilled)
	prometheus.Unregister(a.totalCpu)
}

func newAntiHPAMetrics(namespace, selector string) *antiHPAmetrics {
	constLabels := map[string]string{
		"namespace": namespace,
		"selector":  selector,
	}

	return &antiHPAmetrics{
		runs: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Subsystem:   "antihpawatcher",
			Name:        "runs",
			Help:        "Number of runs",
			ConstLabels: constLabels,
		}),
		podsKilled: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "chaos_monkey",
			Subsystem:   "antihpawatcher",
			Name:        "pods_killed",
			Help:        "Number of pods killed",
			ConstLabels: constLabels,
		}),
		totalCpu: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace:   "chaos_monkey",
			Subsystem:   "antihpawatcher",
			Name:        "average_cpu",
			Help:        "Total value of CPU of all the killed pods",
			ConstLabels: constLabels,
		}),
	}
}

type AntiHPAWatcher struct {
	Enabled           bool
	Logrus            logrus.FieldLogger
	MetricsClient     typedmv1beta1.MetricsV1beta1Interface
	PodsClient        typedcorev1.PodInterface
	Namespace         string
	Running           bool
	Timeout           time.Duration
	LabelSelector     string
	PrometheusMetrics *antiHPAmetrics
	sync.Mutex
}

// SetEnabled implements ConfigurableWatcher.
func (a *AntiHPAWatcher) SetEnabled(v bool) {
	a.Lock()
	defer a.Unlock()

	a.Enabled = v
}

// SetMaxReplicas implements ConfigurableWatcher.
func (a *AntiHPAWatcher) SetMaxReplicas(v int) {}

// SetMinReplicas implements ConfigurableWatcher.
func (a *AntiHPAWatcher) SetMinReplicas(v int) {}

// SetTimeout implements ConfigurableWatcher.
func (a *AntiHPAWatcher) SetTimeout(v time.Duration) {
	a.Lock()
	defer a.Unlock()

	a.Timeout = v
}

func (a *AntiHPAWatcher) GetTimeout() time.Duration {
	a.Lock()
	defer a.Unlock()

	return a.Timeout
}

func (a *AntiHPAWatcher) GetEnabled() bool {
	a.Lock()
	defer a.Unlock()

	return a.Enabled
}

func NewAntiHPAWatcher(clientset metricsv.Interface, podsClientset typedcorev1.PodInterface, namespace, podLabel string) *AntiHPAWatcher {
	return &AntiHPAWatcher{
		Enabled:           true,
		Logrus:            logrus.WithFields(logrus.Fields{"component": "AntiHPAWatcher", "namespace": namespace, "podLabel": podLabel}),
		MetricsClient:     clientset.MetricsV1beta1(),
		PodsClient:        podsClientset,
		Mutex:             sync.Mutex{},
		Running:           false,
		Namespace:         namespace,
		Timeout:           10 * time.Minute,
		PrometheusMetrics: newAntiHPAMetrics(namespace, podLabel),
		LabelSelector:     podLabel,
	}
}

// Close implements Watcher.
func (a *AntiHPAWatcher) Close() error {
	return nil
}

// IsRunning implements Watcher.
func (a *AntiHPAWatcher) IsRunning() bool {
	a.Lock()
	defer a.Unlock()
	return a.Running
}

func (a *AntiHPAWatcher) SetRunning(val bool) {
	a.Lock()
	defer a.Unlock()

	a.Running = val
}

type node struct {
	PodName       string
	ContainerName string
	CpuValue      int64
	MemoryValue   int64
	Left          *node
	Right         *node
}

func newNode(cpu, mem int64, podName, containerName string) *node {
	return &node{
		PodName:       podName,
		ContainerName: containerName,
		CpuValue:      cpu,
		MemoryValue:   mem,
		Left:          nil,
		Right:         nil,
	}
}

func (n *node) insert(cpu, mem int64, podName, containerName string) *node {
	if n == nil {
		return newNode(cpu, mem, podName, containerName)
	}

	if cpu < n.CpuValue {
		n.Left = n.Left.insert(cpu, mem, podName, containerName)
	} else {
		n.Right = n.Right.insert(cpu, mem, podName, containerName)
	}

	return n
}

func (n *node) getMostUsedCpu() *node {
	if n == nil {
		return nil
	}

	// Must go right until there is something
	if n.Right != nil {
		return n.Right.getMostUsedCpu()
	}

	return n
}

func (a *AntiHPAWatcher) getMostUsedPod(in *mv1beta1.PodMetricsList) (string, error) {
	var tree *node = nil

	for _, item := range in.Items {
		a.Logrus.Debugf("Item: %s", item.Name)
		for _, container := range item.Containers {
			cpuRes, cpuOk := container.Usage.Cpu().AsInt64()
			memRes, memOk := container.Usage.Memory().AsInt64()

			if memOk && cpuOk {
				a.Logrus.Debugf("Container %s, cpu: %d, memory: %d", container.Name, cpuRes, memRes)
				if tree == nil {
					tree = tree.insert(cpuRes, memRes, item.Name, container.Name)
				} else {
					tree.insert(cpuRes, memRes, item.Name, container.Name)
				}
			} else {
				a.Logrus.Warnf("Could not convert memory or cpu to int64 (cpu: %+v, memory: %+v)", cpuOk, memOk)
			}
		}
	}

	ret := tree.getMostUsedCpu()

	if ret == nil {
		return "", fmt.Errorf("Could not find a pod to kill (list %d elements)", len(in.Items))
	}

	a.PrometheusMetrics.totalCpu.Add(float64(ret.CpuValue))
	return ret.PodName, nil
}

// Start implements Watcher.
func (a *AntiHPAWatcher) Start(ctx context.Context) error {
	timer := time.NewTimer(a.GetTimeout())

	a.SetRunning(true)

	for a.IsRunning() {
		select {
		case <-timer.C:
			if !a.GetEnabled() {
				a.Logrus.Debug("AntiHPA is not enabled")
				continue
			}

			a.Logrus.Info("AntiHPA kicking in")
			a.PrometheusMetrics.runs.Inc()

			podMetrics, err := a.MetricsClient.PodMetricses(a.Namespace).List(ctx, metav1.ListOptions{
				LabelSelector: a.LabelSelector,
			})
			if err != nil {
				a.Logrus.Error(err)
				break
			}

			mostUsedPod, err := a.getMostUsedPod(podMetrics)
			a.Logrus.Infof("Should kill: %s", mostUsedPod)

			gracePeriodSeconds := int64(0)
			if err := a.PodsClient.Delete(ctx, mostUsedPod, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}); err != nil {
				a.Logrus.Warnf("Could not delete pod: %s", err)
			} else {
				a.PrometheusMetrics.podsKilled.Inc()
			}
		case <-ctx.Done():
			a.Logrus.Info("Watcher context done")
			a.SetRunning(false)
		}

		timer.Reset(a.GetTimeout())
	}

	return nil
}

// Stop implements Watcher.
func (a *AntiHPAWatcher) Stop() error {
	a.Lock()
	defer a.Unlock()

	a.Running = false
	return nil
}

var (
	_ = (Watcher)((*AntiHPAWatcher)(nil))
	_ = (ConfigurableWatcher)((*AntiHPAWatcher)(nil))
)
