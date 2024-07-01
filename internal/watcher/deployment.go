package watcher

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	scalev1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

type DeploymentWatcher struct {
	typedappsv1.DeploymentInterface
	record.EventRecorderLogger

	Logrus             logrus.FieldLogger
	OriginalDeployment *appsv1.Deployment
	Mutex              *sync.Mutex
	MinReplicas        int
	MaxReplicas        int
	Timeout            time.Duration
	ForceStopChan      chan interface{}
	Running            bool
	Enabled            bool
}

func NewDeploymentWatcher(clientset kubernetes.Interface, recorder record.EventRecorderLogger, deployment *appsv1.Deployment) ConfigurableWatcher {
	logrus.Infof("Creating new Deployment watcher for %s/%s", deployment.Namespace, deployment.Name)

	// Build my own recorder here
	if recorder == nil {
		logrus.Debug("No recorder provided, using default")
		broadcaster := record.NewBroadcaster()
		broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events(deployment.Namespace)})
		recorder = broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "chaos-monkey"})
	}

	return &DeploymentWatcher{
		DeploymentInterface: clientset.AppsV1().Deployments(deployment.Namespace),
		OriginalDeployment:  deployment,
		EventRecorderLogger: recorder,

		Logrus:        logrus.WithFields(logrus.Fields{"component": "DeploymentWatcher", "namespace": deployment.Namespace, "deploymentName": deployment.Name}),
		Mutex:         &sync.Mutex{},
		MinReplicas:   0,
		MaxReplicas:   0,
		Timeout:       0,
		ForceStopChan: make(chan interface{}),
		Running:       false,
		Enabled:       false,
	}
}

// IsRunning implements DeploymentWatcherI.
func (d *DeploymentWatcher) IsRunning() bool {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	return d.Running
}

// SetEnabled implements DeploymentWatcherI.
func (d *DeploymentWatcher) SetEnabled(v bool) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.Enabled = v
}

// SetMaxReplicas implements DeploymentWatcherI.
func (d *DeploymentWatcher) SetMaxReplicas(v int) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.MaxReplicas = v
}

// SetMinReplicas implements DeploymentWatcherI.
func (d *DeploymentWatcher) SetMinReplicas(v int) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.MinReplicas = v
}

// SetTimeout implements DeploymentWatcherI.
func (d *DeploymentWatcher) SetTimeout(v time.Duration) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.Timeout = v
}

// Start implements DeploymentWatcherI.
func (d *DeploymentWatcher) Start(ctx context.Context) error {
	d.Logrus.Infof("Starting Chaos Monkey")
	timer := time.NewTimer(d.getTimeout())

	d.setRunning(true)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Allow a maximum of 5 consecutive errors before bailing out
	consecutiveErrors := 0

	for d.IsRunning() {
		select {
		case <-timer.C:
			if !d.isEnabled() {
				d.Logrus.Debug("Skipping scaling")
			} else {
				d.Logrus.Debug("Scaling deployment")
				newReplicas := max(rng.Intn(d.getMaxReplicas()+1), d.getMinReplicas())
				if err := d.scaleDeployment(newReplicas); err != nil {
					d.Logrus.Errorf("Error while scaling deployment: %s", err)
					consecutiveErrors++
					d.Logrus.Debugf("Consecutive errors: %d", consecutiveErrors)
				} else {
					d.Logrus.Debug("Resetting consecutive errors")
					consecutiveErrors = 0
				}
			}
		case <-ctx.Done():
			d.Logrus.Info("Stopping Chaos Monkey")
			d.setRunning(false)
		case <-d.ForceStopChan:
			d.Logrus.Info("Force stopping Chaos Monkey")
		}

		if consecutiveErrors >= 5 {
			d.Logrus.Error("Too many consecutive errors, stopping deployment watcher")
			err1 := d.Stop()
			return errors.Join(err1, errors.New("too many consecutive errors"))
		}

		d.Logrus.Debugf("Resetting timer to %s", d.getTimeout())
		timer.Reset(d.getTimeout())
	}

	d.Logrus.Info("Chaos Monkey stopped")
	return nil
}

func (d *DeploymentWatcher) scaleDeployment(newReplicas int) error {
	d.Logrus.Infof("Scaling deployment to %d replicas", newReplicas)

	res, err := d.UpdateScale(context.Background(), d.getOriginalDeployment().Name, &scalev1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.getOriginalDeployment().Name,
			Namespace: d.getOriginalDeployment().Namespace,
		},
		Spec: scalev1.ScaleSpec{
			Replicas: int32(newReplicas),
		},
	}, metav1.UpdateOptions{})

	if err == nil {
		d.Logrus.Debugf("Successfully scaled to %d replicas, publishing event", res.Spec.Replicas)
		d.Eventf(d.getOriginalDeployment(), corev1.EventTypeNormal, "ChaosMonkey", "Converted to %d replicas", res.Spec.Replicas)
	}

	return err
}

func (d *DeploymentWatcher) getOriginalDeployment() *appsv1.Deployment {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	return d.OriginalDeployment
}

func (d *DeploymentWatcher) getMinReplicas() int {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	return d.MinReplicas
}

func (d *DeploymentWatcher) getMaxReplicas() int {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	return d.MaxReplicas
}

func (d *DeploymentWatcher) getTimeout() time.Duration {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	return d.Timeout
}

func (d *DeploymentWatcher) isEnabled() bool {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	return d.Enabled
}

func (d *DeploymentWatcher) setRunning(v bool) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.Running = v
}

// Stop implements DeploymentWatcherI.
func (d *DeploymentWatcher) Stop() error {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.Logrus.Debug("Stopping Chaos Monkey")
	d.Running = false

	select {
	case d.ForceStopChan <- nil:
	default:
		d.Logrus.Warn("Could not write to ForceStopChan")
	}

	return nil
}
