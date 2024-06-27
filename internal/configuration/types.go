package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
)

type InvalidLogrusLevel struct {
	providedLevel string
}

func (i *InvalidLogrusLevel) Error() string {
	return fmt.Sprintf("Invalid logrus level: %s", i.providedLevel)
}

type LogrusLevel string

func FromEnvironment() (LogrusLevel, error) {
	if val, ok := os.LookupEnv("CHAOSMONKEY_LOGLEVEL"); ok {
		if newLevel, err := NewLogrusLevel(strings.ToLower(val)); err == nil {
			return newLevel, nil
		} else {
			return "", err
		}
	}

	return "", errors.New("No environment variable for configuring the log level found.")
}

func NewLogrusLevel(level string) (LogrusLevel, error) {
	validLevels := []string{
		"panic",
		"fatal",
		"error",
		"warn",
		"info",
		"debug",
		"trace",
	}

	if slices.Contains(validLevels, level) {
		return LogrusLevel(level), nil
	}

	return "", &InvalidLogrusLevel{level}
}

func (l LogrusLevel) LogrusLevel() logrus.Level {
	switch l {
	case "panic":
		return logrus.PanicLevel
	case "fatal":
		return logrus.FatalLevel
	case "error":
		return logrus.ErrorLevel
	case "warn":
		return logrus.WarnLevel
	case "info":
		return logrus.InfoLevel
	case "debug":
		return logrus.DebugLevel
	case "trace":
		return logrus.TraceLevel
	default:
		return logrus.InfoLevel
	}
}

func (l *LogrusLevel) UnmarshalJSON(b []byte) error {
	var level string
	err := json.Unmarshal(b, &level)
	if err != nil {
		return err
	}

	newVal, err := NewLogrusLevel(level)
	if err != nil {
		return err
	}

	*l = newVal

	return nil
}
