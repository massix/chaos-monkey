package v1

import (
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestChaosMonkeyConfigurationSpec_UnmarshalJson(t *testing.T) {
	t.Run("Valid Timeout", func(t *testing.T) {
		jsonString := `
    {
      "kind": "ChaosMonkeyConfiguration",
      "apiVersion": "cm.massix.github.io/v1",
      "metadata": {
        "name": "cmc1",
        "namespace": "cmc1"
      },
      "spec": {
        "enabled": true,
        "minReplicas": 0,
        "maxReplicas": 4,
        "deployment": { "name": "target-deployment" },
        "timeout": "20m"
      }
    }
    `

		var cmc ChaosMonkeyConfiguration
		if err := json.Unmarshal([]byte(jsonString), &cmc); err != nil {
			t.Fatal(err)
		}

		if cmc.Spec.Timeout != 20*time.Minute {
			t.Errorf("Expected timeout to be 20 minutes, got %s", cmc.Spec.Timeout)
		}

		if cmc.Spec.Deployment.Name != "target-deployment" {
			t.Errorf("Expected deployment name to be 'target-deployment', got %s", cmc.Spec.Deployment.Name)
		}
	})

	t.Run("Invalid Timeout (should use default)", func(t *testing.T) {
		jsonString := `
    {
      "kind": "ChaosMonkeyConfiguration",
      "apiVersion": "cm.massix.github.io/v1",
      "metadata": {
        "name": "cmc1",
        "namespace": "cmc1"
      },
      "spec": {
        "enabled": true,
        "minReplicas": 0,
        "maxReplicas": 4,
        "deployment": { "name": "target-deployment" },
        "timeout": "this is not valid"
      }
    }
    `

		var cmc ChaosMonkeyConfiguration
		if err := json.Unmarshal([]byte(jsonString), &cmc); err != nil {
			t.Fatal(err)
		}

		if cmc.Spec.Timeout != 10*time.Minute {
			t.Errorf("Expected timeout to be 10 minutes (default value), got %s", cmc.Spec.Timeout)
		}

		if cmc.Spec.Deployment.Name != "target-deployment" {
			t.Errorf("Expected deployment name to be 'target-deployment', got %s", cmc.Spec.Deployment.Name)
		}
	})
}

func createCMC() *ChaosMonkeyConfiguration {
	return &ChaosMonkeyConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ChaosMonkeyConfiguration",
			APIVersion: "cm.massix.github.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: ChaosMonkeyConfigurationSpec{
			MinReplicas: 0,
			MaxReplicas: 3,
			Timeout:     30 * time.Second,
			Deployment: ChaosMonkeyConfigurationSpecDeployment{
				Name: "test",
			},
			ScalingMode: ScalingModeAntiPressure,
			Enabled:     false,
		},
	}
}

func TestChaosMonkeyConfigurationSpec_MarshalJson(t *testing.T) {
	t.Run("Can convert to JSON", func(t *testing.T) {
		cmc := createCMC()

		b, err := json.Marshal(cmc)
		if err != nil {
			t.Fatal(err)
		}

		if string(b) != `{"kind":"ChaosMonkeyConfiguration","apiVersion":"cm.massix.github.io/v1","metadata":{"name":"test","namespace":"test","creationTimestamp":null},"spec":{"enabled":false,"minReplicas":0,"maxReplicas":3,"scalingMode":"antiPressure","deployment":{"name":"test"},"timeout":"30s"}}` {
			t.Fatalf("Unexpected JSON: %s", string(b))
		}
	})
}

func TestChaosMonkeyConfiguration_ToUnstructured(t *testing.T) {
	cmc := createCMC()
	res, err := cmc.ToUnstructured()
	if err != nil {
		t.Fatal(err)
	}

	if res.GetAPIVersion() != "cm.massix.github.io/v1" {
		t.Fatal("Wrong APIVersion")
	}

	depName, _, _ := unstructured.NestedString(res.Object, "spec", "deployment", "name")
	if depName != "test" {
		t.Fatalf("Expected 'test', received %s instead", depName)
	}
}

func TestChaosMonkeyConfiguration_FromUnstructured(t *testing.T) {
	t.Run("All is good", func(t *testing.T) {
		cmc := createCMC()
		uns, _ := cmc.ToUnstructured()

		res, err := FromUnstructured(uns)
		if err != nil {
			t.Fatal(err)
		}

		if res.Spec.Deployment.Name != "test" {
			t.Fatalf("Was expecting 'test', received %s instead", res.Spec.Deployment.Name)
		}

		if res.Spec.Timeout != 30*time.Second {
			t.Fatalf("Was expecting '30s', received %s instead", res.Spec.Timeout)
		}
	})

	t.Run("Missing fields", func(t *testing.T) {
		cmc := createCMC()
		uns, _ := cmc.ToUnstructured()
		unstructured.RemoveNestedField(uns.Object, "spec", "timeout")
		unstructured.RemoveNestedField(uns.Object, "spec", "deployment", "name")

		_, err := FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != "Field .spec.deploymentName not found\nField .spec.timeout not found" {
			t.Fatalf("Received %q instead", err.Error())
		}
	})

	t.Run("Wrong fields", func(t *testing.T) {
		cmc := createCMC()
		uns, _ := cmc.ToUnstructured()
		_ = unstructured.SetNestedField(uns.Object, int64(42), "spec", "deployment", "name")

		_, err := FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != ".spec.deployment.name accessor error: 42 is of the type int64, expected string\nField .spec.deploymentName not found" {
			t.Fatalf("Received %q instead", err.Error())
		}
	})

	t.Run("Failed to parse timeout", func(t *testing.T) {
		cmc := createCMC()
		uns, _ := cmc.ToUnstructured()
		_ = unstructured.SetNestedField(uns.Object, "invalid", "spec", "timeout")

		res, err := FromUnstructured(uns)
		if err != nil {
			t.Fatal(err)
		}

		if res.Spec.Timeout != 10*time.Minute {
			t.Fatalf("Was expecting '10m', received %s instead", res.Spec.Timeout)
		}
	})

	t.Run("Wrong APIVersion", func(t *testing.T) {
		uns := &unstructured.Unstructured{}
		uns.SetKind("ChaosMonkeyConfiguration")
		uns.SetAPIVersion("wrong")

		_, err := FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != "Wrong APIVersion: wrong" {
			t.Fatalf("Received %q instead", err.Error())
		}
	})

	t.Run("Wrong Kind", func(t *testing.T) {
		uns := &unstructured.Unstructured{}
		uns.SetAPIVersion("cm.massix.github.io/v1")
		uns.SetKind("wrong")

		_, err := FromUnstructured(uns)
		if err == nil {
			t.Fatal("Was expecting error, received nil instead")
		}

		if err.Error() != "Wrong Kind: wrong" {
			t.Fatalf("Received %q instead", err.Error())
		}
	})
}
