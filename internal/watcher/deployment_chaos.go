package watcher

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	scalev1 "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typedappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/rest"
)

type DeploymentChaos interface {
	Watcher
	SetMinReplicas(int)
	SetMaxReplicas(int)
	SetTimeout(time.Duration)
	SetEnabled(bool)
}

type deploymentChaos struct {
	deployment *appsv1.Deployment
	running    bool
	typedappsv1.DeploymentInterface

	startingReplicas int
	mutex            *sync.Mutex

	enabled     bool
	minReplicas int
	maxReplicas int
	timeout     time.Duration
	rng         *rand.Rand
}

// SetMaxReplicas implements DeploymentChaos.
func (d *deploymentChaos) SetMaxReplicas(m int) {
	d.mutex.Lock()
	d.maxReplicas = m
	d.mutex.Unlock()
}

// SetMinReplicas implements DeploymentChaos.
func (d *deploymentChaos) SetMinReplicas(m int) {
	d.mutex.Lock()
	d.minReplicas = m
	d.mutex.Unlock()
}

// SetTimeout implements DeploymentChaos.
func (d *deploymentChaos) SetTimeout(td time.Duration) {
	d.mutex.Lock()
	d.timeout = td
	d.mutex.Unlock()
}

func (d *deploymentChaos) SetEnabled(v bool) {
	d.mutex.Lock()
	d.enabled = v
	d.mutex.Unlock()
}

// IsRunning implements DeploymentChaos.
func (d *deploymentChaos) IsRunning() bool {
	return d.running
}

func (d *deploymentChaos) scaleDeployment(newReplicas int) error {
	logrus.Infof("Scaling deployment %s to %d replicas", d.deployment.Name, newReplicas)
	_, err := d.UpdateScale(context.Background(), d.deployment.Name, &scalev1.Scale{
		ObjectMeta: d.deployment.ObjectMeta,
		Spec: scalev1.ScaleSpec{
			Replicas: int32(newReplicas),
		},
	}, v1.UpdateOptions{})

	return err
}

// Start implements DeploymentChaos.
func (d *deploymentChaos) Start(ctx context.Context) error {
	d.running = true
	timer := time.NewTimer(d.timeout)
	defer timer.Stop()

	for d.running {
		select {
		case <-timer.C:
			if d.enabled {
				newReplicas := d.rng.Intn(d.maxReplicas) + d.minReplicas
				if err := d.scaleDeployment(newReplicas); err != nil {
					logrus.Warnf("Error while scaling deployment: %s", err)
				}
			} else {
				logrus.Infof("Skipping deployment %s (not enabled)", d.deployment.Name)
			}

		case <-ctx.Done():
			d.running = false
		}

		timer.Reset(d.timeout)
	}

	logrus.Infof("Chaos Monkey for %s leaving, restoring to %d replicas", d.deployment.Name, d.startingReplicas)
	if err := d.scaleDeployment(d.startingReplicas); err != nil {
		logrus.Warnf("Error while scaling deployment: %s", err)
	}

	return nil
}

// Stop implements DeploymentChaos.
func (d *deploymentChaos) Stop() error {
	d.mutex.Lock()
	d.running = false
	d.mutex.Unlock()

	return nil
}

func NewDeploymentChaos(
	forDeployment *appsv1.Deployment,
	enabled bool,
	minReplicas int,
	maxReplicas int,
	duration time.Duration,
) DeploymentChaos {
	logrus.Infof("NewDeploymentChaos: %s", forDeployment.Name)

	cfg, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}

	clientset := kubernetes.NewForConfigOrDie(cfg).AppsV1().Deployments(forDeployment.Namespace)

	var sr int32 = 1
	if forDeployment.Spec.Replicas != nil {
		sr = *forDeployment.Spec.Replicas
	} else {
		logrus.Warnf("Deployment %s does not have replicas set, defaulting to %d", forDeployment.Name, sr)
	}

	return &deploymentChaos{
		DeploymentInterface: clientset,

		deployment:       forDeployment,
		running:          false,
		mutex:            &sync.Mutex{},
		minReplicas:      minReplicas,
		maxReplicas:      maxReplicas,
		timeout:          duration,
		rng:              rand.New(rand.NewSource(time.Now().UnixNano())),
		startingReplicas: int(sr),
		enabled:          enabled,
	}
}
