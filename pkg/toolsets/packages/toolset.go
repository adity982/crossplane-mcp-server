// Package packages exposes the tools that answer questions about the
// Crossplane packages installed on a control plane: providers, functions and
// configurations.
package packages

import (
	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/api"
	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/toolsets"
)

// Toolset is the "packages" toolset.
type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

// Name implements api.Toolset.
func (t *Toolset) Name() string { return "packages" }

// Description implements api.Toolset.
func (t *Toolset) Description() string {
	return "Crossplane packages: providers, composition functions, configurations and their revisions."
}

// Tools implements api.Toolset.
func (t *Toolset) Tools() []api.Tool {
	return packageTools()
}

func init() {
	toolsets.Register(&Toolset{})
}
