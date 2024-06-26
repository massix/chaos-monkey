package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	logrus.Info("Getting information from Kubernetes")
	bytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		panic(err)
	}

	namespace := string(bytes)
	logrus.Info("Using namespace: " + namespace)

	cfg, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}

	clientset := kubernetes.NewForConfigOrDie(cfg)
	cmcClientset := versioned.NewForConfigOrDie(cfg)

	nsWatcher := watcher.DefaultNamespaceFactory(clientset, cmcClientset, nil, namespace)

	// Hook signals
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGABRT)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := nsWatcher.Start(ctx); err != nil {
			logrus.Errorf("Error from namespace watcher: %s", err)
		}
	}()

	// Wait for a signal to arrive
	<-s

	logrus.Info("Shutting down...")
	cancel()

	logrus.Info("Wait for namespace watcher to finish...")
	wg.Wait()

	logrus.Info("Bye!")
}
