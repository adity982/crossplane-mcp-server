package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
)

func configTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_environment_configs_list",
			Title: "EnvironmentConfigs: list",
			Description: "List the EnvironmentConfigs on the control plane with the data keys each one holds. " +
				"Compositions read these at render time to pick up values that differ between environments, " +
				"such as region or account. A Composition that selects a missing EnvironmentConfig fails to " +
				"render, so check here when a composite resource will not compose.",
			InputSchema: api.Object(nil),
			Handler:     environmentConfigsList,
		},
		{
			Name:  "crossplane_deployment_runtime_configs_list",
			Title: "DeploymentRuntimeConfigs: list",
			Description: "List the DeploymentRuntimeConfigs and which providers or functions reference each " +
				"one. A runtime config customises the Deployment that runs a package, for example its service " +
				"account or resource limits. A config nothing references is a common reason settings appear " +
				"not to apply.",
			InputSchema: api.Object(nil),
			Handler:     deploymentRuntimeConfigsList,
		},
		{
			Name:  "crossplane_managed_resource_definitions_list",
			Title: "ManagedResourceDefinitions: list",
			Description: "List the ManagedResourceDefinitions on a Crossplane v2 control plane. An MRD decides " +
				"whether a provider's managed resource kind is installed at all: an Inactive definition " +
				"installs no CRD, so the kind does not exist. This is the first thing to check when a provider " +
				"is Healthy but the kind you expect is missing.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"state": api.EnumProp("Only show definitions in this state.", "Active", "Inactive"),
			}),
			Handler: managedResourceDefinitionsList,
		},
		{
			Name:  "crossplane_managed_resource_activation_policies_list",
			Title: "ManagedResourceActivationPolicies: list",
			Description: "List the ManagedResourceActivationPolicies, which activate ManagedResourceDefinitions " +
				"by name pattern on a Crossplane v2 control plane. Use this to understand why a particular " +
				"managed resource kind is or is not active.",
			InputSchema: api.Object(nil),
			Handler:     activationPoliciesList,
		},
	}
}

func environmentConfigsList(p api.Params) (*api.Result, error) {
	configs, err := p.Client.EnvironmentConfigs(p)
	if err != nil {
		return api.Error(err), nil
	}
	if len(configs) == 0 {
		return api.Structured("No EnvironmentConfigs are defined.",
			map[string]any{"count": 0, "environmentConfigs": configs}), nil
	}

	rows := make([][]string, 0, len(configs))
	for _, config := range configs {
		rows = append(rows, []string{
			config.Name, strconv.Itoa(len(config.Keys)), api.OrDash(strings.Join(config.Keys, ", ")), config.Age,
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d EnvironmentConfig(s).\n", len(configs))
	text.WriteString(api.Table([]string{"NAME", "KEYS", "DATA KEYS", "AGE"}, rows))

	return api.Structured(text.String(),
		map[string]any{"count": len(configs), "environmentConfigs": configs}), nil
}

func deploymentRuntimeConfigsList(p api.Params) (*api.Result, error) {
	configs, err := p.Client.DeploymentRuntimeConfigs(p)
	if err != nil {
		return api.Error(err), nil
	}
	if len(configs) == 0 {
		return api.Structured("No DeploymentRuntimeConfigs are defined.",
			map[string]any{"count": 0, "runtimeConfigs": configs}), nil
	}

	rows := make([][]string, 0, len(configs))
	unused := 0
	for _, config := range configs {
		if len(config.UsedBy) == 0 {
			unused++
		}
		rows = append(rows, []string{
			config.Name,
			api.OrDash(config.ServiceAccount),
			api.OrDash(strings.Join(config.UsedBy, ", ")),
			config.Age,
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d DeploymentRuntimeConfig(s), %d referenced by no package.\n", len(configs), unused)
	text.WriteString(api.Table([]string{"NAME", "SERVICE ACCOUNT", "USED BY", "AGE"}, rows))

	return api.Structured(text.String(),
		map[string]any{"count": len(configs), "unused": unused, "runtimeConfigs": configs}), nil
}

func managedResourceDefinitionsList(p api.Params) (*api.Result, error) {
	state := p.Args.OptionalEnum("state", "", "Active", "Inactive")
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	definitions, err := p.Client.ManagedResourceDefinitions(p, state)
	if err != nil {
		return api.Error(err), nil
	}
	if len(definitions) == 0 {
		return api.Structured("No ManagedResourceDefinitions matched. "+
			"They only exist on Crossplane v2 control planes.",
			map[string]any{"count": 0, "managedResourceDefinitions": definitions}), nil
	}

	rows := make([][]string, 0, len(definitions))
	active := 0
	for _, definition := range definitions {
		if definition.State == "Active" {
			active++
		}
		rows = append(rows, []string{
			definition.Name, definition.ManagedKind, definition.Group,
			api.OrDash(definition.State), definition.Ready, definition.Age,
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d ManagedResourceDefinition(s), %d Active.\n", len(definitions), active)
	text.WriteString(api.Table(
		[]string{"NAME", "KIND", "GROUP", "STATE", "ESTABLISHED", "AGE"}, rows))

	return api.Structured(text.String(),
		map[string]any{"count": len(definitions), "active": active, "managedResourceDefinitions": definitions}), nil
}

func activationPoliciesList(p api.Params) (*api.Result, error) {
	policies, err := p.Client.ManagedResourceActivationPolicies(p)
	if err != nil {
		return api.Error(err), nil
	}
	if len(policies) == 0 {
		return api.Structured("No ManagedResourceActivationPolicies are defined. "+
			"They only exist on Crossplane v2 control planes.",
			map[string]any{"count": 0, "activationPolicies": policies}), nil
	}

	rows := make([][]string, 0, len(policies))
	for _, policy := range policies {
		rows = append(rows, []string{
			policy.Name,
			api.OrDash(strings.Join(policy.Activate, ", ")),
			strconv.Itoa(len(policy.Activated)),
			policy.Ready,
			policy.Age,
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d ManagedResourceActivationPolicy(ies).\n", len(policies))
	text.WriteString(api.Table([]string{"NAME", "ACTIVATES", "ACTIVATED", "READY", "AGE"}, rows))

	return api.Structured(text.String(),
		map[string]any{"count": len(policies), "activationPolicies": policies}), nil
}
