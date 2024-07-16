package configuration

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestConfiguration_FromEnvironment(t *testing.T) {
	parsedConfiguration.fromEnvironment = false
	t.Run("It should exist", func(t *testing.T) {
		cnf := parsedConfiguration
		if cnf.fromEnvironment {
			t.Error("fromEnvironment should be false")
		}
	})

	parsedConfiguration.fromEnvironment = false
	t.Run("It should parse environment values", func(t *testing.T) {
		t.Setenv(EnvBehavior, "denyall")
		t.Setenv(EnvLoglevel, "trace")
		t.Setenv(EnvNsTimeout, "48s")
		t.Setenv(EnvCrdTimeout, "32s")
		t.Setenv(EnvPodTimeout, "11s")

		conf := FromEnvironment()

		if conf != &parsedConfiguration {
			t.Fatal("Wrong pointer returned?")
		}

		if !parsedConfiguration.fromEnvironment {
			t.Fatal("fromEnvironment should be true")
		}

		if conf.Behavior != BehaviorDenyAll {
			t.Fatal("Behaviour should be DenyAll")
		}

		if conf.LogrusLevel != logrus.TraceLevel {
			t.Fatal("Loglevel should be trace")
		}

		if to := conf.Timeouts.Namespace; to != 48*time.Second {
			t.Fatalf("Wrong timeout for NS: %q", to)
		}
		if to := conf.Timeouts.Crd; to != 32*time.Second {
			t.Fatalf("Wrong timeout for Crd: %q", to)
		}
		if to := conf.Timeouts.Pod; to != 11*time.Second {
			t.Fatalf("Wrong timeout for Pod: %q", to)
		}
	})

	parsedConfiguration.fromEnvironment = false
	t.Run("It should parse only once", func(t *testing.T) {
		t.Setenv(EnvLoglevel, "panic")

		conf := FromEnvironment()
		if conf.LogrusLevel != logrus.PanicLevel {
			t.Fatalf("Wrong loglevel %q", conf.LogrusLevel)
		}

		t.Setenv(EnvLoglevel, "debug")
		conf = FromEnvironment()
		if conf.LogrusLevel != logrus.PanicLevel {
			t.Fatalf("Wrong loglevel %q", conf.LogrusLevel)
		}
	})

	parsedConfiguration.fromEnvironment = false
	t.Run("It should use default values if fails to parse", func(t *testing.T) {
		t.Setenv(EnvLoglevel, "invalid")
		t.Setenv(EnvBehavior, "whatever")
		t.Setenv(EnvCrdTimeout, "67 centuries")

		conf := FromEnvironment()
		if val := conf.Behavior; val != BehaviorAllowAll {
			t.Fatalf("Wrong behavior: %q", val)
		}

		if val := conf.LogrusLevel; val != logrus.InfoLevel {
			t.Fatalf("Wrong loglevel: %q", val)
		}

		if val := conf.Timeouts.Crd; val != 48*time.Hour {
			t.Fatalf("Wrong timeout for CRD: %q", val)
		}
	})
}
