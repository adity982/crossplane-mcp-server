package resources

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func inspectionTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_resource_get",
			Title: "Resource: describe",
			Description: "Describe a single Crossplane resource: its conditions, its external name, the " +
				"Composition it selected, the events recorded against it and, optionally, its full manifest. " +
				"This is the tool to reach for once a list tool has told you which resource is unhappy.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind":      api.StringProp("Kind of the resource, for example 'Bucket' or 'XPostgreSQLInstance'."),
				"name":      api.StringProp("Name of the resource."),
				"group":     api.GroupProp,
				"namespace": api.StringProp("Namespace of the resource. Ignored for cluster scoped resources."),
				"manifest":  api.BoolProp("Include the full YAML manifest. Defaults to false because manifests are large."),
			}, "kind", "name"),
			Handler: resourceGet,
		},
		{
			Name:  "crossplane_resource_tree",
			Title: "Resource: composition tree",
			Description: "Show the composition tree rooted at a claim or composite resource: the composite it " +
				"resolved to, every resource that composition created, and the status of each. This is the " +
				"equivalent of 'crossplane beta trace' and is the fastest way to find which composed resource " +
				"is keeping a claim from becoming Ready.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind":      api.StringProp("Kind of the root claim or composite resource."),
				"name":      api.StringProp("Name of the root claim or composite resource."),
				"group":     api.GroupProp,
				"namespace": api.StringProp("Namespace of the root resource. Ignored for cluster scoped resources."),
			}, "kind", "name"),
			Handler: resourceTree,
		},
		{
			Name:  "crossplane_resource_events",
			Title: "Resource: events",
			Description: "Show the Kubernetes events recorded against a single Crossplane resource. Crossplane " +
				"puts the underlying provider or cloud API error in an event, so this often carries the real " +
				"reason a resource is failing when the conditions only say 'ReconcileError'.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind":      api.StringProp("Kind of the resource."),
				"name":      api.StringProp("Name of the resource."),
				"group":     api.GroupProp,
				"namespace": api.StringProp("Namespace of the resource. Ignored for cluster scoped resources."),
			}, "kind", "name"),
			Handler: resourceEvents,
		},
	}
}

