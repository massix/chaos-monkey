package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	crdfactory "github.com/massix/chaos-monkey/internal/watcher/crd/factory"
	depfactory "github.com/massix/chaos-monkey/internal/watcher/deployment/factory"
	nsfactory "github.com/massix/chaos-monkey/internal/watcher/namespace/factory"
	"github.com/sirupsen/logrus"
)

func main() {
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	logrus.Info("Getting information from Kubernetes")
	bytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		panic(err)
	}

	namespace := string(bytes)
	logrus.Info("Using namespace: " + namespace)

	depFactory := depfactory.New()
	crdFactory := crdfactory.New(depFactory)
	nsFactory := nsfactory.New(crdFactory, depFactory)

	wg.Add(1)
	cl := nsFactory.New(namespace)

	go func() {
		if err := cl.Start(ctx); err != nil {
			logrus.Warnf("Error from namespace watcher: %s", err)
		}

		wg.Done()
	}()

	// Hook signals
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGABRT)

	// Wait for a signal to arrive
	<-s

	logrus.Info("Shutting down...")
	cancel()

	logrus.Info("Wait for namespace watcher to finish...")
	wg.Wait()

	logrus.Info("Bye!")
}
