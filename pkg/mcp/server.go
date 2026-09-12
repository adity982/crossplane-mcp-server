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
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/version"
)

// Config configures the MCP server.
type Config struct {
	// Client talks to the Crossplane control plane.
	Client *crossplane.Client
	// Toolsets are the tool groups to expose.
	Toolsets []api.Toolset
	// Logger receives operational logs. Never write logs to stdout when the
	// stdio transport is in use: stdout carries the protocol.
	Logger *slog.Logger
	// ToolTimeout bounds how long a single tool call may run. Zero disables
	// the bound.
	ToolTimeout time.Duration
}

// Server owns the MCP server and the tools registered on it.
type Server struct {
	sdk    *sdk.Server
	config Config
	tools  []api.Tool
}

// NewServer builds an MCP server exposing the configured toolsets.
func NewServer(config Config) (*Server, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("a crossplane client is required")
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}

	impl := &sdk.Implementation{
		Name:       version.BinaryName,
		Title:      "Crossplane",
		Version:    version.Version,
		WebsiteURL: "https://github.com/ravibagri5/crossplane-mcp-server",
		Description: "Read-only access to a Crossplane control plane: managed resources, composite resources, " +
			"claims, packages, compositions and their health.",
	}

	s := &Server{
		sdk:    sdk.NewServer(impl, &sdk.ServerOptions{Instructions: instructions}),
		config: config,
	}

	seen := map[string]string{}
	for _, toolset := range config.Toolsets {
		for _, tool := range toolset.Tools() {
			if owner, duplicate := seen[tool.Name]; duplicate {
				return nil, fmt.Errorf("tool %q is defined by both the %q and %q toolsets",
					tool.Name, owner, toolset.Name())
			}
			seen[tool.Name] = toolset.Name()
			s.register(tool)
			s.tools = append(s.tools, tool)
		}
	}
	config.Logger.Info("registered tools", "tools", len(s.tools), "toolsets", len(config.Toolsets))
	return s, nil
}

// Tools returns the registered tools, in registration order.
func (s *Server) Tools() []api.Tool { return s.tools }

// register wires a single tool onto the SDK server.
func (s *Server) register(tool api.Tool) {
	declaration := &sdk.Tool{
		Name:        tool.Name,
		Title:       tool.Title,
		Description: tool.Description,
		InputSchema: tool.InputSchema,
		Annotations: tool.Annotations(),
	}

	s.sdk.AddTool(declaration, func(ctx context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return s.call(ctx, tool, request)
	})
}

func (s *Server) call(ctx context.Context, tool api.Tool, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	started := time.Now()

	arguments := map[string]any{}
	if raw := request.Params.Arguments; len(raw) > 0 {
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return errorResult(fmt.Errorf("cannot parse arguments: %w", err)), nil
		}
	}

	if s.config.ToolTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.ToolTimeout)
		defer cancel()
	}

	result, err := tool.Handler(api.Params{
		Context: ctx,
		Client:  s.config.Client,
		Args:    api.NewArgs(arguments),
	})
	duration := time.Since(started)

	// A handler returning an error means the tool itself is broken. Surface
	// it as a protocol error so the failure is not silently attributed to the
	// control plane.
	if err != nil {
		s.config.Logger.Error("tool failed", "tool", tool.Name, "duration", duration, "error", err)
		return nil, err
	}
	if result.Err != nil {
		s.config.Logger.Warn("tool reported an error", "tool", tool.Name, "duration", duration, "error", result.Err)
		return errorResult(result.Err), nil
	}

	s.config.Logger.Debug("tool completed", "tool", tool.Name, "duration", duration)
	return &sdk.CallToolResult{
		Content:           []sdk.Content{&sdk.TextContent{Text: result.Text}},
		StructuredContent: result.Structured,
	}, nil
}

// ServeStdio runs the server over stdin and stdout, which is how editors and
// desktop MCP clients launch it.
func (s *Server) ServeStdio(ctx context.Context) error {
	s.config.Logger.Info("serving MCP over stdio")
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
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s.sdk }, nil)
}

func errorResult(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}},
	}
}
