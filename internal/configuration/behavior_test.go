package configuration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/massix/chaos-monkey/internal/configuration"
)

func TestBehavior_ParseFromEnvironment(t *testing.T) {
	goodEnvValues := map[string]configuration.Behavior{
		"allowall": configuration.AllowAll,
		"ALLOWALL": configuration.AllowAll,
		"AllOwAll": configuration.AllowAll,
		"denyall":  configuration.DenyAll,
		"DenyAll":  configuration.DenyAll,
		"DENYALL":  configuration.DenyAll,
	}

	for key, val := range goodEnvValues {
		t.Run(fmt.Sprintf("Can parse from environment (%s)", key), func(t *testing.T) {
			t.Setenv("CHAOSMONKEY_BEHAVIOR", key)

			if b, err := configuration.BehaviorFromEnvironment(); err != nil {
				t.Error(err)
			} else if b != val {
				t.Errorf("Was expecting %s, got %s instead", val, b)
			}
		})
	}

	badEnvValues := []string{
		"",
		"invalid",
		"geckos",
	}

	for _, val := range badEnvValues {
		t.Run(fmt.Sprintf("It fails for invalid strings (%s)", val), func(t *testing.T) {
			t.Setenv("CHAOSMONKEY_BEHAVIOR", val)

			if b, err := configuration.BehaviorFromEnvironment(); err == nil {
				t.Errorf("Was expecting error, received %s instead", b)
			} else if err.Error() != fmt.Sprintf("Invalid behaviour: %s", strings.ToUpper(val)) {
				t.Error(err)
			}
		})
	}

	t.Run("It fails if there is no environment variable", func(t *testing.T) {
		if b, err := configuration.BehaviorFromEnvironment(); err == nil {
			t.Errorf("Was expecting error, received %s instead", b)
		} else if err.Error() != "No environment variable provided" {
			t.Error(err)
		}
	})
}
