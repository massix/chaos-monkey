package configuration

import (
	"errors"
	"os"
	"strings"
)

func BehaviorFromEnvironment() (Behavior, error) {
	if val, ok := os.LookupEnv("CHAOSMONKEY_BEHAVIOR"); ok {
		val = strings.ToUpper(val)

		if val == string(AllowAll) || val == string(DenyAll) {
			return Behavior(val), nil
		} else {
			return "", &InvalidBehavior{providedBehaviour: val}
		}
	}

	return "", errors.New("No environment variable provided")
}
