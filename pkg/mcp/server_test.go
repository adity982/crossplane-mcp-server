package mcp

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

// testToolset is a minimal toolset used to exercise the protocol plumbing
// without reaching for a cluster.
type testToolset struct {
	name  string
	tools []api.Tool
}

func (t *testToolset) Name() string        { return t.name }
func (t *testToolset) Description() string { return "tools used by the unit tests" }
func (t *testToolset) Tools() []api.Tool   { return t.tools }

func TestNewServerRejectsDuplicateToolNames(t *testing.T) {
	tool := api.Tool{
		Name:        "crossplane_example",
		Title:       "Example",
		Description: "example",
		InputSchema: api.Object(nil),
		Handler:     func(api.Params) (*api.Result, error) { return api.Text("ok"), nil },
	}

	_, err := NewServer(Config{
		Provider: newFakeProvider(),
		Toolsets: []api.Toolset{
			&testToolset{name: "first", tools: []api.Tool{tool}},
			&testToolset{name: "second", tools: []api.Tool{tool}},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool "crossplane_example" is defined by both the "first" and "second" toolsets`)
}

func TestNewServerRequiresAClient(t *testing.T) {
	_, err := NewServer(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crossplane provider is required")
}

func TestWriteToolsAreWithheldUnlessAskedFor(t *testing.T) {
	toolset := &testToolset{name: "test", tools: []api.Tool{
		{
			Name:        "crossplane_read",
			Title:       "Read",
			Description: "reads",
			InputSchema: api.Object(nil),
			Handler:     func(api.Params) (*api.Result, error) { return api.Text("ok"), nil },
		},
		{
			Name:        "crossplane_write",
			Title:       "Write",
			Description: "writes",
			InputSchema: api.Object(nil),
			Write:       true,
			Handler:     func(api.Params) (*api.Result, error) { return api.Text("written"), nil },
		},
	}}

	t.Run("ReadOnly", func(t *testing.T) {
		server, err := NewServer(Config{Provider: newFakeProvider(), Toolsets: []api.Toolset{toolset}})
		require.NoError(t, err)

		listed, err := connect(t, server).ListTools(context.Background(), nil)
		require.NoError(t, err)
		require.Len(t, listed.Tools, 1)
		assert.Equal(t, "crossplane_read", listed.Tools[0].Name)

		// The tool is not merely hidden, it cannot be reached at all.
		_, err = server.Call(context.Background(), "crossplane_write", nil)
		assert.ErrorContains(t, err, `no tool named "crossplane_write"`)
	})

	t.Run("WritesEnabled", func(t *testing.T) {
		server, err := NewServer(Config{
			Provider:   newFakeProvider(),
			Toolsets:   []api.Toolset{toolset},
			AllowWrite: true,
		})
		require.NoError(t, err)

		listed, err := connect(t, server).ListTools(context.Background(), nil)
		require.NoError(t, err)
		require.Len(t, listed.Tools, 2)
		for _, tool := range listed.Tools {
			assert.Equal(t, tool.Name == "crossplane_read", tool.Annotations.ReadOnlyHint)
		}

		result, err := server.Call(context.Background(), "crossplane_write", nil)
		require.NoError(t, err)
		assert.Equal(t, "written", result.Text)
	})
}

func TestServerExposesToolsOverTheProtocol(t *testing.T) {
	server, err := NewServer(Config{
		Provider: newFakeProvider(),
		Toolsets: []api.Toolset{&testToolset{name: "test", tools: []api.Tool{
			{
				Name:        "crossplane_echo",
				Title:       "Echo",
				Description: "echoes the message argument back",
				InputSchema: api.Object(map[string]*jsonschema.Schema{
					"message": api.StringProp("the message to echo"),
				}, "message"),
				Handler: func(p api.Params) (*api.Result, error) {
					message := p.Args.String("message")
					if argErr := p.Args.Err(); argErr != nil {
						return api.Error(argErr), nil
					}
					return api.Structured(message, map[string]any{"echoed": message}), nil
				},
			},
		}}},
	})
	require.NoError(t, err)

	session := connect(t, server)

	t.Run("ListTools", func(t *testing.T) {
		listed, err := session.ListTools(context.Background(), nil)
		require.NoError(t, err)
		require.Len(t, listed.Tools, 1)
		assert.Equal(t, "crossplane_echo", listed.Tools[0].Name)
		assert.True(t, listed.Tools[0].Annotations.ReadOnlyHint)
	})

	t.Run("CallTool", func(t *testing.T) {
		result, err := session.CallTool(context.Background(), &sdk.CallToolParams{
			Name:      "crossplane_echo",
			Arguments: map[string]any{"message": "hello"},
		})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		require.Len(t, result.Content, 1)
		assert.Equal(t, "hello", result.Content[0].(*sdk.TextContent).Text)
	})

	t.Run("ArgumentErrorsComeBackAsToolErrors", func(t *testing.T) {
		// A bad argument is something the model can fix, so it must arrive as
		// a tool error the model can read, not a protocol failure.
		result, err := session.CallTool(context.Background(), &sdk.CallToolParams{
			Name:      "crossplane_echo",
			Arguments: map[string]any{},
		})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content[0].(*sdk.TextContent).Text, `missing required argument "message"`)
	})
}

// connect wires an in-memory client to the server and returns the client
// session, cleaning both up when the test finishes.
func connect(t *testing.T, server *Server) *sdk.ClientSession {
	t.Helper()

	serverTransport, clientTransport := sdk.NewInMemoryTransports()

	serverSession, err := server.sdk.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Wait()
	})
	return clientSession
}

func newFakeProvider() *crossplane.Provider {
	core := k8sfake.NewSimpleClientset()
	client := crossplane.NewForClients(nil, core.Discovery(), core, "default")
	return crossplane.NewStaticProvider("test-cluster", client)
}
