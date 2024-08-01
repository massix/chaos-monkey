package watcher

import (
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

type containerMetric struct {
	CpuUsage    int64
	MemoryUsage int64
}

func generateMetrics(in []containerMetric) []metricsv1beta1.ContainerMetrics {
	var ret []metricsv1beta1.ContainerMetrics
	for i, m := range in {
		ret = append(ret, metricsv1beta1.ContainerMetrics{
			Name: fmt.Sprintf("container%d", i),
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewQuantity(m.CpuUsage, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(m.MemoryUsage, resource.DecimalSI),
			},
		})
	}

	return ret
}

func TestAntiHPA_binaryTree(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	var root *node = nil

	root = root.
		insert(1, 2, "pod1", "container1").
		insert(2, 3, "pod1", "container2").
		insert(0, 5, "pod2", "container1").
		insert(3, 10, "pod2", "container2").
		insert(6, 11, "pod2", "container3").
		insert(4, 10, "pod3", "container1").
		insert(5, 11, "pod4", "container1")

	mostUsed := root.getMostUsedCpu()
	if mostUsed.PodName != "pod2" && mostUsed.ContainerName != "container3" {
		t.Errorf("Most used: %+v", mostUsed)
	}
}

func TestAntiHPA_getMostUsedPod(t *testing.T) {
	metricsList := &metricsv1beta1.PodMetricsList{
		Items: []metricsv1beta1.PodMetrics{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
				Containers: generateMetrics([]containerMetric{
					{CpuUsage: 100, MemoryUsage: 128},
					{CpuUsage: 98, MemoryUsage: 512},
					{CpuUsage: 110, MemoryUsage: 312},
				}),
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod2"},
				Containers: generateMetrics([]containerMetric{
					{CpuUsage: 214, MemoryUsage: 12},
					{CpuUsage: 203, MemoryUsage: 1024},
					{CpuUsage: 512, MemoryUsage: 14},
				}),
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod3"},
				Containers: generateMetrics([]containerMetric{
					{CpuUsage: 431, MemoryUsage: 3},
					{CpuUsage: 15, MemoryUsage: 2045},
					{CpuUsage: 12, MemoryUsage: 10},
				}),
			},
		},
	}

	logrus.SetLevel(logrus.DebugLevel)
	antiHpa := &AntiHPAWatcher{
		Logrus:            logrus.StandardLogger(),
		PrometheusMetrics: newAntiHPAMetrics("", ""),
	}

	res, err := antiHpa.getMostUsedPod(metricsList)
	if err != nil {
		t.Fatal(err)
	}

	if res != "pod2" {
		t.Fatalf("expected pod2, got %s", res)
	}
}
