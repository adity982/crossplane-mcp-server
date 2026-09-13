package diagnostics

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func impactTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_impact",
			Title: "Resource: deletion impact",
			Description: "Work out what would happen if a resource were deleted, before anyone deletes it. " +
				"This reports every resource that would be torn down with it, because deleting a claim or a " +
				"composite deletes the whole composition tree beneath it and the real cloud infrastructure at " +
				"the bottom, and every Usage that would either block the delete or be left dangling. Read " +
				"only: it tells you the blast radius, it does not delete anything.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind":      api.StringProp("Kind of the resource whose deletion you are considering."),
				"name":      api.StringProp("Name of the resource."),
				"group":     api.GroupProp,
				"namespace": api.StringProp("Namespace of the resource. Ignored for cluster scoped resources."),
			}, "kind", "name"),
			Handler: impact,
		},
	}
}

// impactReport is what the model reads.
type impactReport struct {
	Kind          string             `json:"kind"`
	Name          string             `json:"name"`
	Namespace     string             `json:"namespace,omitempty"`
	Blocked       bool               `json:"blocked"`
	BlockedBy     []crossplane.Usage `json:"blockedBy,omitempty"`
	Dangling      []crossplane.Usage `json:"dangling,omitempty"`
	WouldDelete   []doomed           `json:"wouldDelete"`
	ExternalCount int                `json:"externalResourceCount"`
	Verdict       string             `json:"verdict"`
}

// doomed is a resource that would go away with the one being deleted.
type doomed struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Namespace    string `json:"namespace,omitempty"`
	APIVersion   string `json:"apiVersion"`
	ExternalName string `json:"externalName,omitempty"`
	Managed      bool   `json:"managed"`
}

func impact(p api.Params) (*api.Result, error) {
	kind := p.Args.String("kind")
	name := p.Args.String("name")
	group := p.Args.OptionalString("group", "")
	namespace := p.Args.OptionalString("namespace", "")
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	resource, err := p.Client.ResolveKind(p, kind, group)
	if err != nil {
		return api.Error(err), nil
	}
	if resource.Namespaced && namespace == "" {
		namespace = p.Client.DefaultNamespace()
	}
	obj, err := p.Client.Get(p, resource, namespace, name)
	if err != nil {
		return api.Error(err), nil
	}

	tree := p.Client.Tree(p, obj)
	wouldDelete := collectDoomed(tree, nil)

	external := 0
	for _, d := range wouldDelete {
		if d.ExternalName != "" {
			external++
		}
	}

	report := impactReport{
		Kind:          obj.GetKind(),
		Name:          obj.GetName(),
		Namespace:     obj.GetNamespace(),
		WouldDelete:   wouldDelete,
		ExternalCount: external,
	}

	// Usages are best effort: a control plane without the protection API still
	// deserves the tree half of the answer.
	if usages, err := p.Client.Usages(p); err == nil {
		report.BlockedBy, report.Dangling = classifyUsages(usages, wouldDelete, obj.GetKind(), obj.GetNamespace(), obj.GetName())
	}
	report.Blocked = len(report.BlockedBy) > 0
	report.Verdict = impactVerdict(report)

	return api.Structured(renderImpact(report, tree), report), nil
}

// collectDoomed flattens the composition tree. Everything below the root goes
// when the root goes, and the root itself goes too.
//
// A leaf is a managed resource: composites and claims always reference
// something below them, so anything with no children is what actually holds
// the external infrastructure.
func collectDoomed(node crossplane.TreeNode, acc []doomed) []doomed {
	acc = append(acc, doomed{
		Kind:       node.Kind,
		Name:       node.Name,
		Namespace:  node.Namespace,
		APIVersion: node.APIVersion,
		Managed:    len(node.Children) == 0,
	})
	for _, child := range node.Children {
		acc = collectDoomed(child, acc)
	}
	return acc
}

// classifyUsages splits usages into the ones that would block this delete and
// the ones that would be left pointing at something that no longer exists.
func classifyUsages(usages []crossplane.Usage, wouldDelete []doomed, kind, namespace, name string) (blocking, dangling []crossplane.Usage) {
	doomedKeys := make(map[string]bool, len(wouldDelete))
	for _, d := range wouldDelete {
		doomedKeys[usageKey(d.Kind, d.Name)] = true
	}

	for _, usage := range usages {
		switch {
		case strings.EqualFold(usage.OfKind, kind) && usage.OfName == name:
			blocking = append(blocking, usage)
		case doomedKeys[usageKey(usage.OfKind, usage.OfName)]:
			// Something further down the tree is protected, so the delete
			// stalls there instead of at the root.
			blocking = append(blocking, usage)
		case usage.ByKind != "" && doomedKeys[usageKey(usage.ByKind, usage.ByName)]:
			dangling = append(dangling, usage)
		}
	}
	return blocking, dangling
}

func usageKey(kind, name string) string { return strings.ToLower(kind) + "/" + name }

func impactVerdict(report impactReport) string {
	switch {
	case report.Blocked:
		return fmt.Sprintf("The delete would be blocked. %d Usage(s) protect this resource or something beneath it.",
			len(report.BlockedBy))
	case report.ExternalCount > 0:
		return fmt.Sprintf("Deleting this removes %d resource(s), %d of which have an external name and therefore real infrastructure behind them.",
			len(report.WouldDelete), report.ExternalCount)
	case len(report.WouldDelete) > 1:
		return fmt.Sprintf("Deleting this removes %d resource(s) in the composition tree.", len(report.WouldDelete))
	default:
		return "Nothing else depends on this resource. Deleting it affects only itself."
	}
}

func renderImpact(report impactReport, tree crossplane.TreeNode) string {
	var text strings.Builder

	fmt.Fprintf(&text, "Deletion impact for %s %s\n\nVerdict: %s\n",
		report.Kind, path(report.Namespace, report.Name), report.Verdict)

	rows := make([][]string, 0, len(report.WouldDelete))
	for _, d := range report.WouldDelete {
		kindOfThing := "composite/claim"
		if d.Managed {
			kindOfThing = "managed"
		}
		rows = append(rows, []string{d.Kind, path(d.Namespace, d.Name), kindOfThing, orDash(d.ExternalName)})
	}
	api.Section(&text, fmt.Sprintf("Would be deleted (%d):", len(report.WouldDelete)),
		api.Table([]string{"KIND", "NAME", "ROLE", "EXTERNAL NAME"}, rows))

	if len(report.BlockedBy) > 0 {
		api.Section(&text, "Blocked by these Usages:", renderUsageRows(report.BlockedBy))
	}
	if len(report.Dangling) > 0 {
		api.Section(&text, "Usages that would be left dangling:", renderUsageRows(report.Dangling))
	}
	api.Section(&text, "Tree:", crossplane.RenderTree(tree))

	return strings.TrimRight(text.String(), "\n")
}

func renderUsageRows(usages []crossplane.Usage) string {
	rows := make([][]string, 0, len(usages))
	for _, u := range usages {
		needs := "-"
		if u.ByKind != "" {
			needs = u.ByKind + "/" + u.ByName
		}
		rows = append(rows, []string{u.Name, u.OfKind + "/" + u.OfName, needs, orDash(u.Reason)})
	}
	return api.Table([]string{"USAGE", "PROTECTS", "NEEDED BY", "REASON"}, rows)
}

func path(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}
