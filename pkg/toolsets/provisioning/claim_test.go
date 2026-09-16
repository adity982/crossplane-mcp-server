package provisioning

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

// schemaWith builds an XRD schema whose spec declares the given properties,
// in the shape XRDSchemaFor produces.
func schemaWith(properties map[string]any) *crossplane.XRDSchema {
	return &crossplane.XRDSchema{Spec: map[string]any{"properties": properties}}
}

func object(properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": properties}
}

func TestBuildSpec(t *testing.T) {
	values := []field{
		{names: []string{"size", "instanceSize"}, value: "small"},
		{names: []string{"storageGB", "storage"}, value: 20},
	}

	cases := map[string]struct {
		schema      *crossplane.XRDSchema
		request     request
		wantSpec    map[string]any
		wantIgnored []string
	}{
		"ParametersIsPreferred": {
			schema: schemaWith(map[string]any{
				"parameters": object(map[string]any{
					"size":      map[string]any{"type": "string"},
					"storageGB": map[string]any{"type": "integer"},
				}),
			}),
			request:  request{values: values},
			wantSpec: map[string]any{"parameters": map[string]any{"size": "small", "storageGB": 20}},
		},
		"TopLevelFieldsAreUsedWhenThereIsNoParametersBlock": {
			schema: schemaWith(map[string]any{
				"size":    map[string]any{"type": "string"},
				"storage": map[string]any{"type": "integer"},
			}),
			request:  request{values: values},
			wantSpec: map[string]any{"size": "small", "storage": 20},
		},
		"AlternativeNamesAreTried": {
			schema: schemaWith(map[string]any{
				"parameters": object(map[string]any{
					"instanceSize": map[string]any{"type": "string"},
				}),
			}),
			request:     request{values: values},
			wantSpec:    map[string]any{"parameters": map[string]any{"instanceSize": "small"}},
			wantIgnored: []string{"storageGB"},
		},
		"FieldsTheAPIDoesNotDeclareAreReportedNotGuessed": {
			schema:      schemaWith(map[string]any{"parameters": object(map[string]any{})}),
			request:     request{values: values},
			wantSpec:    map[string]any{},
			wantIgnored: []string{"size", "storageGB"},
		},
		"ValuesAreCoercedToTheDeclaredType": {
			schema: schemaWith(map[string]any{
				"parameters": object(map[string]any{
					"storageGB": map[string]any{"type": "string"},
				}),
			}),
			request:     request{values: values},
			wantSpec:    map[string]any{"parameters": map[string]any{"storageGB": "20"}},
			wantIgnored: []string{"size"},
		},
		"ExplicitParametersAreSetEvenWhenUndeclared": {
			schema:  schemaWith(map[string]any{"parameters": object(map[string]any{})}),
			request: request{parameters: map[string]any{"region": "eu-west-1"}},
			// Nothing was requested beyond the parameter, so nothing is
			// ignored either.
			wantSpec: map[string]any{"parameters": map[string]any{"region": "eu-west-1"}},
		},
		"WithoutASchemaTheConventionalShapeIsUsed": {
			schema:   nil,
			request:  request{values: values},
			wantSpec: map[string]any{"parameters": map[string]any{"size": "small", "storageGB": 20}},
		},
		"OmittedValuesAreLeftToTheComposition": {
			schema: schemaWith(map[string]any{
				"parameters": object(map[string]any{"version": map[string]any{"type": "string"}}),
			}),
			request:  request{values: []field{{names: []string{"version"}, value: nil}}},
			wantSpec: map[string]any{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spec, ignored := buildSpec(tc.schema, tc.request)

			assert.Equal(t, tc.wantSpec, spec)
			assert.Equal(t, tc.wantIgnored, ignored)
		})
	}
}

func TestPickChoosesTheMostSpecificPlatformAPI(t *testing.T) {
	claim := func(kind string) crossplane.APIResource {
		return crossplane.APIResource{Group: "demo.crossplane.io", Version: "v1alpha1", Kind: kind}
	}

	cases := map[string]struct {
		candidates []crossplane.APIResource
		hints      []string
		want       string
		wantOthers int
	}{
		"EngineBeatsTheGenericKind": {
			candidates: []crossplane.APIResource{claim("Database"), claim("PostgreSQLInstance")},
			hints:      databaseHints(enginePostgres),
			want:       "PostgreSQLInstance",
			wantOthers: 1,
		},
		"GenericKindIsUsedWhenNothingMoreSpecificExists": {
			candidates: []crossplane.APIResource{claim("App"), claim("Database")},
			hints:      databaseHints(enginePostgres),
			want:       "Database",
			wantOthers: 0,
		},
		"AnExactMatchWins": {
			candidates: []crossplane.APIResource{claim("DatabaseInstance"), claim("Database")},
			hints:      databaseHints(engineMySQL),
			want:       "Database",
			wantOthers: 1,
		},
		"TheFirstCandidateWinsATie": {
			// Claims are listed before composites, so a control plane that
			// offers both gets the namespaced one.
			candidates: []crossplane.APIResource{claim("Database"), claim("Database")},
			hints:      databaseHints(enginePostgres),
			want:       "Database",
			wantOthers: 1,
		},
		"NothingMatches": {
			candidates: []crossplane.APIResource{claim("Bucket"), claim("Network")},
			hints:      databaseHints(enginePostgres),
			want:       "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			best, others := pick(tc.candidates, tc.hints)

			assert.Equal(t, tc.want, best.Kind)
			assert.Len(t, others, tc.wantOthers)
		})
	}
}

func TestParseRejectsManifestsThatCannotBeApplied(t *testing.T) {
	cases := map[string]struct {
		manifest  string
		namespace string
		wantErr   string
		wantCount int
	}{
		"SingleDocument": {
			manifest:  "apiVersion: demo.crossplane.io/v1alpha1\nkind: PostgreSQLInstance\nmetadata:\n  name: db\n",
			wantCount: 1,
		},
		"SeveralDocuments": {
			manifest: "apiVersion: v1\nkind: A\nmetadata:\n  name: one\n---\n" +
				"apiVersion: v1\nkind: B\nmetadata:\n  name: two\n",
			wantCount: 2,
		},
		"JSONIsAccepted": {
			manifest:  `{"apiVersion":"v1","kind":"A","metadata":{"name":"one"}}`,
			wantCount: 1,
		},
		"TheDefaultNamespaceFillsInTheGaps": {
			manifest:  "apiVersion: v1\nkind: A\nmetadata:\n  name: one\n",
			namespace: "team-a",
			wantCount: 1,
		},
		"MissingName": {
			manifest: "apiVersion: v1\nkind: A\n",
			wantErr:  "document 1 needs apiVersion, kind and metadata.name",
		},
		"NotAManifest": {
			manifest: "this is a sentence, not YAML: [",
			wantErr:  "cannot parse the manifest",
		},
		"Empty": {
			manifest: "# only a comment\n",
			wantErr:  "contains no documents",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			documents, err := parse(tc.manifest, tc.namespace)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Len(t, documents, tc.wantCount)
			if tc.namespace != "" {
				assert.Equal(t, tc.namespace, documents[0].GetNamespace())
			}
		})
	}
}
