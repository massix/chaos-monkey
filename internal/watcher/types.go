package watcher

import (
	"context"
	"time"

	mc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
)

type Watcher interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
}

type DeploymentWatcherI interface {
	Watcher

	SetMinReplicas(v int)
	SetMaxReplicas(v int)
	SetTimeout(v time.Duration)
	SetEnabled(v bool)
}

type (
	NamespaceFactory  func(clientset kubernetes.Interface, cmcClientset mc.Interface, recorder record.EventRecorderLogger, rootNamespace string) Watcher
	CrdFactory        func(clientset kubernetes.Interface, cmcClientset mc.Interface, recorder record.EventRecorderLogger, namespace string) Watcher
	DeploymentFactory func(clientset kubernetes.Interface, recorder record.EventRecorderLogger, deployment *appsv1.Deployment) DeploymentWatcherI
)

// Default factories
var (
	DefaultNamespaceFactory  NamespaceFactory  = NewNamespaceWatcher
	DefaultCrdFactory        CrdFactory        = NewCrdWatcher
	DefaultDeploymentFactory DeploymentFactory = NewDeploymentWatcher
)
