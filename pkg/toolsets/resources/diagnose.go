package resources

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

// maxRootCauses caps how many failing resources are investigated in depth. A
// broken Composition leaves dozens of resources unready that share one cause,
// and reporting every one of them buries the answer.
const maxRootCauses = 5

func diagnosisTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_diagnose",
			Title: "Resource: diagnose",
			Description: "Explain why a claim, composite resource or managed resource is not Ready, and say " +
				"which resource is actually at fault. This walks the whole chain from the claim down to the " +
				"managed resources, finds the deepest failures rather than the symptom at the top, reads the " +
				"provider error out of each one's events, and checks whether the Composition and the providers " +
				"behind it are healthy. Prefer this over calling resource_tree, resource_get and resource_events " +
				"separately: it follows the order an experienced Crossplane operator would and returns a verdict.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind":      api.StringProp("Kind of the resource to diagnose, for example 'PostgreSQLInstance' or 'Bucket'."),
				"name":      api.StringProp("Name of the resource to diagnose."),
				"group":     api.GroupProp,
				"namespace": api.StringProp("Namespace of the resource. Ignored for cluster scoped resources."),
			}, "kind", "name"),
			Handler: diagnose,
		},
	}
}

// rootCause is one failing resource, with the evidence gathered about it.
type rootCause struct {
	APIVersion    string                 `json:"apiVersion"`
	Kind          string                 `json:"kind"`
	Name          string                 `json:"name"`
	Namespace     string                 `json:"namespace,omitempty"`
	Ready         string                 `json:"ready"`
	Synced        string                 `json:"synced"`
	Reason        string                 `json:"reason,omitempty"`
	Message       string                 `json:"message,omitempty"`
	ProviderError string                 `json:"providerError,omitempty"`
	ExternalName  string                 `json:"externalName,omitempty"`
	Conditions    []crossplane.Condition `json:"conditions,omitempty"`
	Events        []crossplane.Event     `json:"events,omitempty"`
}

// check is one control plane level question and its answer.
type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func diagnose(p api.Params) (*api.Result, error) {
	obj, failure := resolveResource(p)
	if failure != nil {
		return failure, nil
	}

	summary := crossplane.Summarize(obj)
	tree := p.Client.Tree(p, obj)

	if summary.Ready == crossplane.StatusTrue {
		text := fmt.Sprintf("%s %s is Ready. Nothing to diagnose.\n\n%s",
			summary.Kind, api.Path(summary.Namespace, summary.Name), crossplane.RenderTree(tree))
		return api.Structured(text, map[string]any{
			"root":    summary,
			"ready":   true,
			"verdict": "Ready",
			"tree":    tree,
		}), nil
	}

	causes := investigate(p, deepestUnready(tree))
	checks := controlPlaneChecks(p, obj, tree)
	verdict := verdictFor(summary, causes, checks)

	return api.Structured(renderDiagnosis(summary, tree, causes, checks, verdict), map[string]any{
		"root":       summary,
		"ready":      false,
		"verdict":    verdict,
		"rootCauses": causes,
		"checks":     checks,
		"tree":       tree,
	}), nil
}

// deepestUnready returns the unready nodes that have no unready descendants.
//
// A claim is almost always unready because something far below it failed, so
// the node the user asked about is the least interesting one in the tree.
func deepestUnready(node crossplane.TreeNode) []crossplane.TreeNode {
	below := make([]crossplane.TreeNode, 0, len(node.Children))
	for _, child := range node.Children {
		below = append(below, deepestUnready(child)...)
	}
	if len(below) > 0 {
		return below
	}
	if node.Ready != crossplane.StatusTrue {
		return []crossplane.TreeNode{node}
	}
	return nil
}

// investigate re-reads each failing resource to collect the detail the tree
// does not carry: its conditions, its events and its external name.
func investigate(p api.Params, nodes []crossplane.TreeNode) []rootCause {
	if len(nodes) > maxRootCauses {
		nodes = nodes[:maxRootCauses]
	}

	causes := make([]rootCause, 0, len(nodes))
	for _, node := range nodes {
		cause := rootCause{
			APIVersion: node.APIVersion,
			Kind:       node.Kind,
			Name:       node.Name,
			Namespace:  node.Namespace,
			Ready:      node.Ready,
			Synced:     node.Synced,
			Message:    node.Message,
		}

		obj, err := p.Client.GetByReference(p, node.APIVersion, node.Kind, node.Namespace, node.Name)
		if err != nil {
			cause.Message = strings.TrimSpace(cause.Message + " (could not re-read: " + err.Error() + ")")
			causes = append(causes, cause)
			continue
		}

		conditions := crossplane.Conditions(obj)
		cause.Conditions = conditions
		cause.ExternalName = crossplane.ExternalName(obj)
		if problem, found := firstFailing(conditions); found {
			cause.Reason = problem.Reason
			if problem.Message != "" {
				cause.Message = problem.Message
			}
		}
		if events, err := p.Client.EventsFor(p, obj); err == nil {
			cause.Events = events
			cause.ProviderError = firstWarning(events)
		}

		causes = append(causes, cause)
	}
	return causes
}

// controlPlaneChecks answers the questions that explain a whole class of
// failures at once: a missing Composition, or a provider that is not running.
func controlPlaneChecks(p api.Params, root *unstructured.Unstructured, tree crossplane.TreeNode) []check {
	checks := make([]check, 0, 2)

	if name := compositionInTree(root, tree); name != "" {
		checks = append(checks, compositionCheck(p, name))
	}
	checks = append(checks, providerCheck(p))
	return checks
}

