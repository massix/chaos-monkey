package configuration

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Configuration values
type (
	Behavior    string
	LogrusLevel string

	Timeouts struct {
		Namespace time.Duration
		Crd       time.Duration
		Pod       time.Duration
	}
)

// Label to look for in the NS to either allow or deny the namespace
const NamespaceLabel = "cm.massix.github.io/namespace"

const (
	BehaviorAllowAll Behavior = "ALLOWALL"
	BehaviorDenyAll  Behavior = "DENYALL"
)

const (
	EnvBehavior   string = "CHAOSMONKEY_BEHAVIOR"
	EnvLoglevel   string = "CHAOSMONKEY_LOGLEVEL"
	EnvNsTimeout  string = "CHAOSMONKEY_NS_TIMEOUT"
	EnvCrdTimeout string = "CHAOSMONKEY_CRD_TIMEOUT"
	EnvPodTimeout string = "CHAOSMONKEY_POD_TIMEOUT"
)

// Helper function to get a value from environment
type ConvertFn[T any] func(in string) (T, error)

func EnvOrElse[T any](envName string, defValue T, fn ConvertFn[T]) T {
	log := logrus.WithField("component", "configuration")

	if val, ok := os.LookupEnv(envName); ok {
		if retVal, err := fn(val); err == nil {
			return retVal
		} else {
			log.Errorf("While parsing %q: %s, using default value %+v", envName, err, defValue)
			return defValue
		}
	}

	log.Warnf("Environment variable %q not found, using default value %+v", envName, defValue)
	return defValue
}

type Configuration struct {
	Behavior        Behavior
	Timeouts        Timeouts
	LogrusLevel     logrus.Level
	fromEnvironment bool
}

// Create a configuration with default values, mostly used for testing
var parsedConfiguration Configuration = Configuration{
	Behavior:    BehaviorAllowAll,
	LogrusLevel: logrus.InfoLevel,
	Timeouts: Timeouts{
		Namespace: 48 * time.Hour,
		Crd:       48 * time.Hour,
		Pod:       48 * time.Hour,
	},
	fromEnvironment: false,
}

// This method changes the ParsedConfiguration and returns a pointer to it
func FromEnvironment() *Configuration {
	// We parse from the environment only once
	if !parsedConfiguration.fromEnvironment {
		parsedConfiguration = Configuration{
			Behavior: EnvOrElse(EnvBehavior, BehaviorAllowAll, func(in string) (Behavior, error) {
				up := strings.ToUpper(in)
				if up == string(BehaviorAllowAll) || up == string(BehaviorDenyAll) {
					return Behavior(up), nil
				} else {
					return "", fmt.Errorf("Invalid value for behavior: %s", in)
				}
			}),

			LogrusLevel: EnvOrElse(EnvLoglevel, logrus.InfoLevel, logrus.ParseLevel),

			Timeouts: Timeouts{
				Namespace: EnvOrElse(EnvNsTimeout, 48*time.Hour, time.ParseDuration),
				Crd:       EnvOrElse(EnvCrdTimeout, 48*time.Hour, time.ParseDuration),
				Pod:       EnvOrElse(EnvPodTimeout, 48*time.Hour, time.ParseDuration),
			},
			fromEnvironment: true,
		}
	}

	return &parsedConfiguration
}
