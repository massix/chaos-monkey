package endpoints

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	v1 "github.com/massix/chaos-monkey/internal/apis/v1"
	"github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ConversionEndpoint struct {
	Logrus logrus.FieldLogger
}

var _ = (http.Handler)((*ConversionEndpoint)(nil))

type conversionReview struct {
	metav1.TypeMeta `json:",inline"`
	Request         *conversionReviewRequest  `json:"request,omitempty"`
	Response        *conversionReviewResponse `json:"response,omitempty"`
}

// I can safely say that I will always accept a v1alpha1 version of the APIs
type conversionReviewRequest struct {
	Id                string                       `json:"uid"`
	DesiredAPIVersion string                       `json:"desiredAPIVersion"`
	Objects           []*unstructured.Unstructured `json:"objects"`
}

// I can safely say that I will always reply with v1 version of the APIs
type conversionReviewResponse struct {
	Id               string                          `json:"uid"`
	Result           *conversionReviewResponseResult `json:"result"`
	ConvertedObjects []*unstructured.Unstructured    `json:"convertedObjects"`
}

type conversionReviewResponseResult struct {
	Status  string `json:"status"` // Either "Success" or "Failure"
	Message string `json:"message,omitempty"`
}

func newConversionReviewFailuref(id, format string, args ...interface{}) *conversionReview {
	return &conversionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConversionReview",
			APIVersion: "apiextensions.k8s.io/v1",
		},
		Response: &conversionReviewResponse{
			Id: id,
			Result: &conversionReviewResponseResult{
				Status:  metav1.StatusFailure,
				Message: fmt.Sprintf(format, args...),
			},
		},
	}
}

func newConversionReviewSuccess(id string, convertedObjects []*unstructured.Unstructured) *conversionReview {
	return &conversionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConversionReview",
			APIVersion: "apiextensions.k8s.io/v1",
		},
		Response: &conversionReviewResponse{
			Id: id,
			Result: &conversionReviewResponseResult{
				Status: metav1.StatusSuccess,
			},
			ConvertedObjects: convertedObjects,
		},
	}
}

func getField[T any](result T, found bool, err error) (T, error) {
	if !found {
		return *new(T), fmt.Errorf("Field not found")
	}

	if err != nil {
		return *new(T), err
	}

	return result, nil
}

func (c *ConversionEndpoint) convertFromV1Alpha1ToV1(objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	var res []*unstructured.Unstructured

	for _, obj := range objects {
		cmv1alpha1, err := v1alpha1.FromUnstructured(obj)
		if err != nil {
			return nil, err
		}

		cmv1 := &v1.ChaosMonkeyConfiguration{}
		cmv1.TypeMeta = cmv1alpha1.TypeMeta
		cmv1.ObjectMeta = cmv1alpha1.ObjectMeta

		cmv1.APIVersion = "cm.massix.github.io/v1"

		cmv1.Spec = v1.ChaosMonkeyConfigurationSpec{
			Deployment:  v1.ChaosMonkeyConfigurationSpecDeployment{Name: cmv1alpha1.Spec.DeploymentName},
			MinReplicas: cmv1alpha1.Spec.MinReplicas,
			MaxReplicas: cmv1alpha1.Spec.MaxReplicas,
			Enabled:     cmv1alpha1.Spec.Enabled,
		}

		if parsedTimeout, err := time.ParseDuration(cmv1alpha1.Spec.Timeout); err == nil {
			cmv1.Spec.Timeout = parsedTimeout
		} else {
			c.Logrus.Errorf("While parsing %q: %s, using default value 10m", cmv1alpha1.Spec.Timeout, err)
			cmv1.Spec.Timeout = 10 * time.Minute
		}

		scalingMode := v1.ScalingModeRandomScale
		if cmv1alpha1.Spec.PodMode {
			scalingMode = v1.ScalingModeKillPod
		}

		cmv1.Spec.ScalingMode = scalingMode
		if uns, err := cmv1.ToUnstructured(); err != nil {
			return nil, err
		} else {
			res = append(res, uns)
		}
	}

	return res, nil
}

func (c *ConversionEndpoint) convertFromV1ToV1Alpha1(objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	var res []*unstructured.Unstructured
	for _, obj := range objects {
		cmv1, err := v1.FromUnstructured(obj)
		if err != nil {
			return nil, err
		}

		cmv1alpha1 := &v1alpha1.ChaosMonkeyConfiguration{}
		cmv1alpha1.TypeMeta = cmv1.TypeMeta
		cmv1alpha1.ObjectMeta = cmv1.ObjectMeta

		cmv1alpha1.TypeMeta.APIVersion = "cm.massix.github.io/v1alpha1"

		cmv1alpha1.Spec = v1alpha1.ChaosMonkeyConfigurationSpec{
			DeploymentName: cmv1.Spec.Deployment.Name,
			MinReplicas:    cmv1.Spec.MinReplicas,
			MaxReplicas:    cmv1.Spec.MaxReplicas,
			Enabled:        cmv1.Spec.Enabled,
			Timeout:        cmv1.Spec.Timeout.String(),
			PodMode:        cmv1.Spec.ScalingMode == v1.ScalingModeKillPod,
		}

		if uns, err := cmv1alpha1.ToUnstructured(); err != nil {
			return nil, err
		} else {
			res = append(res, uns)
		}
	}

	return res, nil
}

// ServeHTTP implements http.Handler.
func (c *ConversionEndpoint) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var in conversionReview

	err := json.NewDecoder(req.Body).Decode(&in)
	if err != nil {
		failure := newConversionReviewFailuref("", "Failed to decode: %v", err)
		_ = json.NewEncoder(w).Encode(failure)
		return
	}

	switch in.Request.DesiredAPIVersion {
	case "cm.massix.github.io/v1alpha1":
		c.Logrus.Info("Converting to v1alpha1")
		result, err := c.convertFromV1ToV1Alpha1(in.Request.Objects)
		if err != nil {
			failure := newConversionReviewFailuref(in.Request.Id, "Failed to convert: %v", err)
			_ = json.NewEncoder(w).Encode(failure)
			return
		}

		success := newConversionReviewSuccess(in.Request.Id, result)
		_ = json.NewEncoder(w).Encode(success)
	case "cm.massix.github.io/v1":
		c.Logrus.Info("Converting to v1")
		result, err := c.convertFromV1Alpha1ToV1(in.Request.Objects)
		if err != nil {
			failure := newConversionReviewFailuref(in.Request.Id, "Failed to convert: %v", err)
			_ = json.NewEncoder(w).Encode(failure)
		}

		success := newConversionReviewSuccess(in.Request.Id, result)
		_ = json.NewEncoder(w).Encode(success)
	}

	c.Logrus.Info("Done")
}

func NewConversionEndpoint() *ConversionEndpoint {
	return &ConversionEndpoint{
		Logrus: logrus.WithFields(logrus.Fields{"component": "ConversionEndpoint"}),
	}
}
