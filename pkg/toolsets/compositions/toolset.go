// Package compositions exposes the tools that describe the platform APIs a
// control plane offers: CompositeResourceDefinitions and Compositions.
package compositions

import (
	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"
)

// Toolset is the "compositions" toolset.
type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

// Name implements api.Toolset.
func (t *Toolset) Name() string { return "compositions" }

// Description implements api.Toolset.
func (t *Toolset) Description() string {
	return "Platform API definitions: CompositeResourceDefinitions (XRDs), Compositions, their schemas, " +
		"static validation and dry-run rendering."
}

// Tools implements api.Toolset.
func (t *Toolset) Tools() []api.Tool {
	return append(compositionTools(), advancedTools()...)
}

func init() {
	toolsets.Register(&Toolset{})
}
