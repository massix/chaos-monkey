package watcher_test

import (
	"context"
	"fmt"
	"sync/atomic"
	gtest "testing"
	"time"

	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
)

func createPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": "name"},
		},
	}
}

func TestPodWatcher_Create(t *gtest.T) {
	clientset := fake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)

	p := watcher.NewPodWatcher(clientset, recorder, "test", "app=name")
	if p == nil {
		t.Fatal("Failed to create pod watcher")
	}

	defer p.Close()

	if p.Timeout != 30*time.Second {
		t.Errorf("Expected timeout to be 30 seconds, got %s", p.Timeout)
	}

	if p.Running {
		t.Error("Expected running to be false")
	}

	p.SetTimeout(1 * time.Second)
	if p.Timeout != 1*time.Second {
		t.Errorf("Expected timeout to be 1 second, got %s", p.Timeout)
	}
}

func TestPodWatcher_BasicBehaviour(t *gtest.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := fake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)
	p := watcher.NewPodWatcher(clientset, recorder, "test", "app=name")

	pause := make(chan interface{})
	defer close(pause)
	done := make(chan interface{})

	clientset.PrependWatchReactor("pods", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		fakeWatch := watch.NewFake()
		go func() {
			fakeWatch.Add(createPod("test"))
			fakeWatch.Add(createPod("test-1"))
			fakeWatch.Add(createPod("test-2"))

			// Wait for the test to do the first assertions
			<-pause

			// This object is not in the list, so it should not cause any movements
			fakeWatch.Delete(createPod("test-4"))
			fakeWatch.Delete(createPod("test"))
			fakeWatch.Delete(createPod("test-1"))

			// Wait for the test to do the second assertions
			<-pause

			// Send some Modified events, which should resolve to no-op
			fakeWatch.Modify(createPod("test-2"))
			fakeWatch.Modify(createPod("test-3"))
		}()

		return true, fakeWatch, nil
	})

	clientset.PrependReactor("delete", "pods", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, nil
	})

	p.SetTimeout(100 * time.Millisecond)

	go func() {
		if err := p.Start(context.Background()); err != nil {
			t.Error(err)
		}

		// Signal that the test is over
		done <- nil
		close(done)
	}()

	// Wait for the first batch of events to be processed
	time.Sleep(500 * time.Millisecond)

	// At this point, we should have 3 objects in the list
	p.Mutex.Lock()

	if len(p.PodList) != 3 {
		t.Errorf("Expected 2 pods, got %d", len(p.PodList))
	}

	p.Mutex.Unlock()

	// Signal that we can continue with the test
	pause <- nil

	// Wait for the second batch of events to be processed
	time.Sleep(500 * time.Millisecond)

	// At this point we should have only one element left in the list
	p.Mutex.Lock()

	if len(p.PodList) != 1 {
		t.Errorf("Expected 1 pod, got %d", len(p.PodList))
	}

	p.Mutex.Unlock()

	// Signal that we can continue with the test
	pause <- nil

	// Wait for the third batch of events to be processed
	time.Sleep(500 * time.Millisecond)

	// At this point we should still have a single element in the list
	p.Mutex.Lock()

	if len(p.PodList) != 1 {
		t.Errorf("Expected 1 pod, got %d", len(p.PodList))
	}

	p.Mutex.Unlock()

	// We can stop here
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	// Wait for the test to finish
	<-done
}

func TestPodWatcher_DeletePods(t *gtest.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := fake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)
	p := watcher.NewPodWatcher(clientset, recorder, "test", "app=name")
	fakeWatch := watch.NewFake()

	p.SetTimeout(100 * time.Millisecond)
	p.SetEnabled(true)

	podsAdded := make(chan interface{})
	done := make(chan interface{})

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	clientset.PrependWatchReactor("pods", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		go func() {
			// Add a bunch of pods at regular intervals
			for i := range [10]int{} {
				fakeWatch.Add(createPod(fmt.Sprintf("test-%d", i+1)))
				time.Sleep(50 * time.Millisecond)
			}

			// Signal that we have finished sending the Add events
			podsAdded <- nil
			close(podsAdded)
		}()
		return true, fakeWatch, nil
	})

	clientset.PrependReactor("delete", "pods", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		podName := action.(ktest.DeleteAction).GetName()

		// We can delete the first 5 pods, not the other ones
		switch podName {
		case "test-1", "test-2", "test-3", "test-4", "test-5":
			go fakeWatch.Delete(createPod(podName))
			return true, nil, nil
		default:
			return false, nil, nil
		}
	})

	go func() {
		if err := p.Start(ctx); err != nil {
			t.Error(err)
		}

		// Signal that the test is over
		done <- nil
		close(done)
	}()

	// Wait for the events to be sent
	<-podsAdded

	// Wait a few more seconds for the events to be processed
	time.Sleep(5 * time.Second)

	// At this point we should have only 5 elements in the list
	p.Mutex.Lock()

	if len(p.PodList) != 5 {
		t.Errorf("Expected 5 pods, got %d", len(p.PodList))
	}

	p.Mutex.Unlock()

	// We can stop here
	cancel()

	<-done
}

