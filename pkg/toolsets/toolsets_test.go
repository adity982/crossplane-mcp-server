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
	_ "github.com/ravibagri5/crossplane-mcp-server/pkg/toolsets/resources"
)

func TestAllToolsetsAreRegistered(t *testing.T) {
	assert.Equal(t, []string{"compositions", "config", "diagnostics", "packages", "resources"}, toolsets.Names())
}

func TestSelect(t *testing.T) {
	cases := map[string]struct {
		names   []string
		want    []string
		wantErr string
	}{
		"EmptySelectsEverything": {
			names: nil,
			want:  []string{"compositions", "config", "diagnostics", "packages", "resources"},
		},
		"AllSelectsEverything": {
			names: []string{"all"},
			want:  []string{"compositions", "config", "diagnostics", "packages", "resources"},
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

				// Everything this server exposes is read only. If that ever
				// changes it should be a deliberate decision, not an accident.
				assert.False(t, tool.Destructive, "tool %q is marked destructive", tool.Name)
				assert.True(t, tool.Annotations().ReadOnlyHint)
			})
		}
	}
}
