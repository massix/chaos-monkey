package endpoints

import (
	"fmt"
	"testing"
	"time"

	v1 "github.com/massix/chaos-monkey/internal/apis/v1"
	"github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func createCMCv1alpha1(name, namespace string) *v1alpha1.ChaosMonkeyConfiguration {
	return &v1alpha1.ChaosMonkeyConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ChaosMonkeyConfiguration",
			APIVersion: "cm.massix.github.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ChaosMonkeyConfigurationSpec{
			MinReplicas:    1,
			MaxReplicas:    2,
			DeploymentName: "test",
			Timeout:        "30s",
			Enabled:        true,
			PodMode:        false,
		},
	}
}

func createCMCv1(name, namespace string) *v1.ChaosMonkeyConfiguration {
	return &v1.ChaosMonkeyConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ChaosMonkeyConfiguration",
			APIVersion: "cm.massix.github.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.ChaosMonkeyConfigurationSpec{
			MinReplicas: 1,
			MaxReplicas: 2,
			Deployment: v1.ChaosMonkeyConfigurationSpecDeployment{
				Name: "test",
			},
			Timeout:     30 * time.Second,
			Enabled:     true,
			ScalingMode: v1.ScalingModeKillPod,
		},
	}
}

func TestConversionEndpoint_convertFromV1Alpha1ToV1(t *testing.T) {
	ep := NewConversionEndpoint()

	t.Run("All is good", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		for i := range [10]int{} {
			r, _ := createCMCv1alpha1(fmt.Sprintf("test-%d", i), "test").ToUnstructured()
			objects = append(objects, r)
		}

		res, err := ep.convertFromV1Alpha1ToV1(objects)
		if err != nil {
			t.Fatal(err)
		}

		if len(res) != 10 {
			t.Fatalf("Expected 10, received %d", len(res))
		}

		for i, item := range res {
			conf, err := v1.FromUnstructured(item)
			if err != nil {
				t.Fatal(err)
			}

			if conf.Name != fmt.Sprintf("test-%d", i) {
				t.Fatalf("Expected 'test-%d', received %s instead", i, conf.Name)
			}

			if conf.Spec.Deployment.Name != "test" {
				t.Fatalf("Expected 'test', received %s instead", conf.Spec.Deployment.Name)
			}
		}
	})
}

func TestConversionEdnpoint_convertFromV1ToV1Alpha1(t *testing.T) {
	ep := NewConversionEndpoint()

	t.Run("All is good", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		for i := range [10]int{} {
			r, _ := createCMCv1(fmt.Sprintf("test-%d", i), "test").ToUnstructured()
			objects = append(objects, r)
		}

		res, err := ep.convertFromV1ToV1Alpha1(objects)
		if err != nil {
			t.Fatal(err)
		}

		if len(res) != 10 {
			t.Fatalf("Expected 10, received %d", len(res))
		}

		for i, item := range res {
			conf, err := v1alpha1.FromUnstructured(item)
			if err != nil {
				t.Fatal(err)
			}

			if conf.Name != fmt.Sprintf("test-%d", i) {
				t.Fatalf("Expected 'test-%d', received %s instead", i, conf.Name)
			}

			if conf.Spec.DeploymentName != "test" {
				t.Fatalf("Expected 'test', received %s instead", conf.Spec.DeploymentName)
			}
		}
	})
}
