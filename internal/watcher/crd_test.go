package watcher_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	cmc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned/fake"
	"github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	k "k8s.io/client-go/kubernetes"
	kubernetes "k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
)

type FakeDeploymentWatcher struct {
	Mutex          *sync.Mutex
	DeploymentName string
	MinReplicas    int
	MaxReplicas    int
	Timeout        time.Duration
	Running        bool
	Enabled        bool
	IsPodMode      bool
}

// IsRunning implements watcher.DeploymentWatcherI.
func (f *FakeDeploymentWatcher) IsRunning() bool {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	return f.Running
}

// SetEnabled implements watcher.DeploymentWatcherI.
func (f *FakeDeploymentWatcher) SetEnabled(v bool) {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	f.Enabled = v
}

// SetMaxReplicas implements watcher.DeploymentWatcherI.
func (f *FakeDeploymentWatcher) SetMaxReplicas(v int) {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	f.MaxReplicas = v
}

// SetMinReplicas implements watcher.DeploymentWatcherI.
func (f *FakeDeploymentWatcher) SetMinReplicas(v int) {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	f.MinReplicas = v
}

// SetTimeout implements watcher.DeploymentWatcherI.
func (f *FakeDeploymentWatcher) SetTimeout(v time.Duration) {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	f.Timeout = v
}

// Start implements watcher.DeploymentWatcherI.
func (f *FakeDeploymentWatcher) Start(ctx context.Context) error {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	f.Running = true
	return nil
}

// Stop implements watcher.DeploymentWatcherI.
func (f *FakeDeploymentWatcher) Stop() error {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	f.Running = false
	return nil
}

var _ watcher.ConfigurableWatcher = &FakeDeploymentWatcher{}

func TestCRDWatcher_Create(t *testing.T) {
	w := watcher.DefaultCrdFactory(kubernetes.NewSimpleClientset(), cmc.NewSimpleClientset(), record.NewFakeRecorder(1024), "chaos-monkey")
	if w.IsRunning() {
		t.Fail()
	}
}

func TestCRDWatcher_BasicBehaviour(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientSet := kubernetes.NewSimpleClientset()
	cmClientset := cmc.NewSimpleClientset()
	w := watcher.DefaultCrdFactory(clientSet, cmClientset, record.NewFakeRecorder(1024), "chaos-monkey").(*watcher.CrdWatcher)
	w.CleanupTimeout = 1 * time.Second

	// Inject my Deployment Factory
	watcher.DefaultDeploymentFactory = func(_ k.Interface, _ record.EventRecorderLogger, dep *appsv1.Deployment) watcher.ConfigurableWatcher {
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}, DeploymentName: dep.Name, IsPodMode: false}
	}

	// Inject my Pod Factory
	watcher.DefaultPodFactory = func(clientset k.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) watcher.ConfigurableWatcher {
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}, DeploymentName: namespace, IsPodMode: true}
	}

	// Create the scenario
	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()

		go func() {
			createCMC := func(name string, enabled, podMode bool, minReplicas, maxReplicas int, deploymentName, timeout string) *v1alpha1.ChaosMonkeyConfiguration {
				return &v1alpha1.ChaosMonkeyConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: v1alpha1.ChaosMonkeyConfigurationSpec{
						Enabled:        enabled,
						MinReplicas:    minReplicas,
						MaxReplicas:    maxReplicas,
						DeploymentName: deploymentName,
						Timeout:        timeout,
						PodMode:        podMode,
					},
				}
			}

			fakeWatch.Add(createCMC("test-1", true, false, 1, 1, "test-1", "1s"))
			fakeWatch.Add(createCMC("test-2", false, true, 1, 1, "test-2", "10s"))
			fakeWatch.Add(createCMC("test-3", true, true, 1, 1, "test-3", "invalidstring"))
			fakeWatch.Modify(createCMC("test-1", true, false, 4, 8, "test-1", "1s"))
			fakeWatch.Delete(createCMC("test-2", true, false, 4, 8, "test-2", "1s"))
		}()

		return true, fakeWatch, nil
	})

	// Setup the scenario for the deployments too
	clientSet.PrependReactor("get", "deployments", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		askedDeployment := action.(ktest.GetAction).GetName()
		t.Logf("Asked deployment %s", askedDeployment)
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: askedDeployment,
			},

			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": askedDeployment},
				},
			},
		}

		return true, dep, nil
	})

	// We can now start the watcher
	done := make(chan struct{})
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- struct{}{}
	}()

	// Give it some time
	time.Sleep(3 * time.Second)

	// We should have 2 running watchers
	w.Mutex.Lock()
	if cnt := len(w.DeploymentWatchers); cnt != 2 {
		t.Fatalf("Expected 2 watchers, got %d", cnt)
	}

	testDW := func(depName string, check func(d *FakeDeploymentWatcher)) bool {
		found := false

		for key, crd := range w.DeploymentWatchers {
			if key == depName {
				found = true
				check(crd.(*FakeDeploymentWatcher))
			}
		}

		return found
	}

	// The deployment for "test-1" should have 4-8 replicas
	if !testDW("test-1", func(d *FakeDeploymentWatcher) {
		if d.MinReplicas != 4 || d.MaxReplicas != 8 {
			t.Errorf("Expected 4-8 replicas, got %d-%d", d.MinReplicas, d.MaxReplicas)
		}
	}) {
		t.Error("Deployment for test-1 not found")
	}

	// The deployment for "test-3" should have the default timeout of 5 minutes
	if !testDW("test-3", func(d *FakeDeploymentWatcher) {
		if d.Timeout != 5*time.Minute {
			t.Errorf("Expected 5 minutes timeout, got %s", d.Timeout)
		}
	}) {
		t.Error("Deployment for test-3 not found")
	}

	w.Mutex.Unlock()

	// Stop the watcher now
	if err := w.Stop(); err != nil {
		t.Error(err)
	}

	<-done

	if w.IsRunning() {
		t.Error("Watcher should be stopped")
	}

	// After the watcher is stopped, we should have 0 watchers
	if cnt := len(w.DeploymentWatchers); cnt != 0 {
		t.Errorf("Expected 0 watchers, got %d", cnt)
	}
}

