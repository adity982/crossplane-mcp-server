package diagnostics

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func TestCollectDoomed(t *testing.T) {
	cases := map[string]struct {
		tree        crossplane.TreeNode
		wantNames   []string
		wantManaged []string
	}{
		"SingleManagedResource": {
			tree:        crossplane.TreeNode{Kind: "Bucket", Name: "data"},
			wantNames:   []string{"Bucket/data"},
			wantManaged: []string{"Bucket/data"},
		},
		"ClaimTakesTheWholeTree": {
			tree: crossplane.TreeNode{
				Kind: "PostgreSQLInstance", Name: "claim",
				Children: []crossplane.TreeNode{{
					Kind: "XPostgreSQL", Name: "xr",
					Children: []crossplane.TreeNode{
						{Kind: "Instance", Name: "db"},
						{Kind: "SecurityGroup", Name: "sg"},
					},
				}},
			},
			wantNames: []string{
				"PostgreSQLInstance/claim", "XPostgreSQL/xr", "Instance/db", "SecurityGroup/sg",
			},
			// Only the leaves hold external infrastructure.
			wantManaged: []string{"Instance/db", "SecurityGroup/sg"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := collectDoomed(tc.tree, nil)

			names := make([]string, 0, len(got))
			managed := make([]string, 0, len(got))
			for _, d := range got {
				names = append(names, d.Kind+"/"+d.Name)
				if d.Managed {
					managed = append(managed, d.Kind+"/"+d.Name)
				}
			}
			assert.Equal(t, tc.wantNames, names)
			assert.Equal(t, tc.wantManaged, managed)
		})
	}
}

func TestClassifyUsages(t *testing.T) {
	wouldDelete := []doomed{
		{Kind: "PostgreSQLInstance", Name: "claim"},
		{Kind: "Instance", Name: "db"},
	}

	cases := map[string]struct {
		usages       []crossplane.Usage
		wantBlocking []string
		wantDangling []string
	}{
		"ProtectingTheRootBlocks": {
			usages:       []crossplane.Usage{{Name: "u1", OfKind: "PostgreSQLInstance", OfName: "claim"}},
			wantBlocking: []string{"u1"},
		},
		"ProtectingSomethingDeeperAlsoBlocks": {
			// The delete stalls at the composed resource instead of the root.
			usages:       []crossplane.Usage{{Name: "u2", OfKind: "Instance", OfName: "db"}},
			wantBlocking: []string{"u2"},
		},
		"DependingOnSomethingDoomedIsLeftDangling": {
			usages:       []crossplane.Usage{{Name: "u3", OfKind: "Bucket", OfName: "other", ByKind: "Instance", ByName: "db"}},
			wantDangling: []string{"u3"},
		},
		"UnrelatedUsageIsIgnored": {
			usages: []crossplane.Usage{{Name: "u4", OfKind: "Bucket", OfName: "other", ByKind: "Queue", ByName: "jobs"}},
		},
		"KindMatchIsCaseInsensitive": {
			usages:       []crossplane.Usage{{Name: "u5", OfKind: "instance", OfName: "db"}},
			wantBlocking: []string{"u5"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			blocking, dangling := classifyUsages(tc.usages, wouldDelete, "PostgreSQLInstance", "claim")

			assert.Equal(t, tc.wantBlocking, usageNames(blocking))
			assert.Equal(t, tc.wantDangling, usageNames(dangling))
		})
	}
}

func TestImpactVerdict(t *testing.T) {
	cases := map[string]struct {
		report impactReport
		want   string
	}{
		"BlockedWins": {
			report: impactReport{
				Blocked:       true,
				BlockedBy:     []crossplane.Usage{{Name: "u1"}},
				WouldDelete:   []doomed{{}, {}},
				ExternalCount: 2,
			},
			want: "would be blocked",
		},
		"CallsOutRealInfrastructure": {
			report: impactReport{WouldDelete: []doomed{{}, {}}, ExternalCount: 2},
			want:   "real infrastructure",
		},
		"TreeWithoutExternalNames": {
			report: impactReport{WouldDelete: []doomed{{}, {}}},
			want:   "composition tree",
		},
		"LoneResource": {
			report: impactReport{WouldDelete: []doomed{{}}},
			want:   "only itself",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, impactVerdict(tc.report), tc.want)
		})
	}
}

func usageNames(usages []crossplane.Usage) []string {
	if len(usages) == 0 {
		return nil
	}
	names := make([]string, 0, len(usages))
	for _, u := range usages {
		names = append(names, u.Name)
	}
	return names
}
