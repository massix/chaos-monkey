package v1alpha1_test

import (
	"testing"

	"github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func createCMC() *v1alpha1.ChaosMonkeyConfiguration {
	return &v1alpha1.ChaosMonkeyConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ChaosMonkeyConfiguration",
			APIVersion: "cm.massix.github.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},

		Spec: v1alpha1.ChaosMonkeyConfigurationSpec{
			MinReplicas:    3,
			MaxReplicas:    4,
			Enabled:        true,
			PodMode:        false,
			Timeout:        "30s",
			DeploymentName: "test-deployment",
		},
	}
}

func TestChaosMonkeyConfiguration_ToUnstructured(t *testing.T) {
	cmc := createCMC()

	res, err := cmc.ToUnstructured()
	if err != nil {
		t.Fatal(err)
	}

	if res.GetAPIVersion() != "cm.massix.github.io/v1alpha1" {
		t.Fatalf("Wrong APIVersion: %s", res.GetAPIVersion())
	}

	if res.GetName() != "test" {
		t.Fatalf("Wrong name: %s", res.GetName())
	}

	depName, _, _ := unstructured.NestedString(res.Object, "spec", "deploymentName")
	if depName != "test-deployment" {
		t.Fatalf("Expected 'test-deployment', received %s instead", depName)
	}
}

func TestChaosMonkeyConfiguration_FromUnstructured(t *testing.T) {
	t.Run("All is good", func(t *testing.T) {
		cmc := createCMC()
		uns, _ := cmc.ToUnstructured()

		res, err := v1alpha1.FromUnstructured(uns)
		if err != nil {
			t.Fatal(err)
		}

		if res.Spec.DeploymentName != "test-deployment" {
			t.Fatalf("Was expecting 'test-deployment', received %s instead", res.Spec.DeploymentName)
		}
	})

	t.Run("Missing fields", func(t *testing.T) {
		cmc := createCMC()
		uns, _ := cmc.ToUnstructured()
		unstructured.RemoveNestedField(uns.Object, "spec", "timeout")

		_, err := v1alpha1.FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != "Field .spec.timeout not found" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Wrong format for field", func(t *testing.T) {
		cmc := createCMC()
		uns, _ := cmc.ToUnstructured()
		unstructured.RemoveNestedField(uns.Object, "spec", "deploymentName")
		_ = unstructured.SetNestedField(uns.Object, int64(42), "spec", "deploymentName")

		_, err := v1alpha1.FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != ".spec.deploymentName accessor error: 42 is of the type int64, expected string\nField .spec.deploymentName not found" {
			t.Fatalf("Unexpected error message: %q", err.Error())
		}
	})

	t.Run("Wrong APIVersion", func(t *testing.T) {
		uns := &unstructured.Unstructured{}
		uns.SetAPIVersion("wrong")

		_, err := v1alpha1.FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != "Wrong APIVersion: wrong" {
			t.Fatalf("Unexpected error message: %q", err.Error())
		}
	})

	t.Run("Wrong Kind", func(t *testing.T) {
		uns := &unstructured.Unstructured{}
		uns.SetAPIVersion("cm.massix.github.io/v1alpha1")
		uns.SetKind("wrong")

		_, err := v1alpha1.FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != "Wrong Kind: wrong" {
			t.Fatalf("Unexpected error message: %q", err.Error())
		}
	})
}
