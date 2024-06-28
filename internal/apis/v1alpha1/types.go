package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfiguration struct {
	Status            ChaosMonkeyConfigurationStatus `json:"status"`
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

type ChaosMonkeyConfigurationStatus struct {
	LastExecution     *metav1.Time `json:"lastExecution"`
	LastKnownReplicas *int         `json:"lastKnownReplicas"`
	Accepted          bool         `json:"accepted"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ChaosMonkeyConfiguration `json:"items"`
}
