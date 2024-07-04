package configuration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/sirupsen/logrus"
)

func Test_LogrusLevel(t *testing.T) {
	t.Run("Can create a logrus level", func(t *testing.T) {
		t.Parallel()

		l, err := configuration.NewLogrusLevel("debug")
		if err != nil {
			t.Fatal(err)
		}

		if level := l.LogrusLevel(); level != logrus.DebugLevel {
			t.Fatal(level)
		}
	})

	t.Run("Should fail on invalid logrus level", func(t *testing.T) {
		t.Parallel()

		_, err := configuration.NewLogrusLevel("invalid")
		if err == nil {
			t.Fatal("Was expecting error")
		}

		if err.Error() != "Invalid logrus level: invalid" {
			t.Fatal(err)
		}
	})
}

func TestLogLevel_FromEnvironment(t *testing.T) {
	for _, level := range []string{"PANIC", "FaTaL", "eRROR", "WARN", "info", "debug", "trace"} {
		t.Run(fmt.Sprintf("Can set loglevel from environment (%s)", level), func(t *testing.T) {
			t.Setenv("CHAOSMONKEY_LOGLEVEL", level)

			ll, err := configuration.LogrusLevelFromEnvironment()
			if err != nil {
				t.Fatal(err)
			}

			if string(ll) != strings.ToLower(level) {
				t.Errorf("Was expecting %s, got %s instead", level, ll)
			}
		})
	}

	for _, level := range []string{"", "invalid", "geckos"} {
		t.Run(fmt.Sprintf("It fails for invalid strings (%s)", level), func(t *testing.T) {
			t.Setenv("CHAOSMONKEY_LOGLEVEL", level)

			ll, err := configuration.LogrusLevelFromEnvironment()
			if err == nil || ll != "" {
				t.Fatalf("Was not expecting to succeed: %s", ll)
			}

			expected := fmt.Sprintf("Invalid logrus level: %s", level)
			if err.Error() != expected {
				t.Fatalf("Was expecting %q, got %q instead", expected, err)
			}

			t.Log(err)
		})
	}

	t.Run("It fails if there is no environment variable", func(t *testing.T) {
		ll, err := configuration.LogrusLevelFromEnvironment()
		if err == nil || ll != "" {
			t.Fatal("Was not expecting to succeed")
		}

		expected := "No environment variable for configuring the log level found."
		if err.Error() != expected {
			t.Fatalf("Was expecting %q, got %q instead", expected, err)
		}
	})
}
