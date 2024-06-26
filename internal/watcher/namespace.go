package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	mc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

type NamespaceWatcher struct {
	typedcorev1.NamespaceInterface
	record.EventRecorderLogger
	Client         kubernetes.Interface
	CmcClient      mc.Interface
	CrdWatchers    map[string]Watcher
	Mutex          *sync.Mutex
	RootNamespace  string
	CleanupTimeout time.Duration
	Running        bool
}

var _ = (Watcher)((*NamespaceWatcher)(nil))

func NewNamespaceWatcher(clientset kubernetes.Interface, cmcClientset mc.Interface, recorder record.EventRecorderLogger, rootNamespace string) Watcher {
	logrus.Infof("Creating new namespace watcher for namespace %s", rootNamespace)

	if clientset == nil {
		panic("Clientset cannot be nil")
	}

	// Build my own recorder
	if recorder == nil {
		broadcaster := record.NewBroadcaster()
		broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events("")})
		recorder = broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "chaos-monkey"})
	}

	return &NamespaceWatcher{
		NamespaceInterface:  clientset.CoreV1().Namespaces(),
		EventRecorderLogger: recorder,
		CrdWatchers:         map[string]Watcher{},
		Mutex:               &sync.Mutex{},
		CleanupTimeout:      1 * time.Minute,
		RootNamespace:       rootNamespace,
		Running:             false,
		Client:              clientset,
		CmcClient:           cmcClientset,
	}
}

// IsRunning implements Watcher.
func (n *NamespaceWatcher) IsRunning() bool {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	return n.Running
}

// Start implements Watcher.
func (n *NamespaceWatcher) Start(ctx context.Context) error {
	var err error
	var wg sync.WaitGroup

	logrus.Infof("Starting namespace watcher for %s", n.RootNamespace)

	w, err := n.Watch(ctx, v1.ListOptions{})
	if err != nil {
		return err
	}

	n.setRunning(true)

	for n.IsRunning() {
		select {
		case evt := <-w.ResultChan():
			ns := evt.Object.(*corev1.Namespace)

			switch evt.Type {

			case "", watch.Error:
				logrus.Errorf("Received empty event or error from watcher: %+v", evt)
				err = errors.New("Empty event or error from namespace watcher")
				_ = n.Stop()

			case watch.Added:
				logrus.Infof("Adding watcher for namespace %s", ns.Name)
				if err := n.addWatcher(ns.Name); err != nil {
					logrus.Errorf("Error while trying to add CRD watcher: %s", err)
					continue
				}

				n.startCrdWatcher(ctx, ns.Name, &wg)
				n.Eventf(ns, "Normal", "Added", "CRD Watcher added for %s", ns.Name)

			case watch.Deleted:
				logrus.Infof("Deleting watcher for namespace %s", ns.Name)
				if err := n.removeWatcher(ns.Name); err != nil {
					logrus.Warnf("Error while trying to remove CRD watcher: %s", err)
				}

				n.Eventf(ns, "Normal", "Deleted", "CRD Watcher deleted for %s", ns.Name)
			}

		case <-ctx.Done():
			logrus.Infof("Context cancelled")
			_ = n.Stop()

		case <-time.After(n.CleanupTimeout):
			logrus.Debug("Cleaning up...")
			n.cleanUp()
		}
	}

	logrus.Info("Namespace watcher stopped, cleaning up...")
	n.Mutex.Lock()

	for ns, crd := range n.CrdWatchers {
		logrus.Infof("Stopping watcher for namespace %s", ns)
		if err := crd.Stop(); err != nil {
			logrus.Warnf("Error while trying to stop CRD watcher: %s", err)
		}

		delete(n.CrdWatchers, ns)
	}

	n.Mutex.Unlock()

	wg.Wait()
	return err
}

// Stop implements Watcher.
func (n *NamespaceWatcher) Stop() error {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	n.Running = false
	return nil
}

// Internal methods
func (n *NamespaceWatcher) addWatcher(namespace string) error {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	if _, ok := n.CrdWatchers[namespace]; ok {
		return fmt.Errorf("Watcher for namespace %s already exists", namespace)
	}

	n.CrdWatchers[namespace] = DefaultCrdFactory(n.Client, n.CmcClient, nil, namespace)

	return nil
}

func (n *NamespaceWatcher) removeWatcher(namespace string) error {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	var err error

	if w, ok := n.CrdWatchers[namespace]; ok {
		err = w.Stop()
		delete(n.CrdWatchers, namespace)
	} else {
		err = fmt.Errorf("Watcher for namespace %s does not exist", namespace)
	}

	return err
}

func (n *NamespaceWatcher) cleanUp() {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	for ns, w := range n.CrdWatchers {
		if !w.IsRunning() {
			logrus.Infof("Cleaning up watcher for namespace %s", ns)
			if err := w.Stop(); err != nil {
				logrus.Warnf("Error while trying to remove CRD watcher: %s", err)
			}

			delete(n.CrdWatchers, ns)
		}
	}
}

func (n *NamespaceWatcher) setRunning(v bool) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	n.Running = v
}

func (n *NamespaceWatcher) startCrdWatcher(ctx context.Context, namespace string, wg *sync.WaitGroup) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	wg.Add(1)
	watcher := n.CrdWatchers[namespace]

	go func() {
		defer wg.Done()

		if watcher == nil {
			logrus.Warnf("No watcher found for namespace %s", namespace)
			return
		}

		if err := watcher.Start(ctx); err != nil {
			logrus.Errorf("Error while starting CRD Watcher for namespace %s", namespace)
		}
	}()
}
