package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/kube"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/mcp"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"
)

func newCallCommand(opts *options) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "call <tool> [json-arguments]",
		Short: "Call a single tool and print what it returns",
		Long: `Call one tool against your control plane without an MCP client.

Useful for checking that the server can reach your cluster, for reading what a
tool really returns rather than what a model says it returned, and for writing
documentation.

Arguments are the same JSON object an MCP client would send. Run 'tools --json'
to see the exact schema a tool accepts.`,
		Example: `  # Check the server can see your control plane
  crossplane-mcp-server call crossplane_status

  # Pass arguments as JSON
  crossplane-mcp-server call crossplane_managed_resources_list '{"status":"not-ready"}'

  # Diagnose a failing resource
  crossplane-mcp-server call crossplane_diagnose '{"kind":"Bucket","name":"app-data"}'

  # Print the structured payload a model receives instead of the text rendering
  crossplane-mcp-server call crossplane_status --json`,
		Args:          cobra.RangeArgs(1, 2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			arguments := map[string]any{}
			if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
				if err := json.Unmarshal([]byte(args[1]), &arguments); err != nil {
					return fmt.Errorf("arguments must be a JSON object: %w", err)
				}
			}

			server, err := buildServer(opts)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			result, err := server.Call(ctx, args[0], arguments)
			if err != nil {
				return err
			}

			// A tool error is the control plane answering rather than the
			// command failing, but the exit code should still say so.
			if result.Err != nil {
				return result.Err
			}

			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result.Structured)
			}
			// Reporting the write error matters here: piping into head closes
			// the pipe early and we should exit rather than carry on.
			_, err = fmt.Fprintln(cmd.OutOrStdout(), result.Text)
			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false,
		"Print the structured payload the model receives instead of the text rendering.")

	return cmd
}

// buildServer wires up the same server the stdio transport uses, so a call
// from the command line behaves exactly as it would from a client.
func buildServer(opts *options) (*mcp.Server, error) {
	selected, err := toolsets.Select(opts.toolsets)
	if err != nil {
		return nil, err
	}

	loader, err := kube.NewLoader(kube.Options{
		Kubeconfig: opts.kubeconfig,
		Context:    opts.context,
		Namespace:  opts.namespace,
		Clusters:   opts.clusters,
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewServer(mcp.Config{
		Provider:    crossplane.NewProvider(loader),
		Toolsets:    selected,
		Logger:      newLogger(opts.logLevel),
		ToolTimeout: opts.toolTimeout,
	})
}
