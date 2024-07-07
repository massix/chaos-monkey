package configuration

import (
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

func TimeoutsFromEnvironment() *Timeouts {
	// The default timeout for everything is 48 hours
	defaultTimeout := 48 * time.Hour

	nsTimeout := defaultTimeout
	crdTimeout := defaultTimeout
	podTimeout := defaultTimeout

	for env, v := range map[string]*time.Duration{
		"CHAOSMONKEY_NS_TIMEOUT":  &nsTimeout,
		"CHAOSMONKEY_CRD_TIMEOUT": &crdTimeout,
		"CHAOSMONKEY_POD_TIMEOUT": &podTimeout,
	} {
		if val, ok := os.LookupEnv(env); ok {
			var err error
			*v, err = time.ParseDuration(val)
			if err != nil {
				logrus.Errorf("Failed to parse %q: %q, using default value %q", env, err, defaultTimeout)
				*v = defaultTimeout
			}
		}
	}

	return &Timeouts{
		Namespace: nsTimeout,
		Crd:       crdTimeout,
		Pod:       podTimeout,
	}
}
