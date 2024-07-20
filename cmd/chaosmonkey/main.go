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
	conf := configuration.FromEnvironment()
	logrus.SetLevel(conf.LogrusLevel)

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

	nsWatcher := watcher.DefaultNamespaceFactory(clientset, cmcClientset, nil, namespace, conf.Behavior)

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
	httpServer := &http.Server{
		Addr: "0.0.0.0:9000",
	}

	tlsServer := &http.Server{
		Addr: "0.0.0.0:9443",
	}

	// Register methods
	http.Handle("GET /metrics", promhttp.Handler())
	http.Handle("GET /health", endpoints.NewHealthEndpoint(nsWatcher.(*watcher.NamespaceWatcher)))

	http.Handle("POST /convertcrd", endpoints.NewConversionEndpoint())

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpServer.ListenAndServe(); err != nil {
			log.Warnf("Could not spawn http server: %s", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := tlsServer.ListenAndServeTLS("./main.crt", "./main.key"); err != nil {
			log.Errorf("Could not spawn https server: %s", err)
		}
	}()

	// Wait for a signal to arrive
	<-s

	if err := httpServer.Shutdown(context.Background()); err != nil {
		log.Warnf("Could not shutdown http server: %s", err)
	}

	if err := tlsServer.Shutdown(context.Background()); err != nil {
		log.Warnf("Could not shutdown https server: %s", err)
	}

	log.Info("Shutting down...")
	cancel()

	log.Info("Wait for namespace watcher to finish...")
	wg.Wait()

	log.Info("Bye!")
}
