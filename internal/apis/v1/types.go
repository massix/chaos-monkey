package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ScalingMode string

const (
	ScalingModeRandomScale  ScalingMode = "randomScale"
	ScalingModeAntiPressure ScalingMode = "antiPressure"
	ScalingModeKillPod      ScalingMode = "killPod"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec ChaosMonkeyConfigurationSpec `json:"spec"`
}

type ChaosMonkeyConfigurationSpec struct {
	Enabled     bool                                   `json:"enabled"`
	MinReplicas int                                    `json:"minReplicas"`
	MaxReplicas int                                    `json:"maxReplicas"`
	ScalingMode ScalingMode                            `json:"scalingMode"`
	Deployment  ChaosMonkeyConfigurationSpecDeployment `json:"deployment"`
	Timeout     time.Duration                          `json:"timeout"`
}

var (
	_ = (json.Marshaler)((*ChaosMonkeyConfigurationSpec)(nil))
	_ = (json.Unmarshaler)((*ChaosMonkeyConfigurationSpec)(nil))
)

type ChaosMonkeyConfigurationSpecDeployment struct {
	Name string `json:"name"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ChaosMonkeyConfiguration `json:"items"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (c *ChaosMonkeyConfigurationSpec) UnmarshalJSON(data []byte) error {
	logger := logrus.WithFields(logrus.Fields{"component": "CMCUnmarshaller"})
	logger.Debugf("Unmarshalling CMC: %q", string(data))

	defaultTimeout := 10 * time.Minute

	var tmp struct {
		Enabled     bool                                   `json:"enabled"`
		MinReplicas int                                    `json:"minReplicas"`
		MaxReplicas int                                    `json:"maxReplicas"`
		ScalingMode ScalingMode                            `json:"scalingMode"`
		Deployment  ChaosMonkeyConfigurationSpecDeployment `json:"deployment"`
		Timeout     string                                 `json:"timeout"`
	}

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	logger.Debugf("Intermediate parsing: %+v", tmp)

	c.Enabled = tmp.Enabled
	c.MinReplicas = tmp.MinReplicas
	c.MaxReplicas = tmp.MaxReplicas
	c.ScalingMode = tmp.ScalingMode
	c.Deployment = tmp.Deployment

	if parsedTimeout, err := time.ParseDuration(tmp.Timeout); err != nil {
		logger.Warnf("Failed to parse duration: %s, using default: %v", err, defaultTimeout)
		c.Timeout = defaultTimeout
	} else {
		logger.Debugf("Parsed timeout: %v", parsedTimeout)
		c.Timeout = parsedTimeout
	}

	return nil
}

// MarshalJSON implements json.Marshaler.
func (c *ChaosMonkeyConfigurationSpec) MarshalJSON() ([]byte, error) {
	var tmp struct {
		Enabled     bool                                   `json:"enabled"`
		MinReplicas int                                    `json:"minReplicas"`
		MaxReplicas int                                    `json:"maxReplicas"`
		ScalingMode ScalingMode                            `json:"scalingMode"`
		Deployment  ChaosMonkeyConfigurationSpecDeployment `json:"deployment"`
		Timeout     string                                 `json:"timeout"`
	}

	tmp.Enabled = c.Enabled
	tmp.MinReplicas = c.MinReplicas
	tmp.MaxReplicas = c.MaxReplicas
	tmp.ScalingMode = c.ScalingMode
	tmp.Deployment = c.Deployment
	tmp.Timeout = c.Timeout.String()

	return json.Marshal(tmp)
}

func (c *ChaosMonkeyConfiguration) ToUnstructured() (*unstructured.Unstructured, error) {
	ret := &unstructured.Unstructured{}

	ret.SetKind(c.Kind)
	ret.SetAPIVersion(c.APIVersion)
	ret.SetName(c.Name)
	ret.SetNamespace(c.Namespace)
	ret.SetLabels(c.Labels)
	ret.SetAnnotations(c.Annotations)
	ret.SetResourceVersion(c.ResourceVersion)
	ret.SetManagedFields(c.ManagedFields)
	ret.SetUID(c.UID)

	// Create the spec field now
	err := errors.Join(
		unstructured.SetNestedField(ret.Object, c.Spec.Enabled, "spec", "enabled"),
		unstructured.SetNestedField(ret.Object, int64(c.Spec.MinReplicas), "spec", "minReplicas"),
		unstructured.SetNestedField(ret.Object, int64(c.Spec.MaxReplicas), "spec", "maxReplicas"),
		unstructured.SetNestedField(ret.Object, string(c.Spec.ScalingMode), "spec", "scalingMode"),
		unstructured.SetNestedField(ret.Object, c.Spec.Deployment.Name, "spec", "deployment", "name"),
		unstructured.SetNestedField(ret.Object, c.Spec.Timeout.String(), "spec", "timeout"),
	)

	return ret, err
}

type fieldFound struct {
	FieldName string
	Found     bool
}

func FromUnstructured(in *unstructured.Unstructured) (*ChaosMonkeyConfiguration, error) {
	if in.GetAPIVersion() != "cm.massix.github.io/v1" {
		return nil, fmt.Errorf("Wrong APIVersion: %s", in.GetAPIVersion())
	}

	if in.GetKind() != "ChaosMonkeyConfiguration" {
		return nil, fmt.Errorf("Wrong Kind: %s", in.GetKind())
	}

	booleansToErrors := func(in []fieldFound) []error {
		var errors []error
		for _, v := range in {
			if !v.Found {
				errors = append(errors, fmt.Errorf("Field %s not found", v.FieldName))
			}
		}

		return errors
	}

	res := &ChaosMonkeyConfiguration{}
	typeMeta := metav1.TypeMeta{}
	typeMeta.Kind = in.GetKind()
	typeMeta.APIVersion = in.GetAPIVersion()
	res.TypeMeta = typeMeta

	objectMeta := metav1.ObjectMeta{}
	objectMeta.Name = in.GetName()
	objectMeta.Namespace = in.GetNamespace()
	objectMeta.Labels = in.GetLabels()
	objectMeta.Annotations = in.GetAnnotations()
	objectMeta.ResourceVersion = in.GetResourceVersion()
	objectMeta.ManagedFields = in.GetManagedFields()
	objectMeta.UID = in.GetUID()
	res.ObjectMeta = objectMeta

	spec := ChaosMonkeyConfigurationSpec{}
	depName, depNameFound, depNameErr := unstructured.NestedString(in.Object, "spec", "deployment", "name")
	timeout, timeoutFound, timeoutErr := unstructured.NestedString(in.Object, "spec", "timeout")
	minReplicas, minReplicasFound, minReplicasErr := unstructured.NestedInt64(in.Object, "spec", "minReplicas")
	maxReplicas, maxReplicasFound, maxReplicasErr := unstructured.NestedInt64(in.Object, "spec", "maxReplicas")
	enabled, enabledFound, enabledErr := unstructured.NestedBool(in.Object, "spec", "enabled")
	scalingMode, scalingModeFound, scalingModeErr := unstructured.NestedString(in.Object, "spec", "scalingMode")

	allErrors := errors.Join(
		depNameErr,
		timeoutErr,
		minReplicasErr,
		maxReplicasErr,
		enabledErr,
		scalingModeErr,
		errors.Join(
			booleansToErrors([]fieldFound{
				{".spec.deploymentName", depNameFound},
				{".spec.timeout", timeoutFound},
				{".spec.minReplicas", minReplicasFound},
				{".spec.maxReplicas", maxReplicasFound},
				{".spec.enabled", enabledFound},
				{".spec.scalingMode", scalingModeFound},
			})...,
		),
	)

	if allErrors != nil {
		return nil, allErrors
	}

	if parsedTimeout, err := time.ParseDuration(timeout); err != nil {
		logrus.WithField("component", "CMCv1ToUnstructured").Errorf("Failed to parse timeout: %v", err)
		spec.Timeout = 10 * time.Minute
	} else {
		spec.Timeout = parsedTimeout
	}

	spec.Deployment = ChaosMonkeyConfigurationSpecDeployment{depName}
	spec.MinReplicas = int(minReplicas)
	spec.MaxReplicas = int(maxReplicas)
	spec.Enabled = enabled
	spec.ScalingMode = ScalingMode(scalingMode)
	res.Spec = spec

	return res, nil
}
