package crossplane

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GroupOps is the API group of the Crossplane v2 operations types.
const GroupOps = "ops.crossplane.io"

// EnvironmentConfig is the summarised view of an EnvironmentConfig, which
// holds data that Compositions read at render time.
type EnvironmentConfig struct {
	Summary `json:",inline"`

	// Keys are the top level keys of spec.data, which is what a Composition
	// selects from.
	Keys []string `json:"keys,omitempty"`
}

// EnvironmentConfigs lists the EnvironmentConfigs on the control plane.
func (c *Client) EnvironmentConfigs(ctx context.Context) ([]EnvironmentConfig, error) {
	list, err := c.listKind(ctx, "EnvironmentConfig", GroupAPIExtensions)
	if err != nil {
		return nil, err
	}

	configs := make([]EnvironmentConfig, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		configs = append(configs, EnvironmentConfig{
			Summary: Summarize(obj),
			Keys:    sortedKeys(nestedMap(obj, "spec", "data")),
		})
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	return configs, nil
}

// DeploymentRuntimeConfig is the summarised view of a DeploymentRuntimeConfig,
// which customises the Deployment that runs a provider or a function.
type DeploymentRuntimeConfig struct {
	Summary `json:",inline"`

	ServiceAccount string `json:"serviceAccount,omitempty"`
	// UsedBy lists the packages that reference this runtime config. An unused
	// runtime config is a common cause of "my settings did not apply".
	UsedBy []string `json:"usedBy,omitempty"`
}

// DeploymentRuntimeConfigs lists the runtime configs and works out which
// packages actually reference each one.
func (c *Client) DeploymentRuntimeConfigs(ctx context.Context) ([]DeploymentRuntimeConfig, error) {
	list, err := c.listKind(ctx, "DeploymentRuntimeConfig", GroupPkg)
	if err != nil {
		return nil, err
	}

	users := c.runtimeConfigUsers(ctx)
	configs := make([]DeploymentRuntimeConfig, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		configs = append(configs, DeploymentRuntimeConfig{
			Summary:        Summarize(obj),
			ServiceAccount: nestedString(obj, "spec", "serviceAccountTemplate", "metadata", "name"),
			UsedBy:         users[obj.GetName()],
		})
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	return configs, nil
}

// runtimeConfigUsers maps a runtime config name to the packages using it.
func (c *Client) runtimeConfigUsers(ctx context.Context) map[string][]string {
	users := map[string][]string{}
	for _, kind := range []PackageKind{PackageProvider, PackageFunction} {
		resource, err := c.ResolveKind(ctx, string(kind), GroupPkg)
		if err != nil {
			continue
		}
		list, err := c.List(ctx, resource, ListOptions{})
		if err != nil {
			continue
		}
		for i := range list.Items {
			obj := &list.Items[i]
			if ref := nestedString(obj, "spec", "runtimeConfigRef", "name"); ref != "" {
				users[ref] = append(users[ref], string(kind)+"/"+obj.GetName())
			}
		}
	}
	return users
}

// ManagedResourceDefinition is the summarised view of an MRD, the Crossplane
// v2 type that decides which of a provider's managed resources are installed.
type ManagedResourceDefinition struct {
	Summary `json:",inline"`

	Group string `json:"group"`
	// Kind is the managed resource this definition installs.
	ManagedKind string `json:"managedKind"`
	// State is Active or Inactive. An Inactive definition installs no CRD, so
	// the kind simply does not exist on the control plane.
	State string `json:"state"`
}

// ManagedResourceDefinitions lists the MRDs, optionally only those in a state.
func (c *Client) ManagedResourceDefinitions(ctx context.Context, state string) ([]ManagedResourceDefinition, error) {
	list, err := c.listKind(ctx, "ManagedResourceDefinition", GroupAPIExtensions)
	if err != nil {
		return nil, err
	}

	definitions := make([]ManagedResourceDefinition, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		definition := ManagedResourceDefinition{
			Summary:     Summarize(obj),
			Group:       nestedString(obj, "spec", "group"),
			ManagedKind: nestedString(obj, "spec", "names", "kind"),
			State:       nestedString(obj, "spec", "state"),
		}
		if state != "" && !equalFoldSafe(definition.State, state) {
			continue
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions, nil
}

// ManagedResourceActivationPolicy is the summarised view of an MRAP, which
// activates managed resource definitions by name pattern.
type ManagedResourceActivationPolicy struct {
	Summary `json:",inline"`

	// Activate holds the name patterns this policy activates.
	Activate []string `json:"activate,omitempty"`
	// Activated lists the definitions the policy actually matched.
	Activated []string `json:"activated,omitempty"`
}

// ManagedResourceActivationPolicies lists the MRAPs on the control plane.
func (c *Client) ManagedResourceActivationPolicies(ctx context.Context) ([]ManagedResourceActivationPolicy, error) {
	list, err := c.listKind(ctx, "ManagedResourceActivationPolicy", GroupAPIExtensions)
	if err != nil {
		return nil, err
	}

	policies := make([]ManagedResourceActivationPolicy, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		policies = append(policies, ManagedResourceActivationPolicy{
			Summary:   Summarize(obj),
			Activate:  stringSlice(anyOf(nestedSlice(obj, "spec", "activate"))),
			Activated: stringSlice(anyOf(nestedSlice(obj, "status", "activated"))),
		})
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].Name < policies[j].Name })
	return policies, nil
}

// listKind resolves a kind and lists it, so that the callers above stay short.
func (c *Client) listKind(ctx context.Context, kind, group string) (*unstructured.UnstructuredList, error) {
	resource, err := c.ResolveKind(ctx, kind, group)
	if err != nil {
		return nil, err
	}
	return c.List(ctx, resource, ListOptions{})
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// anyOf adapts a []any so it can be passed to stringSlice.
func anyOf(items []any) any {
	if items == nil {
		return nil
	}
	return items
}

func equalFoldSafe(a, b string) bool {
	return len(a) == len(b) && (a == b || containsFold(a, b))
}
