package factory

import (
	"time"

	"github.com/massix/chaos-monkey/internal/watcher/deployment/watcher"
	appsv1 "k8s.io/api/apps/v1"
)

type Interface interface {
	New(
		forDeployment *appsv1.Deployment,
		enabled bool,
		minReplicas, maxReplicas int,
		duration time.Duration,
	) watcher.Interface
}

type factory struct{}

func New() Interface {
	return &factory{}
}

func (f *factory) New(
	forDeployment *appsv1.Deployment,
	enabled bool,
	minReplicas, maxReplicas int,
	duration time.Duration,
) watcher.Interface {
	return watcher.NewDeploymentChaos(
		forDeployment,
		enabled,
		minReplicas,
		maxReplicas,
		duration,
	)
}
