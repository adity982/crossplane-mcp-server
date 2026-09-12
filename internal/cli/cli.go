// Package cli implements the crossplane-mcp-server command line.
package cli

import (
	"context"
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

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/kube"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/mcp"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/version"

	// Importing the toolsets registers them.
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/compositions"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/config"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/diagnostics"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/packages"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/resources"
)

type options struct {
	kubeconfig  string
	context     string
	namespace   string
	toolsets    []string
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

Every tool is read-only. The server never creates, updates or deletes
anything on the control plane.`,
		Example: `  # Run over stdio against the current kubeconfig context
  crossplane-mcp-server

  # Inspect a specific cluster, exposing only the diagnostics tools
  crossplane-mcp-server --context prod --toolsets diagnostics

  # Serve over HTTP
  crossplane-mcp-server --http-address :8080`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.kubeconfig, "kubeconfig", "",
		"Path to a kubeconfig file. Defaults to $KUBECONFIG, then ~/.kube/config, then in-cluster credentials.")
	flags.StringVar(&opts.context, "context", "",
		"Name of the kubeconfig context to use. Defaults to the current context.")
	flags.StringVar(&opts.namespace, "namespace", "",
		"Default namespace for namespaced resources. Defaults to the namespace of the selected context.")
	flags.StringSliceVar(&opts.toolsets, "toolsets", nil,
		"Comma separated toolsets to expose. Defaults to all of them. Available: "+strings.Join(toolsets.Names(), ", "))
	flags.StringVar(&opts.httpAddress, "http-address", "",
		"Serve the streamable HTTP transport on this address instead of stdio, for example ':8080'.")
	flags.StringVar(&opts.logLevel, "log-level", "info",
		"Log verbosity: debug, info, warn or error. Logs always go to stderr.")
	flags.DurationVar(&opts.toolTimeout, "tool-timeout", 2*time.Minute,
		"Maximum time a single tool call may run. Set to 0 to disable.")
	flags.BoolVar(&opts.showVersion, "version", false, "Print the version and exit.")

	cmd.AddCommand(newToolsCommand())
	return cmd
}

func run(ctx context.Context, opts *options) error {
	if opts.showVersion {
		fmt.Println(version.String())
		return nil
	}

	logger := newLogger(opts.logLevel)

	selected, err := toolsets.Select(opts.toolsets)
	if err != nil {
		return err
	}

	resolved, err := kube.Load(kube.Options{
		Kubeconfig: opts.kubeconfig,
		Context:    opts.context,
		Namespace:  opts.namespace,
	})
	if err != nil {
		return err
	}
	logger.Info("connected", "context", resolved.Context, "namespace", resolved.Namespace)

	client, err := crossplane.New(resolved.RESTConfig, resolved.Namespace)
	if err != nil {
		return err
	}

	server, err := mcp.NewServer(mcp.Config{
		Client:      client,
		Toolsets:    selected,
		Logger:      logger,
		ToolTimeout: opts.toolTimeout,
	})
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
	return &cobra.Command{
		Use:   "tools",
		Short: "List the tools this server exposes, grouped by toolset",
		Long: "Print every tool this build exposes without connecting to a cluster. " +
			"Useful for writing documentation and for checking what --toolsets would select.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var out strings.Builder
			for _, toolset := range toolsets.All() {
				fmt.Fprintf(&out, "%s: %s\n", toolset.Name(), toolset.Description())
				for _, tool := range toolset.Tools() {
					fmt.Fprintf(&out, "  %-40s %s\n", tool.Name, summarise(tool))
				}
				out.WriteString("\n")
			}
			// Reporting the write error matters here: piping into head closes
			// the pipe early and we should exit rather than carry on.
			_, err := io.WriteString(cmd.OutOrStdout(), out.String())
			return err
		},
	}
}

// summarise reduces a tool description to its first sentence, which is all
// that fits on a terminal line.
func summarise(tool api.Tool) string {
	if idx := strings.Index(tool.Description, ". "); idx > 0 {
		return tool.Description[:idx+1]
	}
	return tool.Description
}
