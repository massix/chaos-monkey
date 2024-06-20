package configuration

import (
	"encoding/json"

	"github.com/sirupsen/logrus"
)

type InvalidLogrusLevel struct {
	providedLevel string
}

func (i InvalidLogrusLevel) Error() string {
	return "Invalid logrus level: " + i.providedLevel
}

type LogrusLevel string

func NewLogrusLevel(level string) (LogrusLevel, error) {
	switch level {
	case "panic":
		fallthrough
	case "fatal":
		fallthrough
	case "error":
		fallthrough
	case "warn":
		fallthrough
	case "info":
		fallthrough
	case "debug":
		fallthrough
	case "trace":
		return LogrusLevel(level), nil
	default:
		return "", &InvalidLogrusLevel{level}
	}
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

type Configuration struct {
	KubernetesConfiguration *KubernetesConfiguration `toml:"kubernetes"`
}

type KubernetesConfiguration struct {
	Namespace string `toml:"namespace"`
}
