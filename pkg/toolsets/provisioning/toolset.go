// Package provisioning exposes the tools that change a Crossplane control
// plane: asking a platform API for a database or a workload, and applying a
// manifest.
//
// Every tool here mutates, so the server only registers them when it was
// started with --read-only=false. None of them deletes: an assistant that can
// create a database and cannot remove one has a blast radius an operator can
// reason about. Nothing in this package assumes a particular platform API
// exists either: what a "database" means on a control plane is decided by
// whichever XRD that control plane installs, and the tools discover it.
package provisioning

import (
	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"
)

// Toolset is the "provisioning" toolset.
type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

// Name implements api.Toolset.
func (t *Toolset) Name() string { return "provisioning" }

// Description implements api.Toolset.
func (t *Toolset) Description() string {
	return "Create and update resources on the control plane: databases, workloads and Crossplane manifests. " +
		"Nothing here deletes. Withheld unless the server runs with --read-only=false."
}

// Tools implements api.Toolset.
func (t *Toolset) Tools() []api.Tool {
	tools := make([]api.Tool, 0, 4)
	tools = append(tools, databaseTools()...)
	tools = append(tools, workloadTools()...)
	tools = append(tools, manifestTools()...)
	return tools
}

func init() {
	toolsets.Register(&Toolset{})
}
