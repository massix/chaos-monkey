package configuration

import (
	"fmt"
)

type (
	Behavior    string
	LogrusLevel string
)

// Label to look for in the NS to either allow or deny the namespace
const NamespaceLabel = "cm.massix.github.io/namespace"

const (
	AllowAll Behavior = "ALLOWALL"
	DenyAll  Behavior = "DENYALL"
)

// Common errors while parsing the configuration
type InvalidLogrusLevel struct {
	providedLevel string
}

type InvalidBehavior struct {
	providedBehaviour string
}

var (
	_ = (error)((*InvalidLogrusLevel)(nil))
	_ = (error)((*InvalidBehavior)(nil))
)

// Error implements error.
func (i *InvalidLogrusLevel) Error() string {
	return fmt.Sprintf("Invalid logrus level: %s", i.providedLevel)
}

// Error implements error.
func (i *InvalidBehavior) Error() string {
	return fmt.Sprintf("Invalid behaviour: %s", i.providedBehaviour)
}