func TestCRDWatcher_Error(t *testing.T) {
	clientSet := kubernetes.NewSimpleClientset()
	cmClientset := cmc.NewSimpleClientset()
	w := watcher.DefaultCrdFactory(clientSet, cmClientset, record.NewFakeRecorder(1024), "chaos-monkey").(*watcher.CrdWatcher)
	w.CleanupTimeout = 1 * time.Second

	// Setup the scenario for the CMCs
	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()
		go func() {
			fakeWatch.Error(&v1alpha1.ChaosMonkeyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			})
		}()
		return true, fakeWatch, nil
	})

	// Now start the watcher
	done := make(chan struct{})
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "Empty event or error from CRD watcher") {
			t.Errorf("Expected error, got %+v instead", err)
		} else {
			t.Logf("Expected: %s", err)
		}

		done <- struct{}{}
	}()

	time.Sleep(1 * time.Second)
	<-done

	// The watcher should be stopped already
	if w.IsRunning() {
		t.Error("Watcher should be stopped")
	}
}

func TestCRDWatcher_Cleanup(t *testing.T) {
	clientSet := kubernetes.NewSimpleClientset()
	cmClientset := cmc.NewSimpleClientset()
	w := watcher.DefaultCrdFactory(clientSet, cmClientset, record.NewFakeRecorder(1024), "chaos-monkey").(*watcher.CrdWatcher)
	w.CleanupTimeout = 1 * time.Second

	// Inject some FakeDeploymentWatchers inside the watcher itself
	w.DeploymentWatchers = map[string]watcher.ConfigurableWatcher{
		"test-1": &FakeDeploymentWatcher{Running: true, Mutex: &sync.Mutex{}},
		"test-2": &FakeDeploymentWatcher{Running: false, Mutex: &sync.Mutex{}},
		"test-3": &FakeDeploymentWatcher{Running: false, Mutex: &sync.Mutex{}},
	}

	// Setup the scenario for the CMCs
	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()
		return true, fakeWatch, nil
	})

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start the watcher in background using the cancellable context
	done := make(chan struct{})
	defer close(done)

	go func() {
		if err := w.Start(ctx); err != nil {
			t.Error(err)
		}

		done <- struct{}{}
	}()

	// Wait for the cleanup to happen
	time.Sleep(2 * time.Second)

	w.Mutex.Lock()

	// We should have only 1 watcher
	if len(w.DeploymentWatchers) != 1 {
		t.Errorf("Expected 1 watcher, got %d", len(w.DeploymentWatchers))
	}

	// That watcher should be for "test-1"
	if _, ok := w.DeploymentWatchers["test-1"]; !ok {
		t.Error("Watcher for test-1 not found")
	}

	w.Mutex.Unlock()

	// Now cancel the context, wait for the goroutine to finish and check that the watcher is no longer running
	cancel()

	<-done

	if w.IsRunning() {
		t.Error("Watcher should be stopped")
	}
}

func TestCRDWatcher_NilObject(t *testing.T) {
	clientSet := kubernetes.NewSimpleClientset()
	cmClientset := cmc.NewSimpleClientset()
	w := watcher.DefaultCrdFactory(clientSet, cmClientset, record.NewFakeRecorder(1024), "chaos-monkey").(*watcher.CrdWatcher)
	w.CleanupTimeout = 300 * time.Millisecond

	// Setup the scenario for the CMCs
	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()
		go func() {
			fakeWatch.Add(nil)
		}()
		return true, fakeWatch, nil
	})

	// Now start the watcher
	done := make(chan struct{})
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- struct{}{}
	}()

	time.Sleep(1 * time.Second)

	// Despite the nil event the watcher is still running
	if !w.IsRunning() {
		t.Error("Watcher stopped")
	}

	_ = w.Stop()

	<-done
}
