// Package kube deals with locating and loading the Kubernetes client
// configuration that the MCP server uses to talk to a cluster.
package kube

import (
	"fmt"
	"sort"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Options describe how to find the clusters this server can talk to.
//
// The zero value is valid and resolves connections the same way kubectl does:
// $KUBECONFIG, then ~/.kube/config, then the in-cluster service account.
type Options struct {
	// Kubeconfig is an explicit path to a kubeconfig file. Empty means "use
	// the standard loading rules".
	Kubeconfig string
	// Context names the kubeconfig context to use by default. Empty means
	// "use the current-context".
	Context string
	// Namespace overrides the namespace associated with a context.
	Namespace string
	// Clusters restricts which contexts are exposed as targets. Empty means
	// every context in the kubeconfig. A kubeconfig holding hundreds of
	// contexts is common, and exposing all of them to a model is rarely what
	// anybody wants.
	Clusters []string
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

// Source describes where a target's credentials come from. Today a target is
// either a kubeconfig context or the pod's own service account; workload
// identity arrives as a further source rather than a change to callers.
type Source string

// The credential sources a target can use.
const (
	// SourceKubeconfig is a named context in a kubeconfig file. Contexts that
	// use an exec credential plugin, which is how Azure workload identity and
	// the cloud CLIs present themselves, are covered by this too.
	SourceKubeconfig Source = "kubeconfig"
	// SourceInCluster is the service account of the pod this server runs in.
	SourceInCluster Source = "in-cluster"
)

// InClusterTarget is the name of the target backed by the pod's own service
// account.
const InClusterTarget = "in-cluster"

// Target is one cluster this server can talk to.
type Target struct {
	// Name is what a caller passes to select this cluster.
	Name string `json:"name"`
	// Source is where the credentials come from.
	Source Source `json:"source"`
	// Server is the API server URL, which tells similar contexts apart.
	Server string `json:"server,omitempty"`
	// Namespace is the default namespace for this target.
	Namespace string `json:"namespace,omitempty"`
	// AuthProvider names the exec plugin a context uses, if any. This is how
	// an operator can see that a target authenticates through, say, the Azure
	// CLI rather than a static token.
	AuthProvider string `json:"authProvider,omitempty"`
	// Default reports whether this is the target used when none is named.
	Default bool `json:"default"`
}

// Resolved holds a ready to use client configuration for one target.
type Resolved struct {
	RESTConfig *rest.Config
	Namespace  string
	Target     string
}

// Loader enumerates the clusters this server can reach and builds client
// configuration for any of them.
type Loader struct {
	options Options
	rules   *clientcmd.ClientConfigLoadingRules
	raw     *clientcmdapi.Config
	// inCluster is set when there is no usable kubeconfig, in which case the
	// only target is the pod's own service account.
	inCluster bool
	fallback  string
}

// NewLoader inspects the environment and returns a loader over whatever
// clusters it can see.
func NewLoader(options Options) (*Loader, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if options.Kubeconfig != "" {
		rules.ExplicitPath = options.Kubeconfig
	}

	loader := &Loader{options: options, rules: rules}

	raw, err := rules.Load()
	if err != nil || raw == nil || len(raw.Contexts) == 0 {
		// No kubeconfig is not an error: running inside a cluster is a normal
		// deployment and the service account is enough.
		if _, inClusterErr := rest.InClusterConfig(); inClusterErr != nil {
			return nil, fmt.Errorf("no kubeconfig and no in-cluster credentials: %w", inClusterErr)
		}
		loader.inCluster = true
		loader.fallback = InClusterTarget
		return loader, nil
	}
	loader.raw = raw

	loader.fallback = options.Context
	if loader.fallback == "" {
		loader.fallback = raw.CurrentContext
	}
	if loader.fallback == "" {
		return nil, fmt.Errorf("the kubeconfig has no current-context, pass --context")
	}
	if _, ok := raw.Contexts[loader.fallback]; !ok {
		return nil, fmt.Errorf("context %q is not in the kubeconfig", loader.fallback)
	}
	return loader, nil
}

// Default returns the target used when a caller does not name one.
func (l *Loader) Default() string { return l.fallback }

// Targets lists the clusters this server can talk to.
func (l *Loader) Targets() []Target {
	if l.inCluster {
		return []Target{{
			Name:      InClusterTarget,
			Source:    SourceInCluster,
			Namespace: l.inClusterNamespace(),
			Default:   true,
		}}
	}

	targets := make([]Target, 0, len(l.raw.Contexts))
	for name, context := range l.raw.Contexts {
		if !l.allowed(name) {
			continue
		}
		target := Target{
			Name:      name,
			Source:    SourceKubeconfig,
			Namespace: context.Namespace,
			Default:   name == l.fallback,
		}
		if cluster, ok := l.raw.Clusters[context.Cluster]; ok {
			target.Server = cluster.Server
		}
		if user, ok := l.raw.AuthInfos[context.AuthInfo]; ok && user.Exec != nil {
			target.AuthProvider = user.Exec.Command
		}
		targets = append(targets, target)
	}

	sort.Slice(targets, func(i, j int) bool {
		// The default sorts first so a caller reading a long list sees the
		// one they get by default immediately.
		if targets[i].Default != targets[j].Default {
			return targets[i].Default
		}
		return targets[i].Name < targets[j].Name
	})
	return targets
}

// Resolve builds the client configuration for one target. An empty name
// selects the default target.
func (l *Loader) Resolve(name string) (*Resolved, error) {
	if name == "" {
		name = l.fallback
	}

	if l.inCluster {
		if name != InClusterTarget {
			return nil, fmt.Errorf("this server has no kubeconfig, its only cluster is %q", InClusterTarget)
		}
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("cannot use the in-cluster credentials: %w", err)
		}
		l.applyDefaults(config)
		return &Resolved{RESTConfig: config, Namespace: l.inClusterNamespace(), Target: name}, nil
	}

	if _, ok := l.raw.Contexts[name]; !ok {
		return nil, fmt.Errorf("unknown cluster %q, call crossplane_clusters_list to see the available ones", name)
	}
	if !l.allowed(name) {
		return nil, fmt.Errorf("cluster %q exists in the kubeconfig but is not exposed by this server", name)
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: name}
	if l.options.Namespace != "" {
		overrides.Context.Namespace = l.options.Namespace
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(l.rules, overrides)

	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot load the configuration for cluster %q: %w", name, err)
	}
	l.applyDefaults(config)

	// A missing namespace is not fatal: cluster scoped tools still work, and
	// namespaced ones fall back to "default".
	namespace, _, err := clientConfig.Namespace()
	if err != nil || namespace == "" {
		namespace = "default"
	}
	return &Resolved{RESTConfig: config, Namespace: namespace, Target: name}, nil
}

func (l *Loader) allowed(name string) bool {
	if len(l.options.Clusters) == 0 {
		return true
	}
	for _, candidate := range l.options.Clusters {
		if candidate == name {
			return true
		}
	}
	return false
}

func (l *Loader) applyDefaults(config *rest.Config) {
	if config.QPS == 0 {
		config.QPS = defaultQPS
	}
	if config.Burst == 0 {
		config.Burst = defaultBurst
	}
	if l.options.QPS > 0 {
		config.QPS = l.options.QPS
	}
	if l.options.Burst > 0 {
		config.Burst = l.options.Burst
	}
	config.UserAgent = rest.DefaultKubernetesUserAgent() + " crossplane-mcp-server"
}

// inClusterNamespace reads the namespace the pod runs in.
func (l *Loader) inClusterNamespace() string {
	if l.options.Namespace != "" {
		return l.options.Namespace
	}
	namespace, _, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		l.rules, &clientcmd.ConfigOverrides{}).Namespace()
	if err != nil || namespace == "" {
		return "default"
	}
	return namespace
}
