package toolsets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets"

	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/compositions"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/config"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/diagnostics"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/packages"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/provisioning"
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/resources"
)

// all is the registry in the order Names returns it, which several cases
// below assert against.
var all = []string{"compositions", "config", "diagnostics", "packages", "provisioning", "resources"}

func TestAllToolsetsAreRegistered(t *testing.T) {
	assert.Equal(t, all, toolsets.Names())
}

func TestSelect(t *testing.T) {
	cases := map[string]struct {
		names   []string
		want    []string
		wantErr string
	}{
		"EmptySelectsEverything": {
			names: nil,
			want:  all,
		},
		"AllSelectsEverything": {
			names: []string{"all"},
			want:  all,
		},
		"SelectionKeepsRegistryOrder": {
			names: []string{"resources", "diagnostics"},
			want:  []string{"diagnostics", "resources"},
		},
		"CaseAndWhitespaceAreForgiven": {
			names: []string{" Packages "},
			want:  []string{"packages"},
		},
		"UnknownNameIsReported": {
			names:   []string{"resources", "bananas"},
			wantErr: `unknown toolset(s) bananas`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			selected, err := toolsets.Select(tc.names)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)

			got := make([]string, 0, len(selected))
			for _, ts := range selected {
				got = append(got, ts.Name())
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEveryToolIsWellFormed(t *testing.T) {
	seen := map[string]bool{}

	for _, toolset := range toolsets.All() {
		assert.NotEmpty(t, toolset.Description(), "toolset %q needs a description", toolset.Name())

		tools := toolset.Tools()
		require.NotEmpty(t, tools, "toolset %q exposes no tools", toolset.Name())

		for _, tool := range tools {
			t.Run(tool.Name, func(t *testing.T) {
				assert.False(t, seen[tool.Name], "tool name %q is used twice", tool.Name)
				seen[tool.Name] = true

				assert.NotEmpty(t, tool.Title, "a title is what clients show in their UI")
				assert.NotEmpty(t, tool.Description, "the description is how the model decides to call the tool")
				assert.NotNil(t, tool.InputSchema, "every tool needs an input schema, even an empty one")
				assert.Equal(t, "object", tool.InputSchema.Type, "MCP input schemas must be objects")
				assert.NotNil(t, tool.Handler)

				// Mutating tools live in one toolset and nowhere else. A tool
				// that changes the control plane from inside a toolset people
				// think of as read-only would be a nasty surprise, and the
				// docs promise the provisioning toolset is all writes.
				assert.Equal(t, toolset.Name() == "provisioning", tool.Mutates(),
					"a tool writes if and only if it is in the provisioning toolset")
				assert.Equal(t, !tool.Mutates(), tool.Annotations().ReadOnlyHint,
					"the annotation has to agree with the flag, it is what clients gate on")
				assert.False(t, *tool.Annotations().DestructiveHint,
					"no tool in this server removes anything: writes create or update")
			})
		}
	}
}
