package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   ChaosMonkeyConfigurationSpec   `json:"spec"`
	Status ChaosMonkeyConfigurationStatus `json:"status"`
}

type ChaosMonkeyConfigurationSpec struct {
	Enabled        bool   `json:"enabled"`
	MinReplicas    int    `json:"minReplicas"`
	MaxReplicas    int    `json:"maxReplicas"`
	DeploymentName string `json:"deploymentName"`
	Timeout        string `json:"timeout,omitempty"`
}

type ChaosMonkeyConfigurationStatus struct {
	Accepted          bool         `json:"accepted"`
	LastExecution     *metav1.Time `json:"lastExecution"`
	LastKnownReplicas *int         `json:"lastKnownReplicas"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ChaosMonkeyConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ChaosMonkeyConfiguration `json:"items"`
}
