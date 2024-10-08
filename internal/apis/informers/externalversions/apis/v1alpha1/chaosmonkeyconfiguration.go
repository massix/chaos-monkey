// Generated code, do not touch

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	versioned "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	internalinterfaces "github.com/massix/chaos-monkey/internal/apis/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/massix/chaos-monkey/internal/apis/listers/apis/v1alpha1"
	apisv1alpha1 "github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// ChaosMonkeyConfigurationInformer provides access to a shared informer and lister for
// ChaosMonkeyConfigurations.
type ChaosMonkeyConfigurationInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.ChaosMonkeyConfigurationLister
}

type chaosMonkeyConfigurationInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewChaosMonkeyConfigurationInformer constructs a new informer for ChaosMonkeyConfiguration type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewChaosMonkeyConfigurationInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredChaosMonkeyConfigurationInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredChaosMonkeyConfigurationInformer constructs a new informer for ChaosMonkeyConfiguration type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredChaosMonkeyConfigurationInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ChaosMonkeyConfigurationV1alpha1().ChaosMonkeyConfigurations(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ChaosMonkeyConfigurationV1alpha1().ChaosMonkeyConfigurations(namespace).Watch(context.TODO(), options)
			},
		},
		&apisv1alpha1.ChaosMonkeyConfiguration{},
		resyncPeriod,
		indexers,
	)
}

func (f *chaosMonkeyConfigurationInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredChaosMonkeyConfigurationInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *chaosMonkeyConfigurationInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&apisv1alpha1.ChaosMonkeyConfiguration{}, f.defaultInformer)
}

func (f *chaosMonkeyConfigurationInformer) Lister() v1alpha1.ChaosMonkeyConfigurationLister {
	return v1alpha1.NewChaosMonkeyConfigurationLister(f.Informer().GetIndexer())
}
