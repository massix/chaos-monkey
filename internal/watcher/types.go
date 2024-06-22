package watcher

import (
	"context"
)

type Watcher interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
}
