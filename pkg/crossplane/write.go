package crossplane

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// FieldManager is the field manager name this server applies with. Server side
// apply records it on every field we own, so an operator can see which changes
// came from an assistant and `kubectl apply` keeps working alongside us.
const FieldManager = "crossplane-mcp-server"

// WriteOptions modify a single write.
type WriteOptions struct {
	// DryRun sends the change to the API server for validation without
	// persisting it.
	DryRun bool
	// Force takes ownership of fields another field manager already owns.
	// Without it, applying over a field somebody else set is a conflict.
	Force bool
}

func (o WriteOptions) dryRun() []string {
	if !o.DryRun {
		return nil
	}
	return []string{metav1.DryRunAll}
}

// ApplyResult describes what a write did.
type ApplyResult struct {
	// Object is the state the API server returned.
	Object *unstructured.Unstructured
	// Created is true when the object did not exist before the apply.
	Created bool
	// DryRun is true when nothing was persisted.
	DryRun bool
}

// Action renders what happened in a word, which is what a tool wants to say
// first and what a model wants to read first.
func (r *ApplyResult) Action() string {
	switch {
	case r.DryRun:
		return "validated"
	case r.Created:
		return "created"
	default:
		return "updated"
	}
}

// Writable reports whether this server is willing to change the resource.
//
// The server deliberately refuses to write anything outside Crossplane's own
// world: a control plane assistant has no business creating Secrets, RBAC or
// Deployments, and the blast radius of a mistake is much smaller when the only
// reachable kinds are Crossplane's. RBAC should enforce the same thing, but a
// second check here costs nothing.
func (r APIResource) Writable() bool {
	for _, category := range []string{CategoryCrossplane, CategoryManaged, CategoryComposite, CategoryClaim} {
		if r.HasCategory(category) {
			return true
		}
	}
	return r.Group == "crossplane.io" || strings.HasSuffix(r.Group, ".crossplane.io")
}

// Apply server side applies an object, creating it when it does not exist.
//
// Server side apply rather than create: it is idempotent, so a model retrying
// a call does not turn into a duplicate resource or an "already exists" error
// it has to reason about.
func (c *Client) Apply(ctx context.Context, obj *unstructured.Unstructured, opts WriteOptions) (*ApplyResult, error) {
	name := obj.GetName()
	if name == "" {
		return nil, fmt.Errorf("metadata.name is required")
	}
	if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
		return nil, fmt.Errorf("apiVersion and kind are required")
	}

	resource, err := c.ResolveAPIVersionKind(ctx, obj.GetAPIVersion(), obj.GetKind())
	if err != nil {
		return nil, err
	}
	if !resource.Writable() {
		return nil, fmt.Errorf("%s is not a Crossplane resource: this server only writes Crossplane kinds "+
			"(anything in a *.crossplane.io group, or tagged managed, composite or claim)", resource.Kind)
	}

	ri, namespace := c.writeInterface(resource, obj.GetNamespace())
	if namespace != "" {
		obj.SetNamespace(namespace)
	} else {
		// A namespace on a cluster scoped object is rejected, and a manifest
		// that carries one is usually a copy of a namespaced example.
		obj.SetNamespace("")
	}
	Sanitize(obj)

	// Whether this is a create or an update is only visible before the apply,
	// and it is the first thing a reader of the answer wants to know.
	created := !exists(ctx, ri, name)

	applied, err := ri.Apply(ctx, name, obj, metav1.ApplyOptions{
		FieldManager: FieldManager,
		Force:        opts.Force,
		DryRun:       opts.dryRun(),
	})
	if err != nil {
		if apierrors.IsConflict(err) {
			return nil, fmt.Errorf("cannot apply %s %q: %w: another field manager owns some of these fields, "+
				"pass force=true to take them over", resource.Kind, name, err)
		}
		return nil, fmt.Errorf("cannot apply %s %q: %w", resource.Kind, name, err)
	}

	return &ApplyResult{Object: applied, Created: created, DryRun: opts.DryRun}, nil
}

// ResolveAPIVersionKind maps an apiVersion/kind pair, as written in a
// manifest, onto a discovered resource.
func (c *Client) ResolveAPIVersionKind(ctx context.Context, apiVersion, kind string) (APIResource, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return APIResource{}, fmt.Errorf("invalid apiVersion %q: %w", apiVersion, err)
	}

	all, err := c.APIResources(ctx)
	if err != nil {
		return APIResource{}, err
	}
	for _, r := range all {
		if r.Group == gv.Group && r.Kind == kind {
			// Discovery reports the preferred version, but a manifest may name
			// any served one, and the API server rejects a body whose
			// apiVersion does not match the path it was sent to. Keep the
			// plural name discovery found and the version the caller wrote.
			r.Version = gv.Version
			return r, nil
		}
	}
	return APIResource{}, &ErrNotFound{What: fmt.Sprintf("kind %q in group %q", kind, gv.Group)}
}

// Sanitize strips the fields the API server owns. They are meaningless in a
// request and server side apply rejects some of them outright, but they are
// present in anything copied out of `kubectl get -o yaml`, which is exactly
// what a model tends to send.
func Sanitize(obj *unstructured.Unstructured) {
	for _, field := range []string{"managedFields", "resourceVersion", "uid", "generation", "creationTimestamp", "selfLink"} {
		unstructured.RemoveNestedField(obj.Object, "metadata", field)
	}
	unstructured.RemoveNestedField(obj.Object, "status")
}

// Manifest renders an object as the YAML somebody would commit, with the
// fields the API server added removed.
func Manifest(obj *unstructured.Unstructured) (string, error) {
	clean := obj.DeepCopy()
	Sanitize(clean)
	encoded, err := yaml.Marshal(clean.Object)
	if err != nil {
		return "", fmt.Errorf("cannot render %s %q: %w", obj.GetKind(), obj.GetName(), err)
	}
	return string(encoded), nil
}

// writeInterface addresses a single object rather than a collection. Unlike a
// list, a write on a namespaced resource has to name a namespace, so an empty
// one falls back to the server's default instead of meaning "all namespaces".
func (c *Client) writeInterface(r APIResource, namespace string) (dynamic.ResourceInterface, string) {
	ri := c.dynamic.Resource(r.GroupVersionResource())
	if !r.Namespaced {
		return ri, ""
	}
	if namespace == "" {
		namespace = c.defaultNamespace
	}
	return ri.Namespace(namespace), namespace
}

// exists reports whether an object is already present. It is only used to
// describe what an apply did, so an error is reported as "present": claiming a
// resource was created when it was not is the more misleading answer.
func exists(ctx context.Context, ri dynamic.ResourceInterface, name string) bool {
	_, err := ri.Get(ctx, name, metav1.GetOptions{})
	return !apierrors.IsNotFound(err)
}
