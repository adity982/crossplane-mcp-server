// Package mcp adapts this server's tools to the Model Context Protocol.
//
// It is the only package that knows about the MCP SDK: tools themselves deal
// in api.Params and api.Result, which keeps them testable without a protocol
// round trip.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/version"
)

// Config configures the MCP server.
type Config struct {
	// Provider hands out a client per cluster.
	Provider *crossplane.Provider
	// Toolsets are the tool groups to expose.
	Toolsets []api.Toolset
	// AllowWrite exposes the tools that change the control plane. It is off
	// unless the operator asked for it: a server nobody granted write access
	// to should not be able to create or update anything, and a tool the
	// client never sees cannot be called.
	AllowWrite bool
	// Logger receives operational logs. Never write logs to stdout when the
	// stdio transport is in use: stdout carries the protocol.
	Logger *slog.Logger
	// ToolTimeout bounds how long a single tool call may run. Zero disables
	// the bound.
	ToolTimeout time.Duration
}

// Server owns the MCP server and the tools registered on it.
type Server struct {
	sdk          *sdk.Server
	config       Config
	tools        []api.Tool
	resourcesMu  sync.Mutex
	xrdResources map[string]struct{}
}

// NewServer builds an MCP server exposing the configured toolsets.
func NewServer(config Config) (*Server, error) {
	if config.Provider == nil {
		return nil, fmt.Errorf("a crossplane provider is required")
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}

	impl := &sdk.Implementation{
		Name:       version.BinaryName,
		Title:      "Crossplane",
		Version:    version.Version,
		WebsiteURL: "https://github.com/ravibagri5/crossplane-mcp-server",
		Description: "Access to a Crossplane control plane: managed resources, composite resources, " +
			"claims, packages, compositions and their health. Read-only unless writes are enabled.",
	}

	s := &Server{
		sdk:          sdk.NewServer(impl, &sdk.ServerOptions{Instructions: instructions(config.AllowWrite)}),
		config:       config,
		xrdResources: map[string]struct{}{},
	}

	seen := map[string]string{}
	withheld := 0
	for _, toolset := range config.Toolsets {
		for _, tool := range toolset.Tools() {
			if owner, duplicate := seen[tool.Name]; duplicate {
				return nil, fmt.Errorf("tool %q is defined by both the %q and %q toolsets",
					tool.Name, owner, toolset.Name())
			}
			seen[tool.Name] = toolset.Name()
			// A withheld tool is not declared at all, so the model never learns
			// it exists and cannot be talked into trying it.
			if tool.Mutates() && !config.AllowWrite {
				withheld++
				continue
			}
			s.register(tool)
			s.tools = append(s.tools, tool)
		}
	}
	config.Logger.Info("registered tools",
		"tools", len(s.tools), "toolsets", len(config.Toolsets),
		"writes", config.AllowWrite, "withheld", withheld)
	s.registerPrompts()
	s.registerXRDResourceTemplate()
	return s, nil
}

// Tools returns the registered tools, in registration order.
func (s *Server) Tools() []api.Tool { return s.tools }

// Declaration renders the MCP declaration for a tool, including the cluster
// selector every tool shares. Callers that need the wire-level tool shape
// without running a server, such as bundle packaging, use this.
func Declaration(tool api.Tool) *sdk.Tool {
	return &sdk.Tool{
		Name:        tool.Name,
		Title:       tool.Title,
		Description: tool.Description,
		InputSchema: withClusterArgument(tool.InputSchema),
		Annotations: tool.Annotations(),
	}
}

// register wires a single tool onto the SDK server.
func (s *Server) register(tool api.Tool) {
	declaration := Declaration(tool)

	s.sdk.AddTool(declaration, func(ctx context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return s.call(ctx, tool, request)
	})
}

// withClusterArgument adds the cluster selector to a tool's schema.
//
// Doing it here rather than in each tool keeps the argument identical across
// every tool, and means a tool author cannot forget it.
func withClusterArgument(schema *jsonschema.Schema) *jsonschema.Schema {
	if schema == nil {
		schema = &jsonschema.Schema{Type: "object"}
	}
	if schema.Properties == nil {
		schema.Properties = map[string]*jsonschema.Schema{}
	}
	schema.Properties[api.ClusterArg] = api.ClusterProp
	return schema
}

