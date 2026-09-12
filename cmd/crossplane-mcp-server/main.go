// Command crossplane-mcp-server runs a Model Context Protocol server that
// gives AI assistants read-only access to a Crossplane control plane.
package main

import (
	"os"

	"github.com/crossplane-contrib/crossplane-mcp-server/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
