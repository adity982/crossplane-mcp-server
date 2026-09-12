package diagnostics

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
)

func protectionTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_deleting_resources",
			Title: "Control plane: stuck deletions",
			Description: "Find Crossplane resources that were asked to delete but have not gone away, and " +
				"explain what is holding each one up: a Usage protecting it, composed resources still being " +
				"removed, or a provider that has not confirmed the external resource is gone. Use this " +
				"whenever a delete appears to hang, because kubectl reports success and then nothing happens, " +
				"which makes this failure hard to spot any other way.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"namespace": api.NamespaceProp,
				"includeRecent": api.BoolProp("Include deletions started in the last 30 seconds, which are " +
					"probably still in progress rather than stuck. Defaults to false."),
			}),
			Handler: deletingResources,
		},
		{
			Name:  "crossplane_usages_list",
			Title: "Usages: list",
			Description: "List the Usage and ClusterUsage objects, which tell Crossplane to refuse to delete " +
				"one resource while another still needs it. This is the direct answer to 'why can I not delete " +
				"this?' and shows both the protected resource and the one depending on it.",
			InputSchema: api.Object(nil),
			Handler:     usagesList,
		},
	}
}

func deletingResources(p api.Params) (*api.Result, error) {
	namespace := p.Args.OptionalString("namespace", "")
	includeRecent := p.Args.OptionalBool("includeRecent", false)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	deleting, warnings, err := p.Client.Deleting(p, namespace, includeRecent)
	if err != nil {
		return api.Error(err), nil
	}

	payload := map[string]any{
		"count":    len(deleting),
		"deleting": deleting,
		"warnings": warnings,
	}

	var text strings.Builder
	if len(deleting) == 0 {
		text.WriteString("Nothing is stuck deleting.")
		appendWarnings(&text, warnings)
		return api.Structured(text.String(), payload), nil
	}

	rows := make([][]string, 0, len(deleting))
	for _, resource := range deleting {
		rows = append(rows, []string{
			resource.Kind,
			orDash(resource.Namespace),
			resource.Name,
			resource.DeletingFor,
			resource.Reason,
		})
	}

	fmt.Fprintf(&text, "%d resource(s) are waiting to be deleted.\n", len(deleting))
	text.WriteString(api.Table(
		[]string{"KIND", "NAMESPACE", "NAME", "DELETING FOR", "WHY"}, rows))

	// Finalizers and blockers are the actionable detail, but too wide for the
	// table, so they follow underneath.
	var detail strings.Builder
	for _, resource := range deleting {
		fmt.Fprintf(&detail, "%s/%s\n", resource.Kind, resource.Name)
		if len(resource.BlockedBy) > 0 {
			fmt.Fprintf(&detail, "  blocked by: %s\n", strings.Join(resource.BlockedBy, "; "))
		}
		if len(resource.Finalizers) > 0 {
			fmt.Fprintf(&detail, "  finalizers: %s\n", strings.Join(resource.Finalizers, ", "))
		}
		if resource.Message != "" {
			fmt.Fprintf(&detail, "  status: %s\n", resource.Message)
		}
	}
	api.Section(&text, "Detail:", strings.TrimRight(detail.String(), "\n"))
	appendWarnings(&text, warnings)

	return api.Structured(text.String(), payload), nil
}

func usagesList(p api.Params) (*api.Result, error) {
	usages, err := p.Client.Usages(p)
	if err != nil {
		return api.Error(err), nil
	}
	if len(usages) == 0 {
		return api.Structured("No Usages are defined, so nothing is protected from deletion.",
			map[string]any{"count": 0, "usages": usages}), nil
	}

	rows := make([][]string, 0, len(usages))
	for _, usage := range usages {
		needs := "-"
		if usage.ByKind != "" {
			needs = usage.ByKind + "/" + usage.ByName
		}
		rows = append(rows, []string{
			usage.Name,
			orDash(usage.Namespace),
			usage.OfKind + "/" + usage.OfName,
			needs,
			orDash(usage.Reason),
			usage.Age,
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d Usage(s).\n", len(usages))
	text.WriteString(api.Table(
		[]string{"NAME", "NAMESPACE", "PROTECTS", "NEEDED BY", "REASON", "AGE"}, rows))

	return api.Structured(text.String(), map[string]any{"count": len(usages), "usages": usages}), nil
}

func appendWarnings(text *strings.Builder, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	api.Section(text, fmt.Sprintf("Warnings (%d):", len(warnings)), "- "+strings.Join(warnings, "\n- "))
}