func TestPodWatcher_NotEnabled(t *gtest.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := fake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)
	p := watcher.NewPodWatcher(clientset, recorder, "test", "app=name")
	fakeWatch := watch.NewFake()

	p.SetTimeout(100 * time.Millisecond)
	p.SetEnabled(false)

	podsAdded := make(chan interface{})
	done := make(chan interface{})

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	clientset.PrependWatchReactor("pods", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		go func() {
			// Add a bunch of pods at regular intervals
			for i := range [10]int{} {
				fakeWatch.Add(createPod(fmt.Sprintf("test-%d", i+1)))
				time.Sleep(50 * time.Millisecond)
			}

			// Signal that we have finished sending the Add events
			podsAdded <- nil
			close(podsAdded)
		}()
		return true, fakeWatch, nil
	})

	clientset.PrependReactor("delete", "pods", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		podName := action.(ktest.DeleteAction).GetName()
		go fakeWatch.Delete(createPod(podName))
		return true, nil, nil
	})

	go func() {
		if err := p.Start(ctx); err != nil {
			t.Error(err)
		}

		// Signal that the test is over
		done <- nil
		close(done)
	}()

	// Wait for the events to be generated
	<-podsAdded
	time.Sleep(500 * time.Millisecond)

	p.Mutex.Lock()
	// We should still have 10 pods in the list
	if cnt := len(p.PodList); cnt != 10 {
		t.Errorf("Was expecting 10 pods in the list, got %d instead", cnt)
	}
	p.Mutex.Unlock()

	p.SetEnabled(true)

	// Wait some more time for the pods to be deleted
	time.Sleep(1 * time.Second)

	p.Mutex.Lock()
	// We should now have 0 pods in the list
	if cnt := len(p.PodList); cnt != 0 {
		t.Errorf("Was expecting 0 pods in the list, got %d instead", cnt)
	}
	p.Mutex.Unlock()

	cancel()
	<-done
}

func TestPodWatcher_Restart(t *gtest.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := fake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)
	p := watcher.NewPodWatcher(clientset, recorder, "test", "app=name")

	p.SetTimeout(5 * time.Hour)
	p.SetEnabled(true)

	done := make(chan interface{})

	timesCalled := &atomic.Int32{}
	timesCalled.Store(0)

	clientset.PrependWatchReactor("pods", func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		timesCalled.Add(1)
		fakeWatch := watch.NewFake()
		go func() {
			// Add a bunch of pods at regular intervals
			for i := range [10]int{} {
				fakeWatch.Add(createPod(fmt.Sprintf("test-%d", i+1)))
				time.Sleep(100 * time.Millisecond)
			}

			fakeWatch.Stop()
		}()
		return true, fakeWatch, nil
	})

	go func() {
		if err := p.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- nil
	}()

	// Wait for the events to be processed
	time.Sleep(3000 * time.Millisecond)

	// At this point, the watcher should have 10 pods in the list
	p.Mutex.Lock()
	if cnt := len(p.PodList); cnt != 10 {
		t.Errorf("Was expecting 10 pods in the list, got %d instead", cnt)
	}
	p.Mutex.Unlock()

	// And the watch should have been called 3 times
	if cnt := timesCalled.Load(); cnt != 3 {
		t.Errorf("Was expecting 4 watch calls, got %d instead", cnt)
	}

	// We can now stop the watch
	_ = p.Stop()

	<-done
}
