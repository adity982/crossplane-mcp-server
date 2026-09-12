package crossplane

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// maxTreeDepth stops traversal of pathological or cyclic composition graphs.
// Ten levels is far deeper than any composition anybody should be writing.
const maxTreeDepth = 10

// TreeNode is one object in a composition tree, alongside everything it
// composed. This mirrors what `crossplane beta trace` shows and is the single
// most useful view when debugging why a claim is not ready.
type TreeNode struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Ready      string `json:"ready"`
	Synced     string `json:"synced"`
	Message    string `json:"message,omitempty"`
	// Composition names the Composition selected for a composite resource.
	Composition string `json:"composition,omitempty"`
	// Children are the resources this object composed, or the composite
	// resource a claim points at.
	Children []TreeNode `json:"children,omitempty"`
	// Error records why a referenced child could not be fetched.
	Error string `json:"error,omitempty"`
}

// Tree walks the composition graph rooted at the given object.
//
// The walk follows spec.resourceRef (claim to composite) and
// spec.resourceRefs (composite to composed resources). Composed resources may
// themselves be composite, so the traversal is recursive.
func (c *Client) Tree(ctx context.Context, root *unstructured.Unstructured) TreeNode {
	seen := map[string]bool{}
	return c.treeNode(ctx, root, seen, 0)
}

func (c *Client) treeNode(ctx context.Context, obj *unstructured.Unstructured, seen map[string]bool, depth int) TreeNode {
	summary := Summarize(obj)
	node := TreeNode{
		APIVersion:  summary.APIVersion,
		Kind:        summary.Kind,
		Name:        summary.Name,
		Namespace:   summary.Namespace,
		Ready:       summary.Ready,
		Synced:      summary.Synced,
		Message:     summary.Message,
		Composition: CompositionName(obj),
	}

	key := node.APIVersion + "/" + node.Kind + "/" + node.Namespace + "/" + node.Name
	if depth >= maxTreeDepth || seen[key] {
		return node
	}
	seen[key] = true

	for _, ref := range childReferences(obj) {
		child, err := c.GetByReference(ctx, ref.APIVersion, ref.Kind, ref.Namespace, ref.Name)
		if err != nil {
			node.Children = append(node.Children, TreeNode{
				APIVersion: ref.APIVersion,
				Kind:       ref.Kind,
				Name:       ref.Name,
				Namespace:  ref.Namespace,
				Ready:      "-",
				Synced:     "-",
				Error:      err.Error(),
			})
			continue
		}
		node.Children = append(node.Children, c.treeNode(ctx, child, seen, depth+1))
	}
	return node
}

// objectReference is the subset of a Crossplane resource reference we need.
type objectReference struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
}

// childReferences collects the references an object publishes to the
// resources below it in the composition graph.
func childReferences(obj *unstructured.Unstructured) []objectReference {
	var refs []objectReference

	// A claim points at exactly one composite resource.
	if single := nestedMap(obj, "spec", "resourceRef"); single != nil {
		if ref, ok := toReference(single, obj.GetNamespace()); ok {
			refs = append(refs, ref)
		}
	}

	// A composite resource points at everything it composed. Crossplane v2
	// moved this to status.resourceRefs, so check both.
	for _, path := range [][]string{{"spec", "resourceRefs"}, {"status", "resourceRefs"}} {
		for _, item := range nestedSlice(obj, path...) {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if ref, ok := toReference(entry, obj.GetNamespace()); ok {
				refs = append(refs, ref)
			}
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].Name < refs[j].Name
	})
	return dedupeReferences(refs)
}

func toReference(entry map[string]any, defaultNamespace string) (objectReference, bool) {
	ref := objectReference{
		APIVersion: stringField(entry, "apiVersion"),
		Kind:       stringField(entry, "kind"),
		Name:       stringField(entry, "name"),
		Namespace:  stringField(entry, "namespace"),
	}
	if ref.APIVersion == "" || ref.Kind == "" || ref.Name == "" {
		return objectReference{}, false
	}
	if ref.Namespace == "" {
		ref.Namespace = defaultNamespace
	}
	return ref, true
}

func dedupeReferences(refs []objectReference) []objectReference {
	if len(refs) < 2 {
		return refs
	}
	out := refs[:0]
	seen := map[objectReference]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// CompositionName reports which Composition a composite resource is using.
// Crossplane v2 moved the reference under spec.crossplane, so check both.
func CompositionName(obj *unstructured.Unstructured) string {
	if name := nestedString(obj, "spec", "compositionRef", "name"); name != "" {
		return name
	}
	return nestedString(obj, "spec", "crossplane", "compositionRef", "name")
}

// RenderTree draws a tree as indented text. The model reads the JSON, but a
// human reading the transcript gets something they recognise.
func RenderTree(root TreeNode) string {
	var b strings.Builder
	b.WriteString(treeLine(root))
	b.WriteString("\n")
	renderChildren(&b, root.Children, "")
	return b.String()
}

func renderChildren(b *strings.Builder, children []TreeNode, prefix string) {
	for i, child := range children {
		last := i == len(children)-1
		connector, indent := "├─ ", "│  "
		if last {
			connector, indent = "└─ ", "   "
		}
		b.WriteString(prefix + connector + treeLine(child))
		b.WriteString("\n")
		renderChildren(b, child.Children, prefix+indent)
	}
}

func treeLine(node TreeNode) string {
	line := fmt.Sprintf("%s/%s  READY=%s SYNCED=%s", node.Kind, node.Name, node.Ready, node.Synced)
	switch {
	case node.Error != "":
		line += "  ERROR: " + node.Error
	case node.Message != "":
		line += "  " + node.Message
	}
	return line
}
