package watcher

import (
	"context"
	"io"
	"time"

	cmcv "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/massix/chaos-monkey/internal/configuration"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Watcher interface {
	io.Closer
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
}

type ConfigurableWatcher interface {
	Watcher

	SetMinReplicas(v int)
	SetMaxReplicas(v int)
	SetTimeout(v time.Duration)
	SetEnabled(v bool)
}

type (
	NamespaceFactory  func(clientset kubernetes.Interface, cmcClientset cmcv.Interface, metricsClientset metricsv.Interface, recorder record.EventRecorderLogger, rootNamespace string, behavior configuration.Behavior) Watcher
	CrdFactory        func(clientset kubernetes.Interface, cmcClientset cmcv.Interface, metricsClientset metricsv.Interface, recorder record.EventRecorderLogger, namespace string) Watcher
	DeploymentFactory func(clientset kubernetes.Interface, recorder record.EventRecorderLogger, deployment *appsv1.Deployment) ConfigurableWatcher
	PodFactory        func(clientset kubernetes.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) ConfigurableWatcher
	AntiHPAFactory    func(client metricsv.Interface, podset typedcorev1.PodInterface, namespace, podLabel string) ConfigurableWatcher
)

// Default factories
var (
	DefaultNamespaceFactory NamespaceFactory = func(clientset kubernetes.Interface, cmcClientset cmcv.Interface, metricsClientset metricsv.Interface, recorder record.EventRecorderLogger, rootNamespace string, behavior configuration.Behavior) Watcher {
		return NewNamespaceWatcher(clientset, cmcClientset, metricsClientset, recorder, rootNamespace, behavior)
	}

	DefaultCrdFactory CrdFactory = func(clientset kubernetes.Interface, cmcClientset cmcv.Interface, metricsClientset metricsv.Interface, recorder record.EventRecorderLogger, namespace string) Watcher {
		return NewCrdWatcher(clientset, cmcClientset, metricsClientset, recorder, namespace)
	}

	DefaultDeploymentFactory DeploymentFactory = func(clientset kubernetes.Interface, recorder record.EventRecorderLogger, deployment *appsv1.Deployment) ConfigurableWatcher {
		return NewDeploymentWatcher(clientset, recorder, deployment)
	}

	DefaultPodFactory PodFactory = func(clientset kubernetes.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) ConfigurableWatcher {
		return NewPodWatcher(clientset, recorder, namespace, labelSelector...)
	}

	DefaultAntiHPAFactory AntiHPAFactory = func(client metricsv.Interface, podset typedcorev1.PodInterface, namespace, podLabel string) ConfigurableWatcher {
		return NewAntiHPAWatcher(client, podset, namespace, podLabel)
	}
)
