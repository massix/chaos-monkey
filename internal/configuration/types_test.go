package configuration_test

import (
	"encoding/json"
	"testing"

	"github.com/massix/chaos-monkey/internal/configuration"
	"github.com/sirupsen/logrus"
)

type UnmarshalTest struct {
	Level configuration.LogrusLevel `json:"level"`
}

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

	t.Run("Can unmarshal a logrus level", func(t *testing.T) {
		t.Parallel()

		var unmarshalTest UnmarshalTest
		err := json.Unmarshal([]byte(`{ "level": "trace" }`), &unmarshalTest)
		if err != nil {
			t.Fatal(err)
		}

		if unmarshalTest.Level.LogrusLevel() != logrus.TraceLevel {
			t.Fatal(unmarshalTest.Level.LogrusLevel())
		}
	})

	t.Run("Will fail if level is not valid", func(t *testing.T) {
		t.Parallel()

		var unmarshalTest UnmarshalTest
		err := json.Unmarshal([]byte(`{ "level": "invalid" }`), &unmarshalTest)
		if err == nil {
			t.Fatal("Was expecting error")
		}

		if err.Error() != "Invalid logrus level: invalid" {
			t.Fatal(err)
		}
	})
}
