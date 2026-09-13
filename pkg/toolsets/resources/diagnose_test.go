package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func TestDeepestUnready(t *testing.T) {
	cases := map[string]struct {
		tree crossplane.TreeNode
		want []string
	}{
		"EverythingReady": {
			tree: crossplane.TreeNode{
				Kind: "XPostgreSQL", Name: "root", Ready: "True",
				Children: []crossplane.TreeNode{{Kind: "RDSInstance", Name: "db", Ready: "True"}},
			},
		},
		"LeafIsTheCause": {
			tree: crossplane.TreeNode{
				Kind: "PostgreSQLInstance", Name: "claim", Ready: "False",
				Children: []crossplane.TreeNode{{
					Kind: "XPostgreSQL", Name: "xr", Ready: "False",
					Children: []crossplane.TreeNode{{Kind: "RDSInstance", Name: "db", Ready: "False"}},
				}},
			},
			want: []string{"RDSInstance/db"},
		},
		"RootItselfWhenNoChildren": {
			tree: crossplane.TreeNode{Kind: "Bucket", Name: "solo", Ready: "False"},
			want: []string{"Bucket/solo"},
		},
		"HealthyBranchIgnored": {
			tree: crossplane.TreeNode{
				Kind: "XApp", Name: "xr", Ready: "False",
				Children: []crossplane.TreeNode{
					{Kind: "Bucket", Name: "good", Ready: "True"},
					{Kind: "RDSInstance", Name: "bad", Ready: "False"},
				},
			},
			want: []string{"RDSInstance/bad"},
		},
		"RootReportedWhenChildrenAreHealthy": {
			// The composite failed on its own account, not because of a child.
			tree: crossplane.TreeNode{
				Kind: "XApp", Name: "xr", Ready: "False",
				Children: []crossplane.TreeNode{{Kind: "Bucket", Name: "good", Ready: "True"}},
			},
			want: []string{"XApp/xr"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := deepestUnready(tc.tree)

			var names []string
			for _, node := range got {
				names = append(names, node.Kind+"/"+node.Name)
			}
			assert.Equal(t, tc.want, names)
		})
	}
}

func TestDiffFields(t *testing.T) {
	cases := map[string]struct {
		desired  map[string]any
		observed map[string]any
		want     []fieldDrift
	}{
		"Identical": {
			desired:  map[string]any{"region": "eu-west-1"},
			observed: map[string]any{"region": "eu-west-1"},
		},
		"ScalarChanged": {
			desired:  map[string]any{"instanceClass": "db.t3.micro"},
			observed: map[string]any{"instanceClass": "db.t3.large"},
			want:     []fieldDrift{{Path: "instanceClass", Declared: "db.t3.micro", Observed: "db.t3.large"}},
		},
		"UnreportedFieldIsNotDrift": {
			// Write-only fields such as credentials never come back.
			desired:  map[string]any{"masterPassword": "secret"},
			observed: map[string]any{},
		},
		"NestedPathsAreQualified": {
			desired:  map[string]any{"tags": map[string]any{"env": "prod"}},
			observed: map[string]any{"tags": map[string]any{"env": "dev"}},
			want:     []fieldDrift{{Path: "tags.env", Declared: "prod", Observed: "dev"}},
		},
		"ListsCompareWhole": {
			desired:  map[string]any{"zones": []any{"a", "b"}},
			observed: map[string]any{"zones": []any{"a"}},
			want:     []fieldDrift{{Path: "zones", Declared: "[a, b]", Observed: "[a]"}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, diffFields("", tc.desired, tc.observed))
		})
	}
}

func TestInspectDriftCorrecting(t *testing.T) {
	cases := map[string]struct {
		obj            map[string]any
		annotations    map[string]string
		wantCorrecting bool
		wantReason     string
	}{
		"HealthyAndSynced": {
			obj:            managedResource("True", nil),
			wantCorrecting: true,
		},
		"Paused": {
			obj:            managedResource("True", nil),
			annotations:    map[string]string{pausedAnnotation: "true"},
			wantCorrecting: false,
			wantReason:     "paused",
		},
		"ObserveOnly": {
			obj:            managedResource("True", []any{"Observe"}),
			wantCorrecting: false,
			wantReason:     "never corrected",
		},
		"NotSynced": {
			obj:            managedResource("False", nil),
			wantCorrecting: false,
			wantReason:     "cannot apply the spec",
		},
		"ObserveWithCreateStillWrites": {
			obj:            managedResource("True", []any{"Observe", "Create"}),
			wantCorrecting: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: tc.obj}
			if tc.annotations != nil {
				obj.SetAnnotations(tc.annotations)
			}

			got := inspectDrift(obj)

			assert.Equal(t, tc.wantCorrecting, got.Correcting)
			if tc.wantReason != "" {
				assert.Contains(t, got.Reason, tc.wantReason)
			}
		})
	}
}

func TestWritesToProvider(t *testing.T) {
	cases := map[string][]string{
		"DefaultIsFullManagement": nil,
		"Wildcard":                {"*"},
		"CreateOnly":              {"Create"},
	}
	for name, policies := range cases {
		t.Run(name, func(t *testing.T) {
			assert.True(t, writesToProvider(policies))
		})
	}

	readOnly := map[string][]string{
		"ObserveOnly":    {"Observe"},
		"ObserveAndFail": {"Observe", "Delete"},
	}
	for name, policies := range readOnly {
		t.Run(name, func(t *testing.T) {
			assert.False(t, writesToProvider(policies))
		})
	}
}

// managedResource builds the minimum object inspectDrift reads.
func managedResource(synced string, policies []any) map[string]any {
	spec := map[string]any{
		"forProvider": map[string]any{"region": "eu-west-1"},
	}
	if policies != nil {
		spec["managementPolicies"] = policies
	}
	return map[string]any{
		"apiVersion": "rds.aws.upbound.io/v1beta1",
		"kind":       "Instance",
		"metadata":   map[string]any{"name": "db"},
		"spec":       spec,
		"status": map[string]any{
			"atProvider": map[string]any{"region": "eu-west-1"},
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
				map[string]any{"type": "Synced", "status": synced, "reason": "ReconcileError", "message": "boom"},
			},
		},
	}
}
