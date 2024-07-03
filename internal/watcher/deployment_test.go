package watcher_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	scalev1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
)

func TestDeploymentWatcher_Create(t *testing.T) {
	clientset := kfake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)

	w := watcher.NewDeploymentWatcher(clientset, recorder, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-1",
		},
	})

	defer w.Close()
	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}
}

func TestDeploymentWatcher_BasicBehaviour(t *testing.T) {
	clientset := kfake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)

	w := watcher.NewDeploymentWatcher(clientset, recorder, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-1",
		},
	})

	// Configure it
	w.SetEnabled(true)
	w.SetMinReplicas(2)
	w.SetMaxReplicas(4)
	w.SetTimeout(100 * time.Millisecond)

	numberOfRequests := &atomic.Int32{}
	numberOfRequests.Store(0)

	// Create the scenario
	clientset.PrependReactor("update", "deployments", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		numberOfRequests.Add(1)
		scaleRequest := action.(ktest.UpdateAction).GetObject().(*scalev1.Scale)

		if scaleRequest.Spec.Replicas < 2 || scaleRequest.Spec.Replicas > 4 {
			t.Errorf("Wrong scale request: %v", scaleRequest)
		}

		return true, scaleRequest, nil
	})

	// Start the watcher
	done := make(chan struct{})
	go func() {
		if err := w.Start(context.Background()); err != nil {
			t.Error(err)
		}

		done <- struct{}{}
	}()

	// Wait for some events to be produced
	time.Sleep(1060 * time.Millisecond)

	// Stop the watcher
	if err := w.Stop(); err != nil {
		t.Error(err)
	}

	<-done

	t.Logf("Number of requests: %d", numberOfRequests)

	// We should have received 10 requests
	if numberOfRequests.Load() != 10 {
		t.Errorf("Wrong number of requests: %d", numberOfRequests)
	}
}

func TestDeploymentWatcher_Error(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	clientset := kfake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(1024)

	w := watcher.NewDeploymentWatcher(clientset, recorder, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-1",
		},
	})

	// Configure it
	w.SetEnabled(true)
	w.SetMinReplicas(1)
	w.SetMaxReplicas(1)
	w.SetTimeout(500 * time.Millisecond)

	clientset.PrependReactor("update", "deployments", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
		scaleRequest := action.(ktest.UpdateAction).GetObject().(*scalev1.Scale)

		if scaleRequest.Spec.Replicas == 1 {
			err = errors.New("some error")
			ret = nil
		} else {
			err = nil
			ret = scaleRequest
		}

		handled = true

		return
	})

	// Let's have 4 consecutive errors, then a success and check that the watcher is still running

	// Start the watcher
	done := make(chan struct{})
	go func() {
		if err := w.Start(context.Background()); err == nil {
			t.Error("Was expecting the watcher to fail with an error")
		}

		done <- struct{}{}
	}()

	time.Sleep(2100 * time.Millisecond)

	// The watcher should still be running after 4 errors
	if !w.IsRunning() {
		t.Errorf("Watcher should still be running")
	}

	// Now increment the replicas so that the reactor won't send an error
	w.SetMinReplicas(2)
	w.SetMaxReplicas(5)

	// Now wait some more time
	time.Sleep(700 * time.Millisecond)

	// The runner should still be running
	if !w.IsRunning() {
		t.Errorf("Watcher should still be running")
	}

	// Now let's wait for 5 consecutive errors
	w.SetMinReplicas(1)
	w.SetMaxReplicas(1)

	<-done

	// The watcher should be stopped
	if w.IsRunning() {
		t.Errorf("Watcher should not be running")
	}
}
