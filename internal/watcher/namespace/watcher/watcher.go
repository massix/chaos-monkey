package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	common "github.com/massix/chaos-monkey/internal/watcher"
	crdfactory "github.com/massix/chaos-monkey/internal/watcher/crd/factory"
	crdwatcher "github.com/massix/chaos-monkey/internal/watcher/crd/watcher"
	deploymentfactory "github.com/massix/chaos-monkey/internal/watcher/deployment/factory"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
)

type Interface interface {
	common.Watcher
}

type NamespaceWatcher struct {
	typedcorev1.NamespaceInterface

	// Keep track of all the currently deployed watchers
	namespaces    map[string]crdwatcher.Interface
	rootNamespace string
	crdFactory    crdfactory.Interface
	depFactory    deploymentfactory.Interface

	running     bool
	broadcaster record.EventRecorderLogger
}

func NewNamespaceWatcher(rootNamespace string, crdFactory crdfactory.Interface, depFactory deploymentfactory.Interface) Interface {
	logrus.Info("Creating namespace watcher")

	cfg, err := rest.InClusterConfig()
	if err != nil {
		logrus.Fatal(err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logrus.Fatal(err)
	}

	bc := record.NewBroadcaster()
	bc.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})

	recorder := bc.NewRecorder(scheme.Scheme, v1.EventSource{Component: "chaos-monkey"})

	return &NamespaceWatcher{
		NamespaceInterface: clientset.CoreV1().Namespaces(),
		namespaces:         map[string]crdwatcher.Interface{},
		running:            false,
		rootNamespace:      rootNamespace,
		broadcaster:        recorder,
		crdFactory:         crdFactory,
		depFactory:         depFactory,
	}
}

func (c *NamespaceWatcher) IsRunning() bool {
	return c.running
}

func (c *NamespaceWatcher) Stop() error {
	c.running = false
	return nil
}

func (c *NamespaceWatcher) addWatcherForNamespace(namespace string) error {
	logrus.Infof("Adding watcher for namespace %s", namespace)
	if _, ok := c.namespaces[namespace]; ok {
		return fmt.Errorf("Watcher for namespace %s already exists", namespace)
	}

	c.namespaces[namespace] = c.crdFactory.New(namespace)

	return nil
}

func (c *NamespaceWatcher) removeWatcherForNamespace(namespace string) error {
	var err error

	if w, ok := c.namespaces[namespace]; ok {
		logrus.Infof("Removing watcher for namespace %s", namespace)
		err = w.Stop()
		delete(c.namespaces, namespace)
	}

	return err
}

func (c *NamespaceWatcher) Start(ctx context.Context) error {
	logrus.Info("Starting namespace watcher")
	w, err := c.Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	var res error
	c.running = true

	// Start the infinite loop
	for c.running {
		select {
		case evt := <-w.ResultChan():
			ns := evt.Object.(*v1.Namespace)
			switch evt.Type {
			case "", watch.Error:
				logrus.Errorf("Received empty event or error from watcher: %+v", evt)
				res = errors.New("Empty event or error from namespace watcher")
				c.running = false
			case watch.Added:
				if err := c.addWatcherForNamespace(ns.ObjectMeta.Name); err != nil {
					logrus.Warnf("Error while trying to add CRD watcher: %s", err)
					continue
				}

				wg.Add(1)
				go func() {
					watcher := c.namespaces[ns.ObjectMeta.Name]

					if watcher != nil {
						if err := watcher.Start(ctx); err != nil {
							logrus.Warnf("Error from CRD watcher: %s", err)
						}
					} else {
						logrus.Warnf("No watcher found for namespace %s", ns.ObjectMeta.Name)
					}

					wg.Done()
				}()

				logrus.Info("Sending event")
				c.broadcaster.Eventf(ns, v1.EventTypeNormal, "ChaosMonkeyAdded", "ChaosMonkey added to %s", ns.Name)
			case watch.Deleted:
				if err := c.removeWatcherForNamespace(ns.ObjectMeta.Name); err != nil {
					logrus.Warnf("Error while trying to remove CRD watcher: %s", err)
				}
			}
		case <-ctx.Done():
			logrus.Info("Context cancelled")
			c.running = false
		case <-time.After(30 * time.Second):
			logrus.Debug("Namespace garbage collecting...")

			for k, w := range c.namespaces {
				if !w.IsRunning() {
					logrus.Infof("Removing watcher for namespace %s", k)
					_ = w.Stop()
					delete(c.namespaces, k)
				}
			}
		}
	}

	// Stop all the remaining watchers
	for k, v := range c.namespaces {
		logrus.Infof("Force stopping watcher for namespace %s", k)
		_ = v.Stop()
	}

	c.running = false
	logrus.Info("Namespace watcher stopped")
	w.Stop()
	wg.Wait()
	return res
}
