package resources

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/api"
	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/crossplane"
)

// defaultLimit caps how many objects a single list tool returns per kind. A
// control plane can hold tens of thousands of managed resources and dumping
// them all would blow past any model's context window.
const defaultLimit = 500

// statusFilter values accepted by the list tools.
const (
	statusAny       = "any"
	statusReady     = "ready"
	statusNotReady  = "not-ready"
	statusNotSynced = "not-synced"
)

func managedResourceTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_managed_resources_summary",
			Title: "Managed resources: summary",
			Description: "Count the managed resources in the control plane, broken down by kind, with how many " +
				"are Ready and Synced. Start here when asked how many managed resources exist, which providers " +
				"are doing the most work, or whether anything is out of sync. Returns counts, not individual " +
				"resources; use crossplane_managed_resources_list for the detail.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"group":     api.GroupProp,
				"namespace": api.NamespaceProp,
			}),
			Handler: managedResourcesSummary,
		},
		{
			Name:  "crossplane_managed_resources_list",
			Title: "Managed resources: list",
			Description: "List managed resources with their Ready and Synced conditions and the reason for any " +
				"failure. Narrow the result with 'kind' to look at one type of resource, or with 'status' to see " +
				"only the ones that need attention.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind": api.StringProp("Kind of managed resource to list, for example 'Bucket' or 'RDSInstance'. " +
					"Omit to list every managed resource kind."),
				"group":         api.GroupProp,
				"namespace":     api.NamespaceProp,
				"labelSelector": api.LabelSelectorProp,
				"status": api.EnumProp("Filter by reconciliation status. 'not-ready' and 'not-synced' are the "+
					"quickest way to find broken resources.", statusAny, statusReady, statusNotReady, statusNotSynced),
				"limit": api.LimitProp,
			}),
			Handler: managedResourcesList,
		},
	}
}

func managedResourcesSummary(p api.Params) (*api.Result, error) {
	group := p.Args.OptionalString("group", "")
	namespace := p.Args.OptionalString("namespace", "")
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	result, err := p.Client.Query(p, crossplane.Query{
		Category:  crossplane.CategoryManaged,
		Group:     group,
		Namespace: namespace,
	})
	if err != nil {
		return api.Error(fmt.Errorf("cannot summarise managed resources: %w", err)), nil
	}

	summaries := crossplane.Summaries(result.Objects, false)
	counts := crossplane.CountByKind(summaries)

	rows := make([][]string, 0, len(counts))
	var total, ready, synced int
	for _, c := range counts {
		rows = append(rows, []string{
			c.Kind,
			c.APIVersion,
			itoa(c.Total),
			fmt.Sprintf("%d/%d", c.Ready, c.Total),
			fmt.Sprintf("%d/%d", c.Synced, c.Total),
		})
		total += c.Total
		ready += c.Ready
		synced += c.Synced
	}

	payload := map[string]any{
		"totalManagedResources": total,
		"ready":                 ready,
		"notReady":              total - ready,
		"synced":                synced,
		"notSynced":             total - synced,
		"kindsInstalled":        len(result.Kinds),
		"kindsInUse":            len(counts),
		"byKind":                counts,
		"warnings":              result.Warnings,
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d managed resources across %d kinds (%d installed managed resource kinds).\n",
		total, len(counts), len(result.Kinds))
	fmt.Fprintf(&text, "Ready: %d/%d. Synced: %d/%d.", ready, total, synced, total)
	if total > 0 {
		api.Section(&text, "", api.Table([]string{"KIND", "APIVERSION", "TOTAL", "READY", "SYNCED"}, rows))
	}
	appendWarnings(&text, result.Warnings)

	return api.Structured(text.String(), payload), nil
}

func managedResourcesList(p api.Params) (*api.Result, error) {
	query := crossplane.Query{
		Category:      crossplane.CategoryManaged,
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
		return api.Error(fmt.Errorf("cannot list managed resources: %w", err)), nil
	}

	summaries := filterByStatus(crossplane.Summaries(result.Objects, false), status)
	text := renderSummaries("managed resources", summaries, result.Warnings)

	return api.Structured(text, map[string]any{
		"count":     len(summaries),
		"resources": summaries,
		"warnings":  result.Warnings,
	}), nil
}

// filterByStatus keeps only the summaries matching the requested status.
func filterByStatus(summaries []crossplane.Summary, status string) []crossplane.Summary {
	if status == statusAny {
		return summaries
	}
	kept := make([]crossplane.Summary, 0, len(summaries))
	for _, s := range summaries {
		switch status {
		case statusReady:
			if s.Ready == "True" {
				kept = append(kept, s)
			}
		case statusNotReady:
			if s.Ready != "True" {
				kept = append(kept, s)
			}
		case statusNotSynced:
			if s.Synced != "True" {
				kept = append(kept, s)
			}
		}
	}
	return kept
}

// renderSummaries is the shared table rendering used by every list tool, so
// that managed resources, composites and claims all look the same.
func renderSummaries(noun string, summaries []crossplane.Summary, warnings []string) string {
	var text strings.Builder
	if len(summaries) == 0 {
		text.WriteString("No " + noun + " matched.")
		appendWarnings(&text, warnings)
		return text.String()
	}

	namespaced := false
	for _, s := range summaries {
		if s.Namespace != "" {
			namespaced = true
			break
		}
	}

	headers := []string{"KIND", "NAME", "READY", "SYNCED", "AGE", "MESSAGE"}
	if namespaced {
		headers = []string{"KIND", "NAMESPACE", "NAME", "READY", "SYNCED", "AGE", "MESSAGE"}
	}

	rows := make([][]string, 0, len(summaries))
	for _, s := range summaries {
		row := []string{s.Kind}
		if namespaced {
			row = append(row, orDash(s.Namespace))
		}
		row = append(row, s.Name, s.Ready, s.Synced, s.Age, orDash(s.Message))
		rows = append(rows, row)
	}

	fmt.Fprintf(&text, "%d %s.\n", len(summaries), noun)
	text.WriteString(api.Table(headers, rows))
	appendWarnings(&text, warnings)
	return text.String()
}

func appendWarnings(text *strings.Builder, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	api.Section(text, fmt.Sprintf("Warnings (%d kinds could not be listed):", len(warnings)),
		"- "+strings.Join(warnings, "\n- "))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
