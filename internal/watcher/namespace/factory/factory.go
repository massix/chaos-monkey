package factory

import (
	crdfactory "github.com/massix/chaos-monkey/internal/watcher/crd/factory"
	depfactory "github.com/massix/chaos-monkey/internal/watcher/deployment/factory"
	"github.com/massix/chaos-monkey/internal/watcher/namespace/watcher"
)

type Interface interface {
	New(ns string) watcher.Interface
}

type factory struct {
	crdFactory crdfactory.Interface
	depFactory depfactory.Interface
}

// New implements Interface.
func (f *factory) New(ns string) watcher.Interface {
	return watcher.NewNamespaceWatcher(ns, f.crdFactory, f.depFactory)
}

func New(crdFactory crdfactory.Interface, depFactory depfactory.Interface) Interface {
	return &factory{
		crdFactory: crdFactory,
		depFactory: depFactory,
	}
}
