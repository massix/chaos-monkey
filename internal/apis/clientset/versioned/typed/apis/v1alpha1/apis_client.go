// Generated code, do not touch

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"net/http"

	"github.com/massix/chaos-monkey/internal/apis/clientset/versioned/scheme"
	v1alpha1 "github.com/massix/chaos-monkey/internal/apis/v1alpha1"
	rest "k8s.io/client-go/rest"
)

type ChaosMonkeyConfigurationV1alpha1Interface interface {
	RESTClient() rest.Interface
	ChaosMonkeyConfigurationsGetter
}

// ChaosMonkeyConfigurationV1alpha1Client is used to interact with features provided by the cm.massix.github.io group.
type ChaosMonkeyConfigurationV1alpha1Client struct {
	restClient rest.Interface
}

func (c *ChaosMonkeyConfigurationV1alpha1Client) ChaosMonkeyConfigurations(namespace string) ChaosMonkeyConfigurationInterface {
	return newChaosMonkeyConfigurations(c, namespace)
}

// NewForConfig creates a new ChaosMonkeyConfigurationV1alpha1Client for the given config.
// NewForConfig is equivalent to NewForConfigAndClient(c, httpClient),
// where httpClient was generated with rest.HTTPClientFor(c).
func NewForConfig(c *rest.Config) (*ChaosMonkeyConfigurationV1alpha1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	httpClient, err := rest.HTTPClientFor(&config)
	if err != nil {
		return nil, err
	}
	return NewForConfigAndClient(&config, httpClient)
}

// NewForConfigAndClient creates a new ChaosMonkeyConfigurationV1alpha1Client for the given config and http client.
// Note the http client provided takes precedence over the configured transport values.
func NewForConfigAndClient(c *rest.Config, h *http.Client) (*ChaosMonkeyConfigurationV1alpha1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientForConfigAndClient(&config, h)
	if err != nil {
		return nil, err
	}
	return &ChaosMonkeyConfigurationV1alpha1Client{client}, nil
}

// NewForConfigOrDie creates a new ChaosMonkeyConfigurationV1alpha1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *ChaosMonkeyConfigurationV1alpha1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new ChaosMonkeyConfigurationV1alpha1Client for the given RESTClient.
func New(c rest.Interface) *ChaosMonkeyConfigurationV1alpha1Client {
	return &ChaosMonkeyConfigurationV1alpha1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1alpha1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *ChaosMonkeyConfigurationV1alpha1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
