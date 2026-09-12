// Package config exposes the tools that describe how a control plane is
// configured: environment configs, runtime configs, and the Crossplane v2
// types that decide which managed resources exist at all.
package config

import (
	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"
)

// Toolset is the "config" toolset.
type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

// Name implements api.Toolset.
func (t *Toolset) Name() string { return "config" }

// Description implements api.Toolset.
func (t *Toolset) Description() string {
	return "Control plane configuration: EnvironmentConfigs, DeploymentRuntimeConfigs, " +
		"ManagedResourceDefinitions and activation policies."
}

// Tools implements api.Toolset.
func (t *Toolset) Tools() []api.Tool {
	return configTools()
}

func init() {
	toolsets.Register(&Toolset{})
}
