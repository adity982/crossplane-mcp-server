// Package crossplane knows how to talk to a Crossplane control plane.
//
// Crossplane is deliberately open ended: managed resources are contributed by
// providers at runtime, so there is no fixed set of Go types we can compile
// against. Everything here therefore goes through the dynamic client and the
// discovery API, and leans on the categories ("crossplane", "managed",
// "composite", "claim") that Crossplane stamps onto every CRD it installs.
package crossplane

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// Categories that Crossplane adds to the CRDs it manages. Providers add
// `managed` to every managed resource, and the API extensions controller adds
// `composite` and `claim` to the CRDs generated from an XRD.
const (
	CategoryCrossplane = "crossplane"
	CategoryManaged    = "managed"
	CategoryComposite  = "composite"
	CategoryClaim      = "claim"
)

// discoveryTTL is how long a discovery snapshot is reused. Installing a
// provider rewrites the API surface, so we refresh often enough to notice but
// not so often that every tool call pays for a full discovery round trip.
const discoveryTTL = 30 * time.Second

// Client is a read-oriented facade over a Crossplane control plane.
//
// It is safe for concurrent use.
type Client struct {
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
	core      kubernetes.Interface

	// defaultNamespace is used by namespaced tools when the caller does not
	// pass one explicitly.
	defaultNamespace string
	// target is the name of the cluster this client talks to.
	target string

	mu       sync.Mutex
	apis     []APIResource
	apisAt   time.Time
	mapper   meta.RESTMapper
	mapperAt time.Time
}

// APIResource is a flattened view of a single discoverable resource. It
// carries just enough information to list the resource dynamically and to
// decide which Crossplane concept it represents.
type APIResource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Kind       string   `json:"kind"`
	Resource   string   `json:"resource"`
	Namespaced bool     `json:"namespaced"`
	Categories []string `json:"categories,omitempty"`
	ShortNames []string `json:"shortNames,omitempty"`
	Verbs      []string `json:"-"`
}

// GroupVersionResource returns the GVR used to address the resource.
func (r APIResource) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}
}

// GroupVersionKind returns the GVK used to address the resource.
func (r APIResource) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: r.Group, Version: r.Version, Kind: r.Kind}
}

// APIVersion renders the resource's apiVersion string, e.g. "ec2.aws.upbound.io/v1beta1".
func (r APIResource) APIVersion() string {
	if r.Group == "" {
		return r.Version
	}
	return r.Group + "/" + r.Version
}

// HasCategory reports whether the resource was tagged with the given category.
func (r APIResource) HasCategory(category string) bool {
	for _, c := range r.Categories {
		if strings.EqualFold(c, category) {
			return true
		}
	}
	return false
}

// Listable reports whether the resource supports the list verb. Subresources
// and a handful of virtual resources do not, and listing them fails.
func (r APIResource) Listable() bool {
	for _, v := range r.Verbs {
		if v == "list" {
			return true
		}
	}
	return false
}

// New builds a Client from a REST configuration.
func New(cfg *rest.Config, defaultNamespace string) (*Client, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot create dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot create discovery client: %w", err)
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot create kubernetes client: %w", err)
	}
	if defaultNamespace == "" {
		defaultNamespace = "default"
	}
	return &Client{
		dynamic:          dyn,
		discovery:        disco,
		core:             core,
		defaultNamespace: defaultNamespace,
	}, nil
}

// NewForClients is used by tests to inject fakes.
func NewForClients(dyn dynamic.Interface, disco discovery.DiscoveryInterface, core kubernetes.Interface, defaultNamespace string) *Client {
	if defaultNamespace == "" {
		defaultNamespace = "default"
	}
	return &Client{dynamic: dyn, discovery: disco, core: core, defaultNamespace: defaultNamespace}
}

// DefaultNamespace returns the namespace namespaced tools fall back to.
func (c *Client) DefaultNamespace() string {
	return c.defaultNamespace
}

// Target returns the name of the cluster this client talks to.
func (c *Client) Target() string {
	return c.target
}

// Core exposes the typed client, used for Deployments, Events and the like.
func (c *Client) Core() kubernetes.Interface {
	return c.core
}

// ServerVersion reports the Kubernetes version of the control plane cluster.
func (c *Client) ServerVersion() (string, error) {
	info, err := c.discovery.ServerVersion()
	if err != nil {
		return "", fmt.Errorf("cannot read kubernetes server version: %w", err)
	}
	return info.GitVersion, nil
}

// Dynamic exposes the underlying dynamic client.
func (c *Client) Dynamic() dynamic.Interface {
	return c.dynamic
}

// APIResources returns every listable resource the API server advertises.
//
// The result is cached for a short while because discovery is expensive and
// several tools need the full picture.
func (c *Client) APIResources(ctx context.Context) ([]APIResource, error) {
	c.mu.Lock()
	if c.apis != nil && time.Since(c.apisAt) < discoveryTTL {
		cached := c.apis
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	lists, err := c.discovery.ServerPreferredResources() //nolint:contextcheck // discovery client has no context-aware variant
	// A single broken aggregated API server makes discovery return a partial
	// result alongside an error. That is common on real clusters, so we keep
	// what we got instead of failing the whole tool call.
	if err != nil && len(lists) == 0 {
		return nil, fmt.Errorf("cannot discover server resources: %w", err)
	}

	resources := make([]APIResource, 0, 256)
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, parseErr := schema.ParseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			continue
		}
		for _, r := range list.APIResources {
			// Skip subresources such as "providers/status".
			if strings.Contains(r.Name, "/") {
				continue
			}
			resources = append(resources, APIResource{
				Group:      gv.Group,
				Version:    gv.Version,
				Kind:       r.Kind,
				Resource:   r.Name,
				Namespaced: r.Namespaced,
				Categories: r.Categories,
				ShortNames: r.ShortNames,
				Verbs:      r.Verbs,
			})
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Group != resources[j].Group {
			return resources[i].Group < resources[j].Group
		}
		return resources[i].Kind < resources[j].Kind
	})

	c.mu.Lock()
	c.apis, c.apisAt = resources, time.Now()
	c.mu.Unlock()
	return resources, nil
}

