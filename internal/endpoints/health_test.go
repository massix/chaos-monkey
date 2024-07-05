package endpoints

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/massix/chaos-monkey/internal/watcher"
	"github.com/sirupsen/logrus"
)

func setup(crdWatchers, deployWatchers int) (*watcher.NamespaceWatcher, int) {
	mainWatcher := &watcher.NamespaceWatcher{
		CrdWatchers:   map[string]watcher.Watcher{},
		RootNamespace: "root",
		Behavior:      configuration.DenyAll,
	}
	created := 1

	for i := 0; i < crdWatchers; i++ {
		crdWatcher := &watcher.CrdWatcher{DeploymentWatchers: map[string]*watcher.WatcherConfiguration{}}
		mainWatcher.CrdWatchers[fmt.Sprintf("%d", i)] = crdWatcher
		created++

		for j := 0; j < deployWatchers; j++ {
			var target watcher.ConfigurableWatcher
			if rand.Intn(2) == 0 {
				target = &watcher.PodWatcher{}
			} else {
				target = &watcher.DeploymentWatcher{}
			}
			crdWatcher.DeploymentWatchers[fmt.Sprintf("%d", j)] = &watcher.WatcherConfiguration{
				Watcher: target,
			}
			created++
		}
	}

	return mainWatcher, created
}

func TestHealthEndpoint_CountWatchers(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	t.Run("Nominal case", func(t *testing.T) {
		mw, created := setup(10, 15)
		ep := NewHealthEndpoint(mw)
		if cnt := ep.CountWatchers(); cnt != (10*15)+10+1 {
			t.Fatalf("wrong count: %d (expecting: %d)", cnt, created)
		}
	})
	t.Run("Nil pointer", func(t *testing.T) {
		ep := NewHealthEndpoint(nil)
		if cnt := ep.CountWatchers(); cnt != 0 {
			t.Fatalf("wrong count: %d (expecting: %d)", cnt, 0)
		}
	})
}

func TestHealthEndpoint_ServeHTTP(t *testing.T) {
	newTest := func() (*httptest.ResponseRecorder, *http.Request) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/health", strings.NewReader(""))

		return rec, req
	}

	t.Run("Nominal case", func(t *testing.T) {
		rec, req := newTest()
		w, tot := setup(10, 20)
		ep := NewHealthEndpoint(w)
		ep.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Wrong status code: %d", rec.Code)
		}

		var response response
		bResponse, err := io.ReadAll(rec.Result().Body)
		defer rec.Result().Body.Close()

		if err != nil {
			t.Fatal(err)
		}

		if err := json.Unmarshal(bResponse, &response); err != nil {
			t.Fatal(err)
		}

		if response.TotalWatchers != tot {
			t.Errorf("Wrong count of watchers %d", response.TotalWatchers)
		}

		if response.RootNamespace != "root" {
			t.Errorf("Wrong namespace %q", response.RootNamespace)
		}

		if response.Behavior != configuration.DenyAll {
			t.Errorf("Wrong behavior %q", response.Behavior)
		}
	})

	t.Run("Nil pointer", func(t *testing.T) {
		rec, req := newTest()
		ep := NewHealthEndpoint(nil)
		ep.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("Wrong status code: %d", rec.Code)
		}

		var response errorResponse
		bResponse, err := io.ReadAll(rec.Result().Body)
		defer rec.Result().Body.Close()

		if err != nil {
			t.Fatal(err)
		}

		if err := json.Unmarshal(bResponse, &response); err != nil {
			t.Fatal(err)
		}

		if response.Error != "Nil pointer in main watcher" {
			t.Fatalf("Unexpected error message %q", response.Error)
		}
	})
}

func BenchmarkHealthEndpoint_CountWatchers(b *testing.B) {
	logrus.SetLevel(logrus.InfoLevel)

	for i := 0; i < b.N; i++ {
		mw, _ := setup(1000, 500)
		ep := NewHealthEndpoint(mw)

		b.StartTimer()
		ep.CountWatchers()
		b.StopTimer()
	}
}