func (s *Server) call(ctx context.Context, tool api.Tool, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	arguments := map[string]any{}
	if raw := request.Params.Arguments; len(raw) > 0 {
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return errorResult(fmt.Errorf("cannot parse arguments: %w", err)), nil
		}
	}

	result, err := s.invoke(ctx, tool, arguments)
	if err != nil {
		return nil, err
	}
	if result.Err != nil {
		return errorResult(result.Err), nil
	}
	return &sdk.CallToolResult{
		Content:           []sdk.Content{&sdk.TextContent{Text: result.Text}},
		StructuredContent: result.Structured,
	}, nil
}

// Call runs a tool by name outside the protocol, which is how the command line
// offers a single tool call without an MCP client in the way.
func (s *Server) Call(ctx context.Context, name string, arguments map[string]any) (*api.Result, error) {
	for _, tool := range s.tools {
		if tool.Name == name {
			return s.invoke(ctx, tool, arguments)
		}
	}
	return nil, fmt.Errorf("no tool named %q, run 'tools' to see the list", name)
}

// invoke runs one tool against the cluster its arguments select.
func (s *Server) invoke(ctx context.Context, tool api.Tool, arguments map[string]any) (*api.Result, error) {
	started := time.Now()

	// Withheld tools are never registered, so this only fires if something
	// reaches a handler another way. Writes are worth checking twice.
	if tool.Mutates() && !s.config.AllowWrite {
		return api.Errorf("tool %q changes the control plane and this server is running read-only: "+
			"restart it with --read-only=false to allow writes", tool.Name), nil
	}

	if s.config.ToolTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.ToolTimeout)
		defer cancel()
	}

	// The cluster selector is handled here so that no tool has to think about
	// it, and is removed from the arguments the handler sees.
	cluster, _ := arguments[api.ClusterArg].(string)
	delete(arguments, api.ClusterArg)

	client, err := s.config.Provider.Client(cluster)
	if err != nil {
		return api.Error(err), nil
	}

	result, err := tool.Handler(api.Params{
		Context:  ctx,
		Client:   client,
		Provider: s.config.Provider,
		Args:     api.NewArgs(arguments),
	})
	duration := time.Since(started)

	// A handler returning an error means the tool itself is broken. Surface
	// it as a protocol error so the failure is not silently attributed to the
	// control plane.
	if err != nil {
		s.config.Logger.Error("tool failed",
			"tool", tool.Name, "cluster", client.Target(), "duration", duration, "error", err)
		return nil, err
	}
	if result.Err != nil {
		s.config.Logger.Warn("tool reported an error",
			"tool", tool.Name, "cluster", client.Target(), "duration", duration, "error", result.Err)
		return result, nil
	}

	s.config.Logger.Debug("tool completed",
		"tool", tool.Name, "cluster", client.Target(), "duration", duration)
	return result, nil
}

// ServeStdio runs the server over stdin and stdout, which is how editors and
// desktop MCP clients launch it.
func (s *Server) ServeStdio(ctx context.Context) error {
	s.config.Logger.Info("serving MCP over stdio")
	if err := s.refreshXRDResources(ctx); err != nil {
		s.config.Logger.Warn("cannot refresh XRD resources", "error", err)
	}
	go s.reconcileXRDResources(ctx)
	err := s.sdk.Run(ctx, &sdk.StdioTransport{})
	// A client closing its end of the pipe, or the context being cancelled by
	// a signal, is how a stdio session ends. Neither is a failure.
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		s.config.Logger.Info("stdio session closed")
		return nil
	}
	return fmt.Errorf("stdio transport failed: %w", err)
}

// HTTPHandler returns a handler serving the streamable HTTP transport, for
// deployments where the server runs in the cluster it inspects.
func (s *Server) HTTPHandler() http.Handler {
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s.sdk }, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.refreshXRDResources(r.Context()); err != nil {
			s.config.Logger.Warn("cannot refresh XRD resources", "error", err)
		}
		handler.ServeHTTP(w, r)
	})
}

func errorResult(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}},
	}
}
