package watcher_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cmc "github.com/massix/chaos-monkey/internal/apis/clientset/versioned/fake"
	v1 "github.com/massix/chaos-monkey/internal/apis/v1"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	k "k8s.io/client-go/kubernetes"
	kubernetes "k8s.io/client-go/kubernetes/fake"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	ktest "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	fakemetrics "k8s.io/metrics/pkg/client/clientset/versioned/fake"
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

func (f *FakeDeploymentWatcher) Close() error {
	return nil
}

var _ watcher.ConfigurableWatcher = &FakeDeploymentWatcher{}

func TestCRDWatcher_Create(t *testing.T) {
	w := watcher.NewCrdWatcher(kubernetes.NewSimpleClientset(), cmc.NewSimpleClientset(), fakemetrics.NewSimpleClientset(), record.NewFakeRecorder(1024), "chaos-monkey")
	defer w.Close()

	if w.IsRunning() {
		t.Fail()
	}
}

func createCMC(name string, enabled bool, scalingMode v1.ScalingMode, minReplicas, maxReplicas int, deploymentName string, timeout time.Duration) *v1.ChaosMonkeyConfiguration {
	return &v1.ChaosMonkeyConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ChaosMonkeyConfigurationSpec{
			Enabled:     enabled,
			MinReplicas: minReplicas,
			MaxReplicas: maxReplicas,
			Timeout:     timeout,
			ScalingMode: scalingMode,
			Deployment:  v1.ChaosMonkeyConfigurationSpecDeployment{Name: deploymentName},
		},
	}
}

