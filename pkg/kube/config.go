// Package kube deals with locating and loading the Kubernetes client
// configuration that the MCP server uses to talk to a cluster.
package kube

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Options describe how to build a *rest.Config.
//
// The zero value is valid and resolves the connection the same way kubectl
// does: $KUBECONFIG, then ~/.kube/config, then the in-cluster service account.
type Options struct {
	// Kubeconfig is an explicit path to a kubeconfig file. Empty means "use
	// the standard loading rules".
	Kubeconfig string
	// Context selects a named context from the kubeconfig. Empty means "use
	// the current-context".
	Context string
	// Namespace overrides the namespace associated with the selected context.
	Namespace string
	// QPS and Burst throttle client-side requests. Zero values fall back to
	// the defaults below, which are more generous than client-go's because a
	// single MCP tool call may fan out over many API groups.
	QPS   float32
	Burst int
}

const (
	defaultQPS   = 50
	defaultBurst = 100
)

// Resolved holds the outcome of loading the client configuration.
type Resolved struct {
	// RESTConfig is ready to be handed to a client-go constructor.
	RESTConfig *rest.Config
	// Namespace is the namespace the caller should default to when a tool
	// does not specify one.
	Namespace string
	// Context is the name of the kubeconfig context that was selected, or an
	// empty string when running in-cluster.
	Context string
}

// Load builds the REST configuration described by opts.
func Load(opts Options) (*Resolved, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		loadingRules.ExplicitPath = opts.Kubeconfig
	}

	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}
	if opts.Namespace != "" {
		overrides.Context.Namespace = opts.Namespace
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot load kubernetes client configuration: %w", err)
	}
	if restConfig.QPS == 0 {
		restConfig.QPS = defaultQPS
	}
	if restConfig.Burst == 0 {
		restConfig.Burst = defaultBurst
	}
	if opts.QPS > 0 {
		restConfig.QPS = opts.QPS
	}
	if opts.Burst > 0 {
		restConfig.Burst = opts.Burst
	}
	restConfig.UserAgent = rest.DefaultKubernetesUserAgent() + " crossplane-mcp-server"

	// A missing or unreadable namespace is not fatal: cluster scoped tools
	// still work, and namespaced ones fall back to "default".
	namespace, _, err := clientConfig.Namespace()
	if err != nil || namespace == "" {
		namespace = "default"
	}

	resolved := &Resolved{
		RESTConfig: restConfig,
		Namespace:  namespace,
		Context:    opts.Context,
	}
	if resolved.Context == "" {
		if raw, err := clientConfig.RawConfig(); err == nil {
			resolved.Context = raw.CurrentContext
		}
	}
	return resolved, nil
}
