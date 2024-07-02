package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var Version string

func main() {
	log := logrus.WithFields(logrus.Fields{"component": "main"})

	// Get the LogLevel from the environment variable
	ll, err := configuration.FromEnvironment()
	if err != nil {
		log.Warnf("No loglevel provided, using default: %s", logrus.GetLevel())
	} else {
		logrus.SetLevel(ll.LogrusLevel())
	}

	log.Infof("Starting Chaos-Monkey version: %s", Version)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	log.Info("Getting information from Kubernetes")
	bytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		log.Panic(err)
	}

	namespace := string(bytes)
	log.Info("Using namespace: " + namespace)

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
			log.Errorf("Error from namespace watcher: %s", err)
		}
	}()

	// Wait for a signal to arrive
	<-s

	log.Info("Shutting down...")
	cancel()

	log.Info("Wait for namespace watcher to finish...")
	wg.Wait()

	log.Info("Bye!")
}
