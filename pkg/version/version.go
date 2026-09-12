// Package version exposes build information about the crossplane-mcp-server
// binary. The values are injected at link time by the build system; see the
// LDFLAGS variable in the Makefile.
package version

import (
	"fmt"
	"runtime"
)

// BinaryName is the canonical name of the executable. It is also reported to
// MCP clients as the server implementation name.
const BinaryName = "crossplane-mcp-server"

// Build information. Overridden via -ldflags at build time.
var (
	// Version is the semantic version of the release, e.g. "v0.3.1".
	Version = "0.0.0-dev"
	// Commit is the git SHA the binary was built from.
	Commit = "unknown"
	// BuildDate is an RFC3339 timestamp of when the binary was built.
	BuildDate = "unknown"
)

// String returns a single line summary suitable for `--version` output.
func String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s, %s/%s, %s)",
		BinaryName, Version, Commit, BuildDate, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
