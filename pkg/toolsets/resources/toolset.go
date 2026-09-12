// Package resources exposes the tools that answer questions about the
// resources a Crossplane control plane manages: managed resources, composite
// resources and claims.
package resources

import (
	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/api"
	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/toolsets"
)

// Toolset is the "resources" toolset.
type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

// Name implements api.Toolset.
func (t *Toolset) Name() string { return "resources" }

// Description implements api.Toolset.
func (t *Toolset) Description() string {
	return "Managed resources, composite resources (XRs) and claims: counts, listings, status and composition trees."
}

// Tools implements api.Toolset.
func (t *Toolset) Tools() []api.Tool {
	tools := make([]api.Tool, 0, 8)
	tools = append(tools, managedResourceTools()...)
	tools = append(tools, compositeResourceTools()...)
	tools = append(tools, inspectionTools()...)
	return tools
}

func init() {
	toolsets.Register(&Toolset{})
}
