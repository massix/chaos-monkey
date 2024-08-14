package v1alpha1

import (
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              ChaosMonkeyConfigurationSpec `json:"spec"`
}

type ChaosMonkeyConfigurationSpec struct {
	DeploymentName string `json:"deploymentName"`
	Timeout        string `json:"timeout,omitempty"`
	MinReplicas    int    `json:"minReplicas"`
	MaxReplicas    int    `json:"maxReplicas"`
	Enabled        bool   `json:"enabled"`
	PodMode        bool   `json:"podMode"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ChaosMonkeyConfiguration `json:"items"`
}

func (c *ChaosMonkeyConfiguration) ToUnstructured() (*unstructured.Unstructured, error) {
	ret := &unstructured.Unstructured{}

	// Set all the common fields
	ret.SetKind(c.Kind)
	ret.SetAPIVersion(c.APIVersion)
	ret.SetName(c.Name)
	ret.SetNamespace(c.Namespace)
	ret.SetLabels(c.Labels)
	ret.SetAnnotations(c.Annotations)
	ret.SetResourceVersion(c.ResourceVersion)
	ret.SetManagedFields(c.ManagedFields)
	ret.SetUID(c.UID)

	// Now create the spec
	err := errors.Join(
		unstructured.SetNestedField(ret.Object, c.Spec.DeploymentName, "spec", "deploymentName"),
		unstructured.SetNestedField(ret.Object, c.Spec.Timeout, "spec", "timeout"),
		unstructured.SetNestedField(ret.Object, int64(c.Spec.MinReplicas), "spec", "minReplicas"),
		unstructured.SetNestedField(ret.Object, int64(c.Spec.MaxReplicas), "spec", "maxReplicas"),
		unstructured.SetNestedField(ret.Object, c.Spec.Enabled, "spec", "enabled"),
		unstructured.SetNestedField(ret.Object, c.Spec.PodMode, "spec", "podMode"),
	)

	return ret, err
}

type fieldFound struct {
	FieldName string
	Found     bool
}

func FromUnstructured(in *unstructured.Unstructured) (*ChaosMonkeyConfiguration, error) {
	if in.GetAPIVersion() != "cm.massix.github.io/v1alpha1" {
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
	depName, depNameFound, depNameErr := unstructured.NestedString(in.Object, "spec", "deploymentName")
	timeout, timeoutFound, timeoutErr := unstructured.NestedString(in.Object, "spec", "timeout")
	minReplicas, minReplicasFound, minReplicasErr := unstructured.NestedInt64(in.Object, "spec", "minReplicas")
	maxReplicas, maxReplicasFound, maxReplicasErr := unstructured.NestedInt64(in.Object, "spec", "maxReplicas")
	enabled, enabledFound, enabledErr := unstructured.NestedBool(in.Object, "spec", "enabled")
	podMode, podModeFound, podModeErr := unstructured.NestedBool(in.Object, "spec", "podMode")

	allErrors := errors.Join(
		depNameErr,
		timeoutErr,
		minReplicasErr,
		maxReplicasErr,
		enabledErr,
		podModeErr,
		errors.Join(
			booleansToErrors([]fieldFound{
				{".spec.deploymentName", depNameFound},
				{".spec.timeout", timeoutFound},
				{".spec.minReplicas", minReplicasFound},
				{".spec.maxReplicas", maxReplicasFound},
				{".spec.enabled", enabledFound},
				{".spec.podMode", podModeFound},
			})...,
		),
	)

	if allErrors != nil {
		return nil, allErrors
	}

	spec.DeploymentName = depName
	spec.Timeout = timeout
	spec.MinReplicas = int(minReplicas)
	spec.MaxReplicas = int(maxReplicas)
	spec.Enabled = enabled
	spec.PodMode = podMode
	res.Spec = spec

	return res, nil
}
