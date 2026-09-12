// Package diagnostics exposes the tools that give an overall picture of a
// control plane's health, and that find the things which are broken.
package diagnostics

import (
	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"
)

// Toolset is the "diagnostics" toolset.
type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

// Name implements api.Toolset.
func (t *Toolset) Name() string { return "diagnostics" }

// Description implements api.Toolset.
func (t *Toolset) Description() string {
	return "Control plane health: overall status, everything that is failing, and the Crossplane API surface."
}

// Tools implements api.Toolset.
func (t *Toolset) Tools() []api.Tool {
	return diagnosticTools()
}

func init() {
	toolsets.Register(&Toolset{})
}
