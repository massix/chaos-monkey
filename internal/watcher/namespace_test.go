package watcher_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	typedcmc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	fakecmc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned/fake"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	k "k8s.io/client-go/kubernetes"
	kubernetes "k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
)

type FakeCrdWatcher struct {
	*sync.Mutex
	StartedTimes int
	StoppedTimes int
	Running      bool
}

// IsRunning implements watcher.Watcher.
func (f *FakeCrdWatcher) IsRunning() bool {
	f.Lock()
	defer f.Unlock()

	return f.Running
}

// Start implements watcher.Watcher.
func (f *FakeCrdWatcher) Start(ctx context.Context) error {
	f.Lock()
	defer f.Unlock()

	f.Running = true
	f.StartedTimes++
	return nil
}

// Stop implements watcher.Watcher.
func (f *FakeCrdWatcher) Stop() error {
	f.Lock()
	defer f.Unlock()

	f.Running = false
	f.StoppedTimes++
	return nil
}

var cmcClientset = fakecmc.NewSimpleClientset()

func TestNamespaceWatcher_Create(t *testing.T) {
	w := watcher.DefaultNamespaceFactory(kubernetes.NewSimpleClientset(), cmcClientset, record.NewFakeRecorder(1024), "chaos-monkey")
	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}
}

func TestNamespaceWatcher_BasicBehaviour(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientSet := kubernetes.NewSimpleClientset()
	w := watcher.DefaultNamespaceFactory(clientSet, cmcClientset, record.NewFakeRecorder(1024), "chaos-monkey").(*watcher.NamespaceWatcher)
	w.CleanupTimeout = 1 * time.Second

	// Inject my CRD Factory
	watcher.DefaultCrdFactory = func(k.Interface, typedcmc.Interface, record.EventRecorderLogger, string) watcher.Watcher {
		return &FakeCrdWatcher{Mutex: &sync.Mutex{}}
	}

	// Create the scenario
	clientSet.PrependWatchReactor("namespaces", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()

		go func() {
			createNamespace := func(name string) *corev1.Namespace {
				return &corev1.Namespace{
					ObjectMeta: v1.ObjectMeta{Name: name},
				}
			}

			fakeWatch.Add(createNamespace("test-1"))
			fakeWatch.Add(createNamespace("test-2"))
			fakeWatch.Add(createNamespace("test-3"))
			fakeWatch.Modify(createNamespace("test-3"))
			fakeWatch.Delete(createNamespace("test-2"))
		}()
		return true, fakeWatch, nil
	})

	// Now start the watcher in background
	done := make(chan struct{}, 1)
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- struct{}{}
	}()

	// Wait for all the events to be processed
	time.Sleep(1 * time.Second)

	// I should have two running watchers
	w.Mutex.Lock()
	if cnt := len(w.CrdWatchers); cnt != 2 {
		t.Fatalf("Expected 2 watchers, got %d", cnt)
	}

	// Foreach watcher, we should have 1 Start and 0 Stop called
	for ns, crd := range w.CrdWatchers {
		crd := crd.(*FakeCrdWatcher)

		crd.Mutex.Lock()
		t.Logf("Watcher: %s, Start: %d, Stop: %d", ns, crd.StartedTimes, crd.StoppedTimes)
		if s := crd.StartedTimes; s != 1 {
			t.Errorf("Expected 1 Start, got %d", s)
		}
		if s := crd.StoppedTimes; s != 0 {
			t.Errorf("Expected 0 Stop, got %d", s)
		}
		crd.Mutex.Unlock()

	}
	w.Mutex.Unlock()

	// Now stop the watcher
	t.Log("Stopping watcher")
	if err := w.Stop(); err != nil {
		t.Error(err)
	}

	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}

	t.Log("Waiting for the watcher to terminate")
	<-done

	// We should have 0 watchers remaining
	if cnt := len(w.CrdWatchers); cnt != 0 {
		t.Fatalf("Expected 2 watchers, got %d", cnt)
	}
}

