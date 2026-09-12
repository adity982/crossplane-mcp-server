package crossplane

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestChildReferences(t *testing.T) {
	cases := map[string]struct {
		object *unstructured.Unstructured
		want   []objectReference
	}{
		"ClaimPointsAtOneComposite": {
			object: newObject(map[string]any{
				"metadata": map[string]any{"namespace": "team-a"},
				"spec": map[string]any{"resourceRef": map[string]any{
					"apiVersion": "example.org/v1", "kind": "XDatabase", "name": "db-abcde",
				}},
			}),
			want: []objectReference{
				{APIVersion: "example.org/v1", Kind: "XDatabase", Name: "db-abcde", Namespace: "team-a"},
			},
		},
		"CompositePointsAtComposedResources": {
			object: newObject(map[string]any{
				"spec": map[string]any{"resourceRefs": []any{
					map[string]any{"apiVersion": "rds.aws.upbound.io/v1beta1", "kind": "Instance", "name": "db-1"},
					map[string]any{"apiVersion": "ec2.aws.upbound.io/v1beta1", "kind": "SecurityGroup", "name": "sg-1"},
				}},
			}),
			want: []objectReference{
				{APIVersion: "rds.aws.upbound.io/v1beta1", Kind: "Instance", Name: "db-1"},
				{APIVersion: "ec2.aws.upbound.io/v1beta1", Kind: "SecurityGroup", Name: "sg-1"},
			},
		},
		"CrossplaneV2KeepsRefsUnderSpecCrossplane": {
			object: newObject(map[string]any{
				"metadata": map[string]any{"namespace": "harmony-staging"},
				"spec": map[string]any{"crossplane": map[string]any{"resourceRefs": []any{
					map[string]any{
						"apiVersion": "dbforpostgresql.azure.upbound.io/v1beta1",
						"kind":       "FlexibleServer",
						"name":       "postgresql-psql-v1-staging-1",
					},
				}}},
			}),
			want: []objectReference{{
				APIVersion: "dbforpostgresql.azure.upbound.io/v1beta1",
				Kind:       "FlexibleServer",
				Name:       "postgresql-psql-v1-staging-1",
				Namespace:  "harmony-staging",
			}},
		},
		"IncompleteReferencesAreIgnored": {
			object: newObject(map[string]any{
				"spec": map[string]any{"resourceRefs": []any{
					map[string]any{"apiVersion": "example.org/v1", "kind": "Thing"},
				}},
			}),
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := childReferences(tc.object)
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestChildReferencesDeduplicates(t *testing.T) {
	// Crossplane v2 publishes resourceRefs under status while v1 publishes
	// them under spec. A control plane mid-upgrade can have both.
	ref := map[string]any{"apiVersion": "example.org/v1", "kind": "XDatabase", "name": "db"}
	obj := newObject(map[string]any{
		"spec":   map[string]any{"resourceRefs": []any{ref}},
		"status": map[string]any{"resourceRefs": []any{ref}},
	})

	got := childReferences(obj)

	require.Len(t, got, 1)
	assert.Equal(t, "XDatabase", got[0].Kind)
}

func TestCompositionName(t *testing.T) {
	v1 := newObject(map[string]any{
		"spec": map[string]any{"compositionRef": map[string]any{"name": "legacy"}},
	})
	v2 := newObject(map[string]any{
		"spec": map[string]any{"crossplane": map[string]any{"compositionRef": map[string]any{"name": "modern"}}},
	})

	assert.Equal(t, "legacy", CompositionName(v1))
	assert.Equal(t, "modern", CompositionName(v2))
	assert.Empty(t, CompositionName(newObject(map[string]any{})))
}

func TestRenderTree(t *testing.T) {
	tree := TreeNode{
		Kind: "PostgreSQLInstance", Name: "app-db", Ready: "False", Synced: "True",
		Children: []TreeNode{
			{
				Kind: "XPostgreSQLInstance", Name: "app-db-x7k2p", Ready: "False", Synced: "True",
				Children: []TreeNode{
					{Kind: "Instance", Name: "app-db-rds", Ready: "False", Synced: "True", Message: "creating"},
					{Kind: "SecurityGroup", Name: "app-db-sg", Ready: "True", Synced: "True"},
				},
			},
		},
	}

	got := RenderTree(tree)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	require.Len(t, lines, 4)
	assert.Equal(t, "PostgreSQLInstance/app-db  READY=False SYNCED=True", lines[0])
	assert.Equal(t, "└─ XPostgreSQLInstance/app-db-x7k2p  READY=False SYNCED=True", lines[1])
	assert.Equal(t, "   ├─ Instance/app-db-rds  READY=False SYNCED=True  creating", lines[2])
	assert.Equal(t, "   └─ SecurityGroup/app-db-sg  READY=True SYNCED=True", lines[3])
}