func TestCRDWatcher_BasicBehaviour(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientSet := kubernetes.NewSimpleClientset()
	cmClientset := cmc.NewSimpleClientset()
	metricsClientset := fakemetrics.NewSimpleClientset()
	w := watcher.NewCrdWatcher(clientSet, cmClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey")
	w.CleanupTimeout = 1 * time.Second

	// Inject my Deployment Factory
	watcher.DefaultDeploymentFactory = func(_ k.Interface, _ record.EventRecorderLogger, dep *appsv1.Deployment) watcher.ConfigurableWatcher {
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}, DeploymentName: dep.Name, IsPodMode: false}
	}

	// Inject my Pod Factory
	watcher.DefaultPodFactory = func(clientset k.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) watcher.ConfigurableWatcher {
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}, DeploymentName: namespace, IsPodMode: true}
	}

	// Inject my AntiHPA Factory
	watcher.DefaultAntiHPAFactory = func(client metricsv.Interface, podset typedcorev1.PodInterface, namespace, podLabel string) watcher.ConfigurableWatcher {
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}, DeploymentName: namespace, IsPodMode: true}
	}

	// Create the scenario
	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()

		go func() {
			fakeWatch.Add(createCMC("test-1", true, v1.ScalingModeRandomScale, 1, 1, "test-1", 1*time.Second))
			fakeWatch.Add(createCMC("test-2", false, v1.ScalingModeKillPod, 1, 1, "test-2", 10*time.Second))
			fakeWatch.Add(createCMC("test-3", true, v1.ScalingModeAntiPressure, 1, 1, "test-3", 10*time.Minute))
			fakeWatch.Modify(createCMC("test-1", true, v1.ScalingModeRandomScale, 4, 8, "test-1", 1*time.Second))
			fakeWatch.Delete(createCMC("test-2", true, v1.ScalingModeRandomScale, 4, 8, "test-2", 1*time.Second))
		}()

		return true, fakeWatch, nil
	})

	// Setup the scenario for the deployments too
	clientSet.PrependReactor("get", "deployments", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		askedDeployment := action.(ktest.GetAction).GetName()
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
				check(crd.Watcher.(*FakeDeploymentWatcher))
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

	// The deployment for "test-3" should have the default timeout of 10 minutes
	if !testDW("test-3", func(d *FakeDeploymentWatcher) {
		if d.Timeout != 10*time.Minute {
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
	metricsClientset := fakemetrics.NewSimpleClientset()
	w := watcher.NewCrdWatcher(clientSet, cmClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey")
	w.CleanupTimeout = 1 * time.Second

	// Setup the scenario for the CMCs
	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()
		go func() {
			fakeWatch.Error(&v1.ChaosMonkeyConfiguration{
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
	metricsClientset := fakemetrics.NewSimpleClientset()
	w := watcher.NewCrdWatcher(clientSet, cmClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey")
	w.CleanupTimeout = 1 * time.Second

	// Inject some FakeDeploymentWatchers inside the watcher itself
	w.DeploymentWatchers = map[string]*watcher.WatcherConfiguration{
		"test-1": {Configuration: nil, Watcher: &FakeDeploymentWatcher{Running: true, Mutex: &sync.Mutex{}}},
		"test-2": {Configuration: nil, Watcher: &FakeDeploymentWatcher{Running: false, Mutex: &sync.Mutex{}}},
		"test-3": {Configuration: nil, Watcher: &FakeDeploymentWatcher{Running: false, Mutex: &sync.Mutex{}}},
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

func TestCRDWatcher_Restart(t *testing.T) {
	clientSet := kubernetes.NewSimpleClientset()
	cmClientset := cmc.NewSimpleClientset()
	metricsClientset := fakemetrics.NewSimpleClientset()
	w := watcher.NewCrdWatcher(clientSet, cmClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey")
	w.CleanupTimeout = 1 * time.Second
	timesRestarted := &atomic.Int32{}
	timesRestarted.Store(0)

	watcher.DefaultDeploymentFactory = func(clientset k.Interface, recorder record.EventRecorderLogger, deployment *appsv1.Deployment) watcher.ConfigurableWatcher {
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}}
	}

	// Setup the scenario for the CMCs
	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()
		timesRestarted.Add(1)

		go func() {
			for i := range [10]int{} {
				depName := fmt.Sprintf("test-%d", i)
				fakeWatch.Add(createCMC(depName, false, v1.ScalingModeRandomScale, 0, 10, depName, 10*time.Second))
				time.Sleep(100 * time.Millisecond)
			}

			fakeWatch.Stop()
		}()
		return true, fakeWatch, nil
	})

	clientSet.PrependReactor("get", "deployments", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		requestedName := action.(ktest.GetAction).GetName()
		return true, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: requestedName},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": requestedName},
				},
			},
		}, nil
	})

	// Start the watcher in background
	done := make(chan interface{})
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- nil
	}()

	time.Sleep(4300 * time.Millisecond)

	// It should still be running
	w.Mutex.Lock()
	if !w.Running {
		t.Error("Watcher should be running")
	}
	w.Mutex.Unlock()

	// Now stop it and verify that the watcher restarted 5 times
	if err := w.Stop(); err != nil {
		t.Error(err)
	}
	<-done

	if timesRestarted.Load() != 5 {
		t.Errorf("Expected 5 restarts, got %d", timesRestarted.Load())
	}
}

func TestCRDWatcher_ModifyWatcherType(t *testing.T) {
	clientSet := kubernetes.NewSimpleClientset()
	cmClientset := cmc.NewSimpleClientset()
	metricsClientset := fakemetrics.NewSimpleClientset()
	w := watcher.NewCrdWatcher(clientSet, cmClientset, metricsClientset, record.NewFakeRecorder(1024), "chaos-monkey")
	w.CleanupTimeout = 1 * time.Second

	// Number of times each watcher has been created
	podWatchers := &atomic.Int32{}
	deployWatchers := &atomic.Int32{}
	podWatchers.Store(0)
	deployWatchers.Store(0)

	fakeWatch := watch.NewFake()

	watcher.DefaultDeploymentFactory = func(clientset k.Interface, recorder record.EventRecorderLogger, deployment *appsv1.Deployment) watcher.ConfigurableWatcher {
		deployWatchers.Add(1)
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}}
	}

	watcher.DefaultPodFactory = func(clientset k.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) watcher.ConfigurableWatcher {
		podWatchers.Add(1)
		return &FakeDeploymentWatcher{Mutex: &sync.Mutex{}}
	}

	clientSet.PrependReactor("get", "deployments", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		requestedName := action.(ktest.GetAction).GetName()
		return true, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: requestedName},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": requestedName}},
			},
		}, nil
	})

	cmClientset.PrependWatchReactor("chaosmonkeyconfigurations", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		go func() {
			fakeWatch.Add(createCMC("test-deploy", false, v1.ScalingModeRandomScale, 0, 10, "test-deploy", 10*time.Second))
			fakeWatch.Add(createCMC("test-pod", false, v1.ScalingModeKillPod, 0, 10, "test-pod", 10*time.Second))
		}()

		return true, fakeWatch, nil
	})

	done := make(chan interface{})
	defer close(done)

	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- nil
	}()

	// Wait for the events to be processed
	time.Sleep(500 * time.Millisecond)

	// We should have 2 watchers
	w.Mutex.Lock()
	if cnt := len(w.DeploymentWatchers); cnt != 2 {
		t.Errorf("Expected 2 watchers, got %d", cnt)
	}
	if podWatchers.Load() != 1 {
		t.Errorf("Expected 1 pod watcher, got %d", podWatchers.Load())
	}
	if deployWatchers.Load() != 1 {
		t.Errorf("Expected 1 deployment watcher, got %d", deployWatchers.Load())
	}

	w.Mutex.Unlock()

	// Now send a Modify event
	fakeWatch.Modify(createCMC("test-deploy", false, v1.ScalingModeKillPod, 0, 10, "test-deploy", 10*time.Second))
	time.Sleep(100 * time.Millisecond)

	// We should still have 2 watchers
	w.Mutex.Lock()
	if cnt := len(w.DeploymentWatchers); cnt != 2 {
		t.Errorf("Expected 2 watchers, got %d", cnt)
	}

	// This time we should have 2 podwatchers created
	if podWatchers.Load() != 2 {
		t.Errorf("Expected 2 pod watchers, got %d", podWatchers.Load())
	}

	// But still only 1 deploywatchers
	if deployWatchers.Load() != 1 {
		t.Errorf("Expected 1 deployment watcher, got %d", deployWatchers.Load())
	}
	w.Mutex.Unlock()

	// Now send another Modify event
	fakeWatch.Modify(createCMC("test-pod", false, v1.ScalingModeRandomScale, 0, 10, "test-pod", 10*time.Second))
	time.Sleep(100 * time.Millisecond)

	// Still 2 watchers
	w.Mutex.Lock()
	if cnt := len(w.DeploymentWatchers); cnt != 2 {
		t.Errorf("Expected 2 watchers, got %d", cnt)
	}

	// Now both should have 2 calls
	if podWatchers.Load() != 2 {
		t.Errorf("Expected 2 pod watchers, got %d", podWatchers.Load())
	}

	if deployWatchers.Load() != 2 {
		t.Errorf("Expected 2 deployment watchers, got %d", deployWatchers.Load())
	}
	w.Mutex.Unlock()

	_ = w.Stop()
	<-done
}
