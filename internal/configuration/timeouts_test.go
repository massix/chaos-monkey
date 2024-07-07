package configuration_test

import (
	"testing"
	"time"

	"github.com/massix/chaos-monkey/internal/configuration"
)

func checkTimeouts(t *testing.T, to *configuration.Timeouts, ns, crd, pod time.Duration) {
	if to.Namespace != ns {
		t.Errorf("Wrong value for namespace timeout: %q, expected %q", to.Namespace, ns)
	}
	if to.Crd != crd {
		t.Errorf("Wrong value for crd timeout: %q, expected %q", to.Crd, crd)
	}
	if to.Pod != pod {
		t.Errorf("Wrong value for pod timeout: %q, expected %q", to.Pod, pod)
	}
}

func TestTimeouts_ParseFromEnvironment(t *testing.T) {
	t.Run("Default timeouts if no env variable is present", func(t *testing.T) {
		to := configuration.TimeoutsFromEnvironment()
		checkTimeouts(t, to, 48*time.Hour, 48*time.Hour, 48*time.Hour)
	})

	t.Run("Can set timeout for namespace", func(t *testing.T) {
		t.Setenv("CHAOSMONKEY_NS_TIMEOUT", "1h")
		to := configuration.TimeoutsFromEnvironment()
		checkTimeouts(t, to, 1*time.Hour, 48*time.Hour, 48*time.Hour)
	})

	t.Run("Can set timeout for crd", func(t *testing.T) {
		t.Setenv("CHAOSMONKEY_CRD_TIMEOUT", "1h")
		to := configuration.TimeoutsFromEnvironment()
		checkTimeouts(t, to, 48*time.Hour, 1*time.Hour, 48*time.Hour)
	})

	t.Run("Can set timeout for pod", func(t *testing.T) {
		t.Setenv("CHAOSMONKEY_POD_TIMEOUT", "1h")
		to := configuration.TimeoutsFromEnvironment()
		checkTimeouts(t, to, 48*time.Hour, 48*time.Hour, 1*time.Hour)
	})

	t.Run("Can set timeout for all", func(t *testing.T) {
		t.Setenv("CHAOSMONKEY_NS_TIMEOUT", "1h")
		t.Setenv("CHAOSMONKEY_CRD_TIMEOUT", "1h")
		t.Setenv("CHAOSMONKEY_POD_TIMEOUT", "1h")
		to := configuration.TimeoutsFromEnvironment()
		checkTimeouts(t, to, 1*time.Hour, 1*time.Hour, 1*time.Hour)
	})

	t.Run("If invalid value, use default", func(t *testing.T) {
		t.Setenv("CHAOSMONKEY_NS_TIMEOUT", "invalid")
		t.Setenv("CHAOSMONKEY_CRD_TIMEOUT", "")
		t.Setenv("CHAOSMONKEY_POD_TIMEOUT", "86y")
		to := configuration.TimeoutsFromEnvironment()
		checkTimeouts(t, to, 48*time.Hour, 48*time.Hour, 48*time.Hour)
	})
}
