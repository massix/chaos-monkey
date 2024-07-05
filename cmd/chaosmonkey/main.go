package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/massix/chaos-monkey/internal/apis/clientset/versioned"
	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/massix/chaos-monkey/internal/endpoints"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var Version string

func main() {
	log := logrus.WithFields(logrus.Fields{"component": "main"})

	// Get the LogLevel from the environment variable
	ll, err := configuration.LogrusLevelFromEnvironment()
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

	log.Info("Configuring default behavior via environment variable")
	behavior, err := configuration.BehaviorFromEnvironment()
	if err != nil {
		log.Warnf("Error while configuring default behavior: %s", err)

		behavior = configuration.AllowAll
		log.Warnf("Using default behavior: %s", behavior)
	}

	clientset := kubernetes.NewForConfigOrDie(cfg)
	cmcClientset := versioned.NewForConfigOrDie(cfg)

	nsWatcher := watcher.DefaultNamespaceFactory(clientset, cmcClientset, nil, namespace, behavior)

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

	// Spawn the HTTP Server for Prometheus in background
	srv := &http.Server{
		Addr: "0.0.0.0:9000",
	}

	// Register methods
	http.Handle("GET /metrics", promhttp.Handler())
	http.Handle("GET /health", endpoints.NewHealthEndpoint(nsWatcher.(*watcher.NamespaceWatcher)))

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.ListenAndServe(); err != nil {
			log.Warnf("Could not spawn http server: %s", err)
		}
	}()

	// Wait for a signal to arrive
	<-s

	if err := srv.Shutdown(context.Background()); err != nil {
		log.Warnf("Could not shutdown http server: %s", err)
	}

	log.Info("Shutting down...")
	cancel()

	log.Info("Wait for namespace watcher to finish...")
	wg.Wait()

	log.Info("Bye!")
}
