package watcher

import (
	"context"
	"io"
	"time"

	mc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/massix/chaos-monkey/internal/configuration"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
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
	NamespaceFactory  func(clientset kubernetes.Interface, cmcClientset mc.Interface, recorder record.EventRecorderLogger, rootNamespace string, behavior configuration.Behavior) Watcher
	CrdFactory        func(clientset kubernetes.Interface, cmcClientset mc.Interface, recorder record.EventRecorderLogger, namespace string) Watcher
	DeploymentFactory func(clientset kubernetes.Interface, recorder record.EventRecorderLogger, deployment *appsv1.Deployment) ConfigurableWatcher
	PodFactory        func(clientset kubernetes.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) ConfigurableWatcher
)

// Default factories
var (
	DefaultNamespaceFactory  NamespaceFactory  = NewNamespaceWatcher
	DefaultCrdFactory        CrdFactory        = NewCrdWatcher
	DefaultDeploymentFactory DeploymentFactory = NewDeploymentWatcher
	DefaultPodFactory        PodFactory        = NewPodWatcher
)
