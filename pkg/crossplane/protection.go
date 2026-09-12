package crossplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GroupProtection is the API group of the Crossplane deletion protection
// types. Older control planes keep Usage in apiextensions instead.
const GroupProtection = "protection.crossplane.io"

// Usage is the summarised view of a Usage or ClusterUsage, which tells
// Crossplane to refuse to delete one resource while another needs it.
type Usage struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Age       string `json:"age"`

	// Of identifies the protected resource, the one that cannot be deleted.
	OfKind string `json:"ofKind"`
	OfName string `json:"ofName"`
	// By identifies the resource that needs it. Empty when the usage exists
	// only to state a reason.
	ByKind string `json:"byKind,omitempty"`
	ByName string `json:"byName,omitempty"`

	Reason string `json:"reason,omitempty"`
	// ReplayDeletion asks Crossplane to retry the blocked delete once the
	// usage goes away.
	ReplayDeletion bool `json:"replayDeletion,omitempty"`
}

// protects reports whether this usage blocks deletion of the given object.
func (u Usage) protects(kind, namespace, name string) bool {
	if !strings.EqualFold(u.OfKind, kind) || u.OfName != name {
		return false
	}
	return u.Namespace == "" || u.Namespace == namespace
}

// Usages lists the Usage and ClusterUsage objects on the control plane.
//
// Both the modern protection.crossplane.io group and the older
// apiextensions.crossplane.io one are searched, because a control plane part
// way through an upgrade can have either.
func (c *Client) Usages(ctx context.Context) ([]Usage, error) {
	kinds := []struct{ kind, group string }{
		{"Usage", GroupProtection},
		{"ClusterUsage", GroupProtection},
		{"Usage", GroupAPIExtensions},
	}

	usages := make([]Usage, 0, 8)
	seen := map[string]bool{}
	var lastErr error
	for _, want := range kinds {
		resource, err := c.ResolveKind(ctx, want.kind, want.group)
		if err != nil {
			lastErr = err
			continue
		}
		list, err := c.List(ctx, resource, ListOptions{})
		if err != nil {
			lastErr = err
			continue
		}
		for i := range list.Items {
			obj := &list.Items[i]
			key := obj.GetKind() + "/" + obj.GetNamespace() + "/" + obj.GetName()
			if seen[key] {
				continue
			}
			seen[key] = true
			usages = append(usages, summarizeUsage(obj))
		}
	}

	if len(usages) == 0 && len(seen) == 0 && lastErr != nil {
		var notFound *ErrNotFound
		if errors.As(lastErr, &notFound) {
			return nil, &ErrNotFound{What: "Usage support, this control plane does not install it"}
		}
	}
	sort.Slice(usages, func(i, j int) bool { return usages[i].Name < usages[j].Name })
	return usages, nil
}

func summarizeUsage(obj *unstructured.Unstructured) Usage {
	usage := Usage{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Age:       Age(obj),
		Reason:    nestedString(obj, "spec", "reason"),
	}
	if replay, found, _ := unstructured.NestedBool(obj.Object, "spec", "replayDeletion"); found {
		usage.ReplayDeletion = replay
	}

	// spec.of and spec.by each hold an apiVersion, kind and a resourceRef.
	if of := nestedMap(obj, "spec", "of"); of != nil {
		usage.OfKind = stringField(of, "kind")
		usage.OfName = referencedName(of)
	}
	if by := nestedMap(obj, "spec", "by"); by != nil {
		usage.ByKind = stringField(by, "kind")
		usage.ByName = referencedName(by)
	}
	return usage
}

func referencedName(reference map[string]any) string {
	ref, _ := reference["resourceRef"].(map[string]any)
	return stringField(ref, "name")
}

