package resources

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func compositeResourceTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_composite_resources_list",
			Title: "Composite resources: list",
			Description: "List composite resources (XRs) with their Ready and Synced conditions and the " +
				"Composition each one selected. Composite resources are the instances of the APIs your XRDs " +
				"define. Use this to answer how many composite resources exist or which ones are broken.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind": api.StringProp("Kind of composite resource to list, for example 'XPostgreSQLInstance'. " +
					"Omit to list every composite kind."),
				"group":         api.GroupProp,
				"namespace":     api.NamespaceProp,
				"labelSelector": api.LabelSelectorProp,
				"status": api.EnumProp("Filter by reconciliation status.",
					statusAny, statusReady, statusNotReady, statusNotSynced),
				"limit": api.LimitProp,
			}),
			Handler: compositeResourcesList,
		},
		{
			Name:  "crossplane_claims_list",
			Title: "Claims: list",
			Description: "List claims with their Ready and Synced conditions and the composite resource each " +
				"one is bound to. Claims are the namespaced, developer facing entry point to a platform API. " +
				"Crossplane v2 deprecates claims in favour of namespaced composite resources, so an empty " +
				"result on a v2 control plane is expected.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind": api.StringProp("Kind of claim to list, for example 'PostgreSQLInstance'. " +
					"Omit to list every claim kind."),
				"group":         api.GroupProp,
				"namespace":     api.NamespaceProp,
				"labelSelector": api.LabelSelectorProp,
				"status": api.EnumProp("Filter by reconciliation status.",
					statusAny, statusReady, statusNotReady, statusNotSynced),
				"limit": api.LimitProp,
			}),
			Handler: claimsList,
		},
	}
}

func compositeResourcesList(p api.Params) (*api.Result, error) {
	return listCategory(p, crossplane.CategoryComposite, "composite resources")
}

func claimsList(p api.Params) (*api.Result, error) {
	return listCategory(p, crossplane.CategoryClaim, "claims")
}

// listCategory implements the composite and claim list tools, which differ
// only in the category they search and the noun they report.
func listCategory(p api.Params, category, noun string) (*api.Result, error) {
	query := crossplane.Query{
		Category:      category,
		Kind:          p.Args.OptionalString("kind", ""),
		Group:         p.Args.OptionalString("group", ""),
		Namespace:     p.Args.OptionalString("namespace", ""),
		LabelSelector: p.Args.OptionalString("labelSelector", ""),
		Limit:         int64(p.Args.OptionalInt("limit", defaultLimit)),
	}
	status := p.Args.OptionalEnum("status", statusAny, statusAny, statusReady, statusNotReady, statusNotSynced)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	result, err := p.Client.Query(p, query)
	if err != nil {
		return api.Error(fmt.Errorf("cannot list %s: %w", noun, err)), nil
	}

	summaries := filterByStatus(crossplane.Summaries(result.Objects, false), status)

	// Composites and claims gain a column the generic renderer does not
	// know about: which Composition, or which composite, they resolved to.
	type entry struct {
		crossplane.Summary `json:",inline"`
		Composition        string `json:"composition,omitempty"`
	}
	entries := make([]entry, 0, len(summaries))
	byName := indexObjects(result.Objects)
	rows := make([][]string, 0, len(summaries))
	for _, s := range summaries {
		composition := ""
		if obj, ok := byName[objectKey(s.Kind, s.Namespace, s.Name)]; ok {
			composition = crossplane.CompositionName(obj)
		}
		entries = append(entries, entry{Summary: s, Composition: composition})
		rows = append(rows, []string{
			s.Kind, orDash(s.Namespace), s.Name, orDash(composition),
			s.Ready, s.Synced, s.Age, orDash(s.Message),
		})
	}

	var text strings.Builder
	if len(entries) == 0 {
		text.WriteString("No " + noun + " matched.")
	} else {
		fmt.Fprintf(&text, "%d %s.\n", len(entries), noun)
		text.WriteString(api.Table(
			[]string{"KIND", "NAMESPACE", "NAME", "COMPOSITION", "READY", "SYNCED", "AGE", "MESSAGE"}, rows))
	}
	appendWarnings(&text, result.Warnings)

	return api.Structured(text.String(), map[string]any{
		"count":     len(entries),
		"resources": entries,
		"warnings":  result.Warnings,
	}), nil
}
