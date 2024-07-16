package endpoints

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
)

type HealthEndpoint struct {
	mainWatcher *watcher.NamespaceWatcher
	logrus      logrus.FieldLogger
}

type response struct {
	Status        string                 `json:"status"`
	RootNamespace string                 `json:"root_namespace"`
	Behavior      configuration.Behavior `json:"behavior"`
	Loglevel      string                 `json:"loglevel"`
	TotalWatchers int                    `json:"total_watchers"`
}

type errorResponse struct {
	Error string `json:"error_message"`
	Code  int    `json:"code"`
}

type stack[T any] struct {
	elems []T
}

func (s *stack[T]) push(elem T) {
	s.elems = append(s.elems, elem)
}

func (s *stack[T]) pop() (T, error) {
	if s.isEmpty() {
		return *new(T), errors.New("Empty stack")
	}
	ret := s.elems[0]
	s.elems = s.elems[1:]
	return ret, nil
}

func (s *stack[T]) isEmpty() bool {
	return len(s.elems) == 0
}

func NewHealthEndpoint(w *watcher.NamespaceWatcher) *HealthEndpoint {
	return &HealthEndpoint{
		mainWatcher: w,
		logrus:      logrus.WithFields(logrus.Fields{"component": "HealthEndpoint"}),
	}
}

func (e *HealthEndpoint) CountWatchers() int {
	stack := &stack[watcher.Watcher]{}
	count := 0
	stack.push(e.mainWatcher)
	e.logrus.Debug("Counting watchers")

	for !stack.isEmpty() {
		current, err := stack.pop()
		if err != nil {
			e.logrus.Warn(err)
			continue
		}

		switch w := current.(type) {
		case (*watcher.NamespaceWatcher):
			if w == nil {
				e.logrus.Warn("Nil pointer in loop")
				continue
			}
			e.logrus.Debugf("Found NamespaceWatcher with %d children", len(w.CrdWatchers))
			for _, child := range w.CrdWatchers {
				stack.push(child)
			}
		case (*watcher.CrdWatcher):
			if w == nil {
				e.logrus.Warn("Nil pointer in loop")
				continue
			}
			e.logrus.Debugf("Found CrdWatcher with %d children", len(w.DeploymentWatchers))
			for _, child := range w.DeploymentWatchers {
				stack.push(child.Watcher)
			}
		default:
			if w == nil {
				e.logrus.Warn("Nil pointer in loop")
				continue
			}
			e.logrus.Debug("Pod or DeploymentWatcher found")
		}

		count++
	}

	e.logrus.Debugf("Total count: %d", count)
	return count
}

// ServeHTTP implements http.Handler.
func (e *HealthEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.logrus.Debugf(
		"Handling request '%s %s' from %s",
		r.Method,
		r.URL.Path,
		r.RemoteAddr,
	)

	write := func(b []byte) {
		if _, err := w.Write(b); err != nil {
			e.logrus.Errorf("Could not write response: %s", err)
		}
	}

	if e.mainWatcher == nil {
		e.logrus.Error("Nil pointer in main watcher")
		w.WriteHeader(http.StatusInternalServerError)
		if b, err := json.Marshal(
			&errorResponse{
				"Nil pointer in main watcher",
				http.StatusInternalServerError,
			}); err == nil {
			write(b)
		} else {
			e.logrus.Error(err)
			write([]byte{})
		}
		return
	}

	response := &response{
		Status:        "up",
		TotalWatchers: e.CountWatchers(),
		RootNamespace: e.mainWatcher.RootNamespace,
		Behavior:      e.mainWatcher.Behavior,
		Loglevel:      logrus.GetLevel().String(),
	}

	if b, err := json.Marshal(response); err == nil {
		w.WriteHeader(http.StatusOK)
		write(b)
	} else {
		e.logrus.Errorf("Could not marshal response: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		write([]byte{})
	}
}

var _ = (http.Handler)((*HealthEndpoint)(nil))