// ResourcesInCategory returns the listable resources tagged with a category.
func (c *Client) ResourcesInCategory(ctx context.Context, category string) ([]APIResource, error) {
	all, err := c.APIResources(ctx)
	if err != nil {
		return nil, err
	}
	matched := make([]APIResource, 0, 32)
	for _, r := range all {
		if r.HasCategory(category) && r.Listable() {
			matched = append(matched, r)
		}
	}
	return matched, nil
}

// ErrNotFound is returned when a kind cannot be resolved through discovery.
type ErrNotFound struct {
	What string
}

func (e *ErrNotFound) Error() string {
	return e.What + " not found"
}

// ResolveKind maps a user supplied kind, plural name or short name onto a
// discovered resource. The optional group narrows ambiguous matches, which
// happens a lot in Crossplane: "Bucket" exists in both the AWS and GCP
// providers on a cluster running both.
func (c *Client) ResolveKind(ctx context.Context, kind, group string) (APIResource, error) {
	all, err := c.APIResources(ctx)
	if err != nil {
		return APIResource{}, err
	}

	var matches []APIResource
	for _, r := range all {
		if group != "" && !strings.EqualFold(r.Group, group) {
			continue
		}
		if !r.Listable() {
			continue
		}
		if strings.EqualFold(r.Kind, kind) || strings.EqualFold(r.Resource, kind) || hasFold(r.ShortNames, kind) {
			matches = append(matches, r)
		}
	}

	switch len(matches) {
	case 0:
		what := fmt.Sprintf("kind %q", kind)
		if group != "" {
			what = fmt.Sprintf("kind %q in group %q", kind, group)
		}
		return APIResource{}, &ErrNotFound{What: what}
	case 1:
		return matches[0], nil
	default:
		groups := make([]string, 0, len(matches))
		for _, m := range matches {
			groups = append(groups, m.APIVersion())
		}
		return APIResource{}, fmt.Errorf("kind %q is ambiguous, it exists in %s: pass the 'group' argument to disambiguate",
			kind, strings.Join(groups, ", "))
	}
}

// RESTMapper returns a mapper built from the current discovery snapshot. It is
// used to turn the apiVersion/kind pairs found in resource references into
// something the dynamic client can address.
func (c *Client) RESTMapper(ctx context.Context) (meta.RESTMapper, error) {
	c.mu.Lock()
	if c.mapper != nil && time.Since(c.mapperAt) < discoveryTTL {
		cached := c.mapper
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	groups, err := restmapper.GetAPIGroupResources(c.discovery)
	if err != nil && len(groups) == 0 {
		return nil, fmt.Errorf("cannot discover server groups: %w", err)
	}
	built := restmapper.NewDiscoveryRESTMapper(groups)

	c.mu.Lock()
	c.mapper, c.mapperAt = built, time.Now()
	c.mu.Unlock()
	return built, nil
}

// ListOptions narrows a dynamic list request.
type ListOptions struct {
	Namespace     string
	LabelSelector string
	FieldSelector string
	Limit         int64
}

func (o ListOptions) toMeta() metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: o.LabelSelector,
		FieldSelector: o.FieldSelector,
		Limit:         o.Limit,
	}
}

// List returns the objects of a single resource type.
func (c *Client) List(ctx context.Context, r APIResource, opts ListOptions) (*unstructured.UnstructuredList, error) {
	ri := c.resourceInterface(r, opts.Namespace)
	list, err := ri.List(ctx, opts.toMeta())
	if err != nil {
		return nil, fmt.Errorf("cannot list %s: %w", r.Kind, err)
	}
	return list, nil
}

// Get fetches a single object.
func (c *Client) Get(ctx context.Context, r APIResource, namespace, name string) (*unstructured.Unstructured, error) {
	obj, err := c.resourceInterface(r, namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("cannot get %s %q: %w", r.Kind, name, err)
	}
	return obj, nil
}

// GetByReference fetches the object pointed at by an apiVersion/kind/name
// triple, resolving the resource through the REST mapper.
func (c *Client) GetByReference(ctx context.Context, apiVersion, kind, namespace, name string) (*unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid apiVersion %q: %w", apiVersion, err)
	}
	mapper, err := c.RESTMapper(ctx)
	if err != nil {
		return nil, err
	}
	mapping, err := mapper.RESTMapping(gv.WithKind(kind).GroupKind(), gv.Version)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s/%s: %w", apiVersion, kind, err)
	}

	ri := c.dynamic.Resource(mapping.Resource)
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = c.defaultNamespace
		}
		return ri.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	return ri.Get(ctx, name, metav1.GetOptions{})
}

func (c *Client) resourceInterface(r APIResource, namespace string) dynamic.ResourceInterface {
	ri := c.dynamic.Resource(r.GroupVersionResource())
	if !r.Namespaced {
		return ri
	}
	// An empty namespace on a namespaced resource means "across all
	// namespaces", which is almost always what an operator asking an LLM
	// about their control plane wants.
	return ri.Namespace(namespace)
}

func hasFold(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}
