// Package api defines the contract between the MCP transport layer and the
// tools this server exposes.
//
// Tools never import the MCP SDK directly. They take an api.Params, return an
// api.Result, and stay ordinary Go functions that are trivial to unit test.
package api

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/crossplane"
)

// Params is everything a tool handler is given.
type Params struct {
	context.Context

	// Client talks to the Crossplane control plane.
	Client *crossplane.Client
	// Args holds the arguments the model passed.
	Args *Args
}

// Handler implements a single tool.
//
// A handler returns an error only for problems the model cannot act on, such
// as a programming mistake. Everything an operator would consider a normal
// failure — a missing resource, an unreachable cluster — belongs in the
// Result so the model can read it and try something else.
type Handler func(p Params) (*Result, error)

// Tool bundles a tool's MCP declaration with its implementation.
type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema *jsonschema.Schema
	// Destructive marks tools that mutate the control plane. Every tool in
	// this server is currently read only, but the flag keeps the annotations
	// honest as the tool surface grows.
	Destructive bool
	Handler     Handler
}

// Annotations renders the MCP tool annotations for the tool.
func (t Tool) Annotations() *mcp.ToolAnnotations {
	readOnly := !t.Destructive
	return &mcp.ToolAnnotations{
		Title:           t.Title,
		ReadOnlyHint:    readOnly,
		DestructiveHint: &t.Destructive,
		IdempotentHint:  readOnly,
		OpenWorldHint:   boolPtr(true),
	}
}

// Toolset is a named group of related tools. Users enable and disable them by
// name via --toolsets, which keeps the tool list short enough for models with
// small context windows.
type Toolset interface {
	// Name is the identifier used on the command line.
	Name() string
	// Description is one line of prose shown in help output and docs.
	Description() string
	// Tools returns the tools in the set.
	Tools() []Tool
}

func boolPtr(b bool) *bool { return &b }
