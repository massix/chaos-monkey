package watcher

import (
	"context"
	"time"
)

type Watcher interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
}

type DeploymentWatcher interface {
	Watcher
	GetDeploymentName() string
	SetTimeout(time.Duration)
	SetMinReplicas(int)
	SetMaxReplicas(int)
}