func TestNamespaceWatcher_Error(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := kubernetes.NewSimpleClientset()
	w := watcher.DefaultNamespaceFactory(clientset, cmcClientset, record.NewFakeRecorder(1024), "chaos-monkey")

	clientset.PrependWatchReactor("namespaces", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()

		go func() {
			fakeWatch.Error(&corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: "test",
				},
			})
		}()
		return true, fakeWatch, nil
	})

	// Now start the watcher
	done := make(chan struct{}, 1)
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "Empty event or error from namespace watcher") {
			t.Errorf("Expected error, got %+v instead", err)
		} else {
			t.Logf("Expected: %s", err)
		}

		done <- struct{}{}
	}()

	// Wait for all the events to be processed
	time.Sleep(1 * time.Second)
	<-done

	// The watcher should be stopped already
	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}
}

func TestNamespaceWatcher_Cleanup(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := kubernetes.NewSimpleClientset()
	w := watcher.DefaultNamespaceFactory(clientset, cmcClientset, record.NewFakeRecorder(1024), "chaos-monkey").(*watcher.NamespaceWatcher)
	w.CleanupTimeout = 1 * time.Second

	// Add some fake watchers
	w.CrdWatchers = map[string]watcher.Watcher{
		"test-1": &FakeCrdWatcher{Mutex: &sync.Mutex{}, Running: true},
		"test-2": &FakeCrdWatcher{Mutex: &sync.Mutex{}, Running: false},
		"test-3": &FakeCrdWatcher{Mutex: &sync.Mutex{}, Running: false},
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Now start the watcher
	done := make(chan struct{}, 1)
	defer close(done)

	go func() {
		if err := w.Start(ctx); err != nil {
			t.Error(err)
		}

		done <- struct{}{}
	}()

	// Wait for the clean timeout to happen
	time.Sleep(2 * time.Second)

	w.Mutex.Lock()
	// The NamespaceWatcher should have removed all the stopped watchers
	if len(w.CrdWatchers) != 1 {
		t.Errorf("Expected 0 watchers, got %d", len(w.CrdWatchers))
	}
	w.Mutex.Unlock()

	// Now cancel the context
	cancel()

	// Wait for the watcher to stop correctly
	<-done

	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}

	// In the end, all the remaining CrdWatchers should be removed
	if len(w.CrdWatchers) != 0 {
		t.Errorf("Expected 0 watchers, got %d", len(w.CrdWatchers))
	}
}

func TestNamespaceWatcher_RestartWatcher(t *testing.T) {
	clientset := kubernetes.NewSimpleClientset()
	w := watcher.DefaultNamespaceFactory(clientset, cmcClientset, record.NewFakeRecorder(1024), "chaos-monkey")
	w.(*watcher.NamespaceWatcher).CleanupTimeout = 1 * time.Second
	timeAsked := &atomic.Int32{}
	timeAsked.Store(0)

	watcher.DefaultCrdFactory = func(clientset k.Interface, cmcClientset typedcmc.Interface, recorder record.EventRecorderLogger, namespace string) watcher.Watcher {
		return &FakeCrdWatcher{Mutex: &sync.Mutex{}}
	}

	clientset.PrependWatchReactor("namespaces", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()
		timeAsked.Add(1)

		go func() {
			for i := range [10]int{} {
				fakeWatch.Add(&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: fmt.Sprintf("test-%d", i)}})
				time.Sleep(100 * time.Millisecond)
			}

			fakeWatch.Stop()
		}()
		return true, fakeWatch, nil
	})

	// Now start the watcher
	done := make(chan interface{})
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- nil
	}()

	// Now wait, if the namespace watcher doesn't terminate with an error, all is good!
	time.Sleep(5 * time.Second)
	_ = w.Stop()

	// We should have 5 restarts (5 calls to the Watch method)
	if timeAsked.Load() != 5 {
		t.Errorf("Expected 5 restarts, got %d", timeAsked)
	}

	<-done
}