func compositionCheck(p api.Params, name string) check {
	resource, err := p.Client.ResolveKind(p, "Composition", "apiextensions.crossplane.io")
	if err != nil {
		return check{Name: "composition", Detail: "could not resolve the Composition kind: " + err.Error()}
	}
	if _, err := p.Client.Get(p, resource, "", name); err != nil {
		return check{
			Name:   "composition",
			Detail: fmt.Sprintf("Composition %q could not be read: %v", name, err),
		}
	}
	return check{
		Name:   "composition",
		OK:     true,
		Detail: fmt.Sprintf("Composition %q exists. Run crossplane_composition_validate to check its function pipeline.", name),
	}
}

// providerCheck reports unhealthy providers rather than guessing which provider
// owns a failing resource. Mapping an API group to a package is unreliable, but
// an unhealthy provider explains failures across every kind it owns.
func providerCheck(p api.Params) check {
	packages, err := p.Client.Packages(p, crossplane.PackageProvider)
	if err != nil {
		return check{Name: "providers", Detail: "could not be read: " + err.Error()}
	}

	var unhealthy []string
	for _, pkg := range packages {
		if pkg.Installed != crossplane.StatusTrue || pkg.Healthy != crossplane.StatusTrue {
			unhealthy = append(unhealthy, fmt.Sprintf("%s (installed=%s healthy=%s)",
				pkg.Name, pkg.Installed, pkg.Healthy))
		}
	}
	if len(unhealthy) > 0 {
		return check{
			Name:   "providers",
			Detail: "not every provider is running, which can stall any resource it owns: " + strings.Join(unhealthy, ", "),
		}
	}
	return check{
		Name:   "providers",
		OK:     true,
		Detail: fmt.Sprintf("all %d provider(s) are Installed and Healthy", len(packages)),
	}
}

// verdictFor turns the evidence into the one sentence a user actually wants.
func verdictFor(root crossplane.Summary, causes []rootCause, checks []check) string {
	for _, c := range checks {
		if !c.OK && c.Name == "providers" {
			return "A provider is not healthy, so its resources cannot reconcile. " + c.Detail
		}
	}

	if len(causes) == 0 {
		if root.Message != "" {
			return fmt.Sprintf("%s reports %s, and nothing below it is failing. The cause is on the resource itself.",
				root.Kind, root.Message)
		}
		return fmt.Sprintf("%s is not Ready but reports no failing condition yet. It may not have been reconciled.", root.Kind)
	}

	first := causes[0]
	detail := firstNonEmpty(first.ProviderError, first.Message, first.Reason)
	if detail == "" {
		detail = fmt.Sprintf("it reports ready=%s synced=%s with no message", first.Ready, first.Synced)
	}
	if len(causes) == 1 {
		return fmt.Sprintf("%s/%s is the deepest failure: %s", first.Kind, first.Name, detail)
	}
	return fmt.Sprintf("%d resources are failing. The first is %s/%s: %s",
		len(causes), first.Kind, first.Name, detail)
}

func renderDiagnosis(root crossplane.Summary, tree crossplane.TreeNode, causes []rootCause, checks []check, verdict string) string {
	var text strings.Builder

	fmt.Fprintf(&text, "%s %s is NOT READY.\n\nVerdict: %s\n",
		root.Kind, api.Path(root.Namespace, root.Name), verdict)

	if len(causes) > 0 {
		rows := make([][]string, 0, len(causes))
		for _, c := range causes {
			rows = append(rows, []string{
				c.Kind,
				api.Path(c.Namespace, c.Name),
				c.Ready,
				c.Synced,
				api.OrDash(c.Reason),
				api.OrDash(firstNonEmpty(c.ProviderError, c.Message)),
			})
		}
		api.Section(&text, "Root causes (deepest failing resources):",
			api.Table([]string{"KIND", "NAME", "READY", "SYNCED", "REASON", "DETAIL"}, rows))
	}

	rows := make([][]string, 0, len(checks))
	for _, c := range checks {
		state := "FAIL"
		if c.OK {
			state = "OK"
		}
		rows = append(rows, []string{c.Name, state, c.Detail})
	}
	api.Section(&text, "Control plane checks:", api.Table([]string{"CHECK", "STATE", "DETAIL"}, rows))
	api.Section(&text, "Chain:", crossplane.RenderTree(tree))

	return strings.TrimRight(text.String(), "\n")
}

// compositionInTree finds the Composition the composite resolved to, looking at
// the root first and then at the composite the claim points to.
func compositionInTree(root *unstructured.Unstructured, tree crossplane.TreeNode) string {
	if name := crossplane.CompositionName(root); name != "" {
		return name
	}
	if tree.Composition != "" {
		return tree.Composition
	}
	for _, child := range tree.Children {
		if child.Composition != "" {
			return child.Composition
		}
	}
	return ""
}

func firstFailing(conditions []crossplane.Condition) (crossplane.Condition, bool) {
	for _, c := range conditions {
		if !c.IsTrue() {
			return c, true
		}
	}
	return crossplane.Condition{}, false
}

// firstWarning returns the newest Warning event message, which is where a
// provider records the cloud API error behind a ReconcileError condition.
func firstWarning(events []crossplane.Event) string {
	for _, e := range events {
		if strings.EqualFold(e.Type, "Warning") && e.Message != "" {
			return e.Message
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
