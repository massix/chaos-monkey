package factory

import (
	crdwatcher "github.com/massix/chaos-monkey/internal/watcher/crd/watcher"
	deploymentfactory "github.com/massix/chaos-monkey/internal/watcher/deployment/factory"
)

type Interface interface {
	New(forNamespace string) crdwatcher.Interface
}

type factory struct {
	depFactory deploymentfactory.Interface
}

// New implements Interface.
func (f *factory) New(forNamespace string) crdwatcher.Interface {
	return crdwatcher.NewCrdWatcher(forNamespace, f.depFactory)
}

func New(f deploymentfactory.Interface) Interface {
	return &factory{
		depFactory: f,
	}
}