func resourceGet(p api.Params) (*api.Result, error) {
	obj, failure := resolveResource(p)
	if failure != nil {
		return failure, nil
	}
	includeManifest := p.Args.OptionalBool("manifest", false)
	if argErr := p.Args.Err(); argErr != nil {
		return api.Error(argErr), nil
	}

	summary := crossplane.Summarize(obj)
	events, eventsErr := p.Client.EventsFor(p, obj)

	payload := map[string]any{
		"apiVersion":      summary.APIVersion,
		"kind":            summary.Kind,
		"name":            summary.Name,
		"namespace":       summary.Namespace,
		"age":             summary.Age,
		"conditions":      summary.Conditions,
		"externalName":    crossplane.ExternalName(obj),
		"composition":     crossplane.CompositionName(obj),
		"labels":          obj.GetLabels(),
		"annotations":     obj.GetAnnotations(),
		"ownerReferences": obj.GetOwnerReferences(),
	}
	if eventsErr == nil {
		payload["events"] = events
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%s %s", summary.Kind, resourcePath(summary.Namespace, summary.Name))
	fmt.Fprintf(&text, "\napiVersion: %s\nage: %s", summary.APIVersion, summary.Age)
	if external := crossplane.ExternalName(obj); external != "" {
		fmt.Fprintf(&text, "\nexternal-name: %s", external)
	}
	if composition := crossplane.CompositionName(obj); composition != "" {
		fmt.Fprintf(&text, "\ncomposition: %s", composition)
	}

	api.Section(&text, "Conditions:", renderConditions(summary.Conditions))
	switch {
	case eventsErr != nil:
		api.Section(&text, "Events:", "could not be read: "+eventsErr.Error())
	default:
		api.Section(&text, "Events:", renderEvents(events))
	}

	if includeManifest {
		manifest, marshalErr := yaml.Marshal(obj.Object)
		if marshalErr != nil {
			return api.Error(fmt.Errorf("cannot render manifest: %w", marshalErr)), nil
		}
		payload["manifest"] = string(manifest)
		api.Section(&text, "Manifest:", string(manifest))
	}

	return api.Structured(text.String(), payload), nil
}

func resourceTree(p api.Params) (*api.Result, error) {
	obj, failure := resolveResource(p)
	if failure != nil {
		return failure, nil
	}

	tree := p.Client.Tree(p, obj)
	unready := collectUnready(tree, nil)

	var text strings.Builder
	text.WriteString(crossplane.RenderTree(tree))
	if len(unready) > 0 {
		api.Section(&text, fmt.Sprintf("%d resource(s) in this tree are not Ready:", len(unready)),
			"- "+strings.Join(unready, "\n- "))
	}

	return api.Structured(strings.TrimRight(text.String(), "\n"), map[string]any{
		"tree":     tree,
		"unready":  unready,
		"allReady": len(unready) == 0,
	}), nil
}

func resourceEvents(p api.Params) (*api.Result, error) {
	obj, failure := resolveResource(p)
	if failure != nil {
		return failure, nil
	}

	events, err := p.Client.EventsFor(p, obj)
	if err != nil {
		return api.Error(err), nil
	}
	if len(events) == 0 {
		return api.Structured(fmt.Sprintf("No events recorded against %s %s.",
			obj.GetKind(), resourcePath(obj.GetNamespace(), obj.GetName())),
			map[string]any{"count": 0, "events": []crossplane.Event{}}), nil
	}

	return api.Structured(renderEvents(events), map[string]any{
		"count":  len(events),
		"events": events,
	}), nil
}

// resolveResource reads the kind/name/group/namespace arguments shared by the
// inspection tools and fetches the object.
//
// It returns either the object, or a Result describing why it could not be
// found. Exactly one of the two is non-nil.
func resolveResource(p api.Params) (*unstructured.Unstructured, *api.Result) {
	kind := p.Args.String("kind")
	name := p.Args.String("name")
	group := p.Args.OptionalString("group", "")
	namespace := p.Args.OptionalString("namespace", "")
	if err := p.Args.Err(); err != nil {
		return nil, api.Error(err)
	}

	resource, err := p.Client.ResolveKind(p, kind, group)
	if err != nil {
		return nil, api.Error(err)
	}
	if resource.Namespaced && namespace == "" {
		namespace = p.Client.DefaultNamespace()
	}

	obj, err := p.Client.Get(p, resource, namespace, name)
	if err != nil {
		return nil, api.Error(err)
	}
	return obj, nil
}

func renderConditions(conditions []crossplane.Condition) string {
	if len(conditions) == 0 {
		return "none reported yet, the resource has not been reconciled"
	}
	rows := make([][]string, 0, len(conditions))
	for _, c := range conditions {
		rows = append(rows, []string{c.Type, c.Status, orDash(c.Reason), orDash(c.Message), orDash(c.LastTransitionTime)})
	}
	return api.Table([]string{"TYPE", "STATUS", "REASON", "MESSAGE", "LAST TRANSITION"}, rows)
}

func renderEvents(events []crossplane.Event) string {
	if len(events) == 0 {
		return "none"
	}
	rows := make([][]string, 0, len(events))
	for _, e := range events {
		rows = append(rows, []string{e.Type, e.Reason, e.Age, itoa(int(e.Count)), e.Message})
	}
	return api.Table([]string{"TYPE", "REASON", "AGE", "COUNT", "MESSAGE"}, rows)
}

// collectUnready flattens a tree into the list of resources that are not Ready.
func collectUnready(node crossplane.TreeNode, acc []string) []string {
	if node.Ready != crossplane.StatusTrue {
		entry := fmt.Sprintf("%s/%s (ready=%s, synced=%s)", node.Kind, node.Name, node.Ready, node.Synced)
		switch {
		case node.Error != "":
			entry += ": " + node.Error
		case node.Message != "":
			entry += ": " + node.Message
		}
		acc = append(acc, entry)
	}
	for _, child := range node.Children {
		acc = collectUnready(child, acc)
	}
	return acc
}

// indexObjects builds a lookup from kind/namespace/name to object, so list
// tools can enrich a summary without a second API call.
func indexObjects(objects []unstructured.Unstructured) map[string]*unstructured.Unstructured {
	index := make(map[string]*unstructured.Unstructured, len(objects))
	for i := range objects {
		obj := &objects[i]
		index[objectKey(obj.GetKind(), obj.GetNamespace(), obj.GetName())] = obj
	}
	return index
}

func objectKey(kind, namespace, name string) string {
	return kind + "/" + namespace + "/" + name
}

func resourcePath(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}
