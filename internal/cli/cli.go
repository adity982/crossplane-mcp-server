// Package cli implements the crossplane-mcp-server command line.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/mcp"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/version"

	// Importing the toolsets registers them.
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/compositions"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/config"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/diagnostics"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/packages"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/provisioning"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/resources"
)

type options struct {
	kubeconfig  string
	context     string
	namespace   string
	clusters    []string
	toolsets    []string
	readOnly    bool
	httpAddress string
	logLevel    string
	toolTimeout time.Duration
	showVersion bool
}

// Execute runs the root command. It returns the process exit code.
func Execute() int {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newRootCommand() *cobra.Command {
	opts := &options{}

	cmd := &cobra.Command{
		Use:   version.BinaryName,
		Short: "Model Context Protocol server for Crossplane control planes",
		Long: `crossplane-mcp-server exposes a Crossplane control plane to MCP clients.

It speaks the Model Context Protocol over stdio by default, which is what
editors and desktop assistants expect. Pass --http-address to serve the
streamable HTTP transport instead, for example when running inside the cluster
being inspected.

The server is read-only unless you say otherwise. Tools that create or update
are not registered at all until --read-only=false is passed, so a client never
even sees that they exist. No tool deletes anything, in either mode.`,
		Example: `  # Run over stdio against the current kubeconfig context
  crossplane-mcp-server

  # Inspect a specific cluster, exposing only the diagnostics tools
  crossplane-mcp-server --context prod --toolsets diagnostics

  # Allow the assistant to provision things on a development control plane
  crossplane-mcp-server --context dev --read-only=false

  # Serve over HTTP
  crossplane-mcp-server --http-address :8080`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opts)
		},
	}

	// Persistent, so that 'call' can be pointed at the same cluster, toolsets
	// and mode as the server itself.
	flags := cmd.PersistentFlags()
	flags.StringVar(&opts.kubeconfig, "kubeconfig", "",
		"Path to a kubeconfig file. Defaults to $KUBECONFIG, then ~/.kube/config, then in-cluster credentials.")
	flags.StringVar(&opts.context, "context", "",
		"Name of the kubeconfig context to use by default. Defaults to the current context.")
	flags.StringSliceVar(&opts.clusters, "clusters", nil,
		"Comma separated kubeconfig contexts to expose as targets. Defaults to every context. "+
			"Tools take a 'cluster' argument to choose between them.")
	flags.StringVar(&opts.namespace, "namespace", "",
		"Default namespace for namespaced resources. Defaults to the namespace of the selected context.")
	flags.StringSliceVar(&opts.toolsets, "toolsets", nil,
		"Comma separated toolsets to expose. Defaults to all of them. Available: "+strings.Join(toolsets.Names(), ", "))
	flags.BoolVar(&opts.readOnly, "read-only", true,
		"Withhold every tool that changes the control plane. Pass --read-only=false to let the assistant create "+
			"and update resources, which also needs RBAC that allows it. No tool deletes in either mode.")
	flags.StringVar(&opts.httpAddress, "http-address", "",
		"Serve the streamable HTTP transport on this address instead of stdio, for example ':8080'.")
	flags.StringVar(&opts.logLevel, "log-level", "info",
		"Log verbosity: debug, info, warn or error. Logs always go to stderr.")
	flags.DurationVar(&opts.toolTimeout, "tool-timeout", 2*time.Minute,
		"Maximum time a single tool call may run. Set to 0 to disable.")
	cmd.Flags().BoolVar(&opts.showVersion, "version", false, "Print the version and exit.")

	cmd.AddCommand(newToolsCommand())
	cmd.AddCommand(newPromptsCommand())
	cmd.AddCommand(newCallCommand(opts))
	return cmd
}

func run(ctx context.Context, opts *options) error {
	if opts.showVersion {
		fmt.Println(version.String())
		return nil
	}

	logger := newLogger(opts.logLevel)

	server, err := buildServer(opts)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.httpAddress != "" {
		return serveHTTP(ctx, server, opts.httpAddress, logger)
	}
	return server.ServeStdio(ctx)
}

func serveHTTP(ctx context.Context, server *mcp.Server, address string, logger *slog.Logger) error {
	mux := http.NewServeMux()
	mux.Handle("/mcp", server.HTTPHandler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	httpServer := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("serving MCP over HTTP", "address", address, "endpoint", "/mcp")
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server failed: %w", err)
	}
	return nil
}

func newLogger(level string) *slog.Logger {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		parsed = slog.LevelInfo
	}
	// stdout belongs to the MCP protocol when running over stdio, so logs go
	// to stderr unconditionally.
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parsed}))
}

func newToolsCommand() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List the tools this server exposes, grouped by toolset",
		Long: "Print every tool this build exposes without connecting to a cluster. " +
			"Useful for writing documentation and for checking what --toolsets would select. " +
			"Tools marked [write] are only registered when the server runs with --read-only=false.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if asJSON {
				return writeToolsJSON(cmd.OutOrStdout())
			}

			var out strings.Builder
			for _, toolset := range toolsets.All() {
				fmt.Fprintf(&out, "%s: %s\n", toolset.Name(), toolset.Description())
				for _, tool := range toolset.Tools() {
					fmt.Fprintf(&out, "  %-40s %s%s\n", tool.Name, marker(tool), summarise(tool))
				}
				out.WriteString("\n")
			}
			// Reporting the write error matters here: piping into head closes
			// the pipe early and we should exit rather than carry on.
			_, err := io.WriteString(cmd.OutOrStdout(), out.String())
			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false,
		"Print the full MCP tool declarations, including input schemas and annotations.")

	return cmd
}

// writeToolsJSON prints the tool declarations exactly as a client would see
// them in a tools/list reply, which is what bundle manifests need to embed.
func writeToolsJSON(w io.Writer) error {
	declarations := []*sdk.Tool{}
	for _, toolset := range toolsets.All() {
		for _, tool := range toolset.Tools() {
			declarations = append(declarations, mcp.Declaration(tool))
		}
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(declarations)
}

func newPromptsCommand() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "List the guided workflows this server offers",
		Long: "Print the prompts this build exposes without connecting to a cluster. Prompts describe the " +
			"order an experienced operator would call the tools in.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			declarations := mcp.PromptDeclarations()
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(declarations)
			}

			var out strings.Builder
			for _, prompt := range declarations {
				fmt.Fprintf(&out, "%-24s %s\n", prompt.Name, prompt.Description)
				for _, arg := range prompt.Arguments {
					required := "optional"
					if arg.Required {
						required = "required"
					}
					fmt.Fprintf(&out, "  %-12s %-8s %s\n", arg.Name, required, arg.Description)
				}
				out.WriteString("\n")
			}
			_, err := io.WriteString(cmd.OutOrStdout(), out.String())
			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the full MCP prompt declarations.")

	return cmd
}

// summarise reduces a tool description to its first sentence, which is all
// that fits on a terminal line.
func summarise(tool api.Tool) string {
	if idx := strings.Index(tool.Description, ". "); idx > 0 {
		return tool.Description[:idx+1]
	}
	return tool.Description
}

// marker flags the tools a read-only server withholds, so that a listing taken
// without a cluster still says which tools need writes enabled.
func marker(tool api.Tool) string {
	if tool.Mutates() {
		return "[write] "
	}
	return ""
}
