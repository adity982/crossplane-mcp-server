package crossplane

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// nestedString reads a string at a path, returning "" when the path is absent
// or holds another type. Crossplane objects are sparsely populated while they
// reconcile, so "absent" is the common case rather than an error.
func nestedString(obj *unstructured.Unstructured, fields ...string) string {
	value, found, err := unstructured.NestedString(obj.Object, fields...)
	if err != nil || !found {
		return ""
	}
	return value
}

// nestedSlice reads a slice at a path, returning nil when it is absent.
func nestedSlice(obj *unstructured.Unstructured, fields ...string) []any {
	value, found, err := unstructured.NestedSlice(obj.Object, fields...)
	if err != nil || !found {
		return nil
	}
	return value
}

// nestedMap reads a map at a path, returning nil when it is absent.
func nestedMap(obj *unstructured.Unstructured, fields ...string) map[string]any {
	value, found, err := unstructured.NestedMap(obj.Object, fields...)
	if err != nil || !found {
		return nil
	}
	return value
}

// ownedBy reports whether obj has an owner reference with the given name. It
// is how we associate package revisions with their package.
func ownedBy(obj *unstructured.Unstructured, ownerName string) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Name == ownerName {
			return true
		}
	}
	return false
}

// imageTag extracts the tag or digest from an OCI image reference.
func imageTag(image string) string {
	if at := strings.LastIndex(image, "@"); at >= 0 {
		return image[at+1:]
	}
	// A colon before the last slash belongs to a registry port, not a tag.
	if colon := strings.LastIndex(image, ":"); colon > strings.LastIndex(image, "/") {
		return image[colon+1:]
	}
	return "latest"
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
