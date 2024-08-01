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
	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	k "k8s.io/client-go/kubernetes"
	kubernetes "k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	fakemetricsv "k8s.io/metrics/pkg/client/clientset/versioned/fake"
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

func (f *FakeCrdWatcher) Close() error {
	return nil
}

var cmcClientset = fakecmc.NewSimpleClientset()

func TestNamespaceWatcher_Create(t *testing.T) {
	metricsClientset := fakemetricsv.NewSimpleClientset()
	w := watcher.NewNamespaceWatcher(kubernetes.NewSimpleClientset(), cmcClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey", configuration.BehaviorAllowAll)
	defer w.Close()

	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}
}

func TestNamespaceWatcher_BasicBehaviour(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientSet := kubernetes.NewSimpleClientset()
	metricsClientset := fakemetricsv.NewSimpleClientset()
	w := watcher.NewNamespaceWatcher(clientSet, cmcClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey", configuration.BehaviorAllowAll)
	w.CleanupTimeout = 1 * time.Second

	// Inject my CRD Factory
	watcher.DefaultCrdFactory = func(k.Interface, typedcmc.Interface, metricsv.Interface, record.EventRecorderLogger, string) watcher.Watcher {
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
	for _, crd := range w.CrdWatchers {
		crd := crd.(*FakeCrdWatcher)

		crd.Mutex.Lock()
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
	if err := w.Stop(); err != nil {
		t.Error(err)
	}

	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}

	<-done

	// We should have 0 watchers remaining
	if cnt := len(w.CrdWatchers); cnt != 0 {
		t.Fatalf("Expected 2 watchers, got %d", cnt)
	}
}

func TestNamespaceWatcher_Error(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := kubernetes.NewSimpleClientset()
	metricsClientset := fakemetricsv.NewSimpleClientset()
	w := watcher.NewNamespaceWatcher(clientset, cmcClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey", configuration.BehaviorAllowAll)

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
	metricsClientset := fakemetricsv.NewSimpleClientset()
	w := watcher.NewNamespaceWatcher(clientset, cmcClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey", configuration.BehaviorAllowAll)
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
	metricsClientset := fakemetricsv.NewSimpleClientset()
	w := watcher.NewNamespaceWatcher(clientset, cmcClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey", configuration.BehaviorAllowAll)
	w.CleanupTimeout = 1 * time.Second
	timeAsked := &atomic.Int32{}
	timeAsked.Store(0)

	watcher.DefaultCrdFactory = func(clientset k.Interface, cmcClientset typedcmc.Interface, _ metricsv.Interface, recorder record.EventRecorderLogger, namespace string) watcher.Watcher {
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

func TestNamespaceWatcher_ModifyNamespace(t *testing.T) {
	fakeWatch := watch.NewFake()
	clientset := kubernetes.NewSimpleClientset()

	clientset.PrependWatchReactor("namespaces", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		return true, fakeWatch, nil
	})

	watcher.DefaultCrdFactory = func(clientset k.Interface, cmcClientset typedcmc.Interface, _ metricsv.Interface, recorder record.EventRecorderLogger, namespace string) watcher.Watcher {
		return &FakeCrdWatcher{Mutex: &sync.Mutex{}}
	}

	nsWithLabel := func(name, label string) *corev1.Namespace {
		lbl := map[string]string{
			"cm.massix.github.io/namespace": label,
		}

		return &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{Name: name, Labels: lbl},
		}
	}

	metricsClientset := fakemetricsv.NewSimpleClientset()
	w := watcher.NewNamespaceWatcher(clientset, nil, metricsClientset, record.NewFakeRecorder(1024), "chaosmonkey", configuration.BehaviorAllowAll)
	w.WatcherTimeout = 24 * time.Hour
	w.CleanupTimeout = 300 * time.Millisecond

	done := make(chan interface{})
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- nil
	}()

	checkWatchers := func(t *testing.T, num int) {
		w.Mutex.Lock()
		defer w.Mutex.Unlock()

		if cnt := len(w.CrdWatchers); cnt != num {
			t.Errorf("Expected %d watchers, got %d", num, cnt)
		}
	}

	t.Run("AllowAll", func(t *testing.T) {
		t.Run("ADD", func(t *testing.T) {
			go func() {
				// These should all pass
				fakeWatch.Add(nsWithLabel("test-ok-1", "blabla"))
				fakeWatch.Add(nsWithLabel("test-ok-2", "true"))
				fakeWatch.Add(&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "test-ok-3"}})
			}()

			// We should have exactly 3 watchers registered
			time.Sleep(300 * time.Millisecond)
			checkWatchers(t, 3)

			go func() {
				// These should all be rejected
				fakeWatch.Add(nsWithLabel("test-ko-1", "false"))
				fakeWatch.Add(nsWithLabel("test-ko-2", "false"))
				fakeWatch.Add(nsWithLabel("test-ko-3", "false"))
			}()

			time.Sleep(300 * time.Millisecond)
			checkWatchers(t, 3)
		})

		t.Run("MODIFY", func(t *testing.T) {
			go func() {
				// Modifying an existing watcher should remove it from the list
				fakeWatch.Modify(nsWithLabel("test-ok-1", "false"))

				// Modifying a non existing watcher should not panic
				fakeWatch.Modify(nsWithLabel("test-notexisting", "false"))
			}()

			time.Sleep(300 * time.Millisecond)
			checkWatchers(t, 2)
		})
	})

	// Change the behavior to "DenyAll" and reset the watcher
	w.Mutex.Lock()
	w.Behavior = configuration.BehaviorDenyAll
	w.CrdWatchers = map[string]watcher.Watcher{}
	w.Mutex.Unlock()

	t.Run("DenyAll", func(t *testing.T) {
		t.Run("ADD", func(t *testing.T) {
			go func() {
				// These should all be rejected
				fakeWatch.Add(nsWithLabel("test-ko-1", "blabla"))
				fakeWatch.Add(nsWithLabel("test-ko-2", "false"))
				fakeWatch.Add(&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "test-ko-3"}})
			}()

			time.Sleep(300 * time.Millisecond)
			checkWatchers(t, 0)

			go func() {
				// These should all be accepted
				fakeWatch.Add(nsWithLabel("test-ok-1", "true"))
				fakeWatch.Add(nsWithLabel("test-ok-2", "true"))
				fakeWatch.Add(nsWithLabel("test-ok-3", "true"))
			}()

			time.Sleep(300 * time.Millisecond)
			checkWatchers(t, 3)
		})

		t.Run("MODIFY", func(t *testing.T) {
			go func() {
				// Modifying an existing watcher should remove it from the list
				fakeWatch.Modify(nsWithLabel("test-ok-1", "blabla"))
				fakeWatch.Modify(nsWithLabel("test-ok-2", "false"))
				fakeWatch.Modify(&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "test-ok-3"}})

				// Modifying a watcher which is not in the list should not panic
				fakeWatch.Modify(nsWithLabel("test-notexisting", "false"))
				fakeWatch.Modify(&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "test-notexisting-2"}})
			}()

			time.Sleep(300 * time.Millisecond)
			checkWatchers(t, 0)
		})
	})

	_ = w.Stop()
	<-done
}