// DeletingResource is an object that has been asked to go away but has not.
type DeletingResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`

	// DeletingFor is how long the object has had a deletion timestamp. A
	// healthy delete completes in seconds, so anything longer is stuck.
	DeletingFor string `json:"deletingFor"`
	// Finalizers are what actually hold the object in place. Kubernetes will
	// not remove it until every one of them is gone.
	Finalizers []string `json:"finalizers,omitempty"`
	// BlockedBy names the usages that are refusing the delete, if any.
	BlockedBy []string `json:"blockedBy,omitempty"`
	// Reason is the most likely explanation, in plain language.
	Reason string `json:"reason"`
	// Message is the first unsatisfied condition message, which often carries
	// the provider's own explanation.
	Message string `json:"message,omitempty"`
}

// stuckAfter is how long a deletion has to be pending before it is worth
// reporting as stuck rather than merely in progress.
const stuckAfter = 30 * time.Second

// Deleting finds every Crossplane object waiting to be deleted, and explains
// what is holding each one up.
//
// A delete that never finishes is one of the more confusing Crossplane
// failures, because kubectl reports success and then nothing happens.
func (c *Client) Deleting(ctx context.Context, namespace string, includeRecent bool) ([]DeletingResource, []string, error) {
	usages, usageErr := c.Usages(ctx)
	_ = usageErr // a control plane without Usage support simply has none

	var (
		deleting []DeletingResource
		warnings []string
	)
	for _, category := range []string{CategoryManaged, CategoryComposite, CategoryClaim} {
		result, err := c.Query(ctx, Query{Category: category, Namespace: namespace})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not search %s resources: %v", category, err))
			continue
		}
		warnings = append(warnings, result.Warnings...)

		for i := range result.Objects {
			obj := &result.Objects[i]
			timestamp := obj.GetDeletionTimestamp()
			if timestamp == nil {
				continue
			}
			pending := time.Since(timestamp.Time)
			if !includeRecent && pending < stuckAfter {
				continue
			}
			deleting = append(deleting, describeDeleting(obj, Duration(pending), usages))
		}
	}

	sort.Slice(deleting, func(i, j int) bool {
		if deleting[i].Kind != deleting[j].Kind {
			return deleting[i].Kind < deleting[j].Kind
		}
		return deleting[i].Name < deleting[j].Name
	})
	return deleting, warnings, nil
}

func describeDeleting(obj *unstructured.Unstructured, pending string, usages []Usage) DeletingResource {
	resource := DeletingResource{
		APIVersion:  obj.GetAPIVersion(),
		Kind:        obj.GetKind(),
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		DeletingFor: pending,
		Finalizers:  obj.GetFinalizers(),
		Message:     FirstProblem(Conditions(obj)),
	}

	for _, usage := range usages {
		if usage.protects(resource.Kind, resource.Namespace, resource.Name) {
			blocker := usage.Name
			if usage.ByKind != "" {
				blocker = fmt.Sprintf("%s (needed by %s/%s)", usage.Name, usage.ByKind, usage.ByName)
			}
			if usage.Reason != "" {
				blocker += ": " + usage.Reason
			}
			resource.BlockedBy = append(resource.BlockedBy, blocker)
		}
	}

	resource.Reason = explainDeletion(resource)
	return resource
}

// explainDeletion turns finalizers into an explanation an operator can act on.
func explainDeletion(resource DeletingResource) string {
	switch {
	case len(resource.BlockedBy) > 0:
		return "a Usage is protecting it, delete the usage or the resource that needs it"
	case len(resource.Finalizers) == 0:
		return "no finalizers remain, the API server should remove it imminently"
	}

	for _, finalizer := range resource.Finalizers {
		switch {
		case strings.Contains(finalizer, "composite"), strings.Contains(finalizer, "apiextensions"):
			return "Crossplane is still deleting the resources this one composed"
		case strings.Contains(finalizer, "managed"), strings.Contains(finalizer, "finalizer.managedresource"):
			return "the provider has not confirmed the external resource is gone, " +
				"check its events and whether its credentials still work"
		case strings.Contains(finalizer, "usage"):
			return "a Usage is protecting it, delete the usage or the resource that needs it"
		}
	}
	return "held by finalizer " + strings.Join(resource.Finalizers, ", ")
}
