package crossplane

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The API groups Crossplane owns. These are stable across the v1 and v2 lines
// and are the one place in this package where we hard-code knowledge of the
// Crossplane API surface.
const (
	GroupPkg           = "pkg.crossplane.io"
	GroupAPIExtensions = "apiextensions.crossplane.io"
)

// PackageKind identifies one of the three Crossplane package types.
type PackageKind string

// The Crossplane package kinds.
const (
	PackageProvider      PackageKind = "Provider"
	PackageFunction      PackageKind = "Function"
	PackageConfiguration PackageKind = "Configuration"
)

// RevisionKind returns the revision kind that accompanies a package kind.
func (k PackageKind) RevisionKind() string { return string(k) + "Revision" }

// Package is the summarised view of an installed Crossplane package.
type Package struct {
	Summary `json:",inline"`

	// Package is the OCI reference the package was installed from.
	Package string `json:"package"`
	// CurrentRevision is the name of the active revision.
	CurrentRevision string `json:"currentRevision,omitempty"`
	// Installed and Healthy mirror the two conditions packages report. They
	// are duplicated out of Summary for readability of the JSON payload.
	Installed string `json:"installed"`
	Healthy   string `json:"healthy"`
	// RevisionActivationPolicy is Automatic or Manual.
	RevisionActivationPolicy string `json:"revisionActivationPolicy,omitempty"`
}

// Packages lists the installed packages of a given kind.
func (c *Client) Packages(ctx context.Context, kind PackageKind) ([]Package, error) {
	resource, err := c.ResolveKind(ctx, string(kind), GroupPkg)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s: is Crossplane installed? %w", kind, err)
	}

	list, err := c.List(ctx, resource, ListOptions{})
	if err != nil {
		return nil, err
	}

	packages := make([]Package, 0, len(list.Items))
	for i := range list.Items {
		packages = append(packages, summarizePackageObject(&list.Items[i]))
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	return packages, nil
}

func summarizePackageObject(obj *unstructured.Unstructured) Package {
	summary := SummarizePackage(obj)
	conditions := summary.Conditions

	pkg := Package{
		Summary:                  summary,
		Package:                  nestedString(obj, "spec", "package"),
		CurrentRevision:          nestedString(obj, "status", "currentRevision"),
		Installed:                conditionStatus(conditions, TypeInstalled),
		Healthy:                  conditionStatus(conditions, TypeHealthy),
		RevisionActivationPolicy: nestedString(obj, "spec", "revisionActivationPolicy"),
	}
	return pkg
}

// Revisions lists the revisions belonging to a package. Revisions are where
// the interesting failure detail lives: an image pull failure or a missing
// dependency shows up on the revision, not on the package.
func (c *Client) Revisions(ctx context.Context, kind PackageKind, packageName string) ([]Summary, error) {
	resource, err := c.ResolveKind(ctx, kind.RevisionKind(), GroupPkg)
	if err != nil {
		return nil, err
	}
	list, err := c.List(ctx, resource, ListOptions{})
	if err != nil {
		return nil, err
	}

	revisions := make([]Summary, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		if packageName != "" && !ownedBy(obj, packageName) {
			continue
		}
		s := SummarizePackage(obj)
		revisions = append(revisions, s)
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Name < revisions[j].Name })
	return revisions, nil
}

// CompositeResourceDefinition is the summarised view of an XRD plus the CRDs
// it generates, which is the mapping users most often need when they ask
// "what APIs does my platform offer?".
type CompositeResourceDefinition struct {
	Summary `json:",inline"`

	Group          string   `json:"group"`
	CompositeKind  string   `json:"compositeKind"`
	ClaimKind      string   `json:"claimKind,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	Versions       []string `json:"versions"`
	ServedVersions []string `json:"servedVersions,omitempty"`
}

// XRDs lists the CompositeResourceDefinitions installed on the control plane.
func (c *Client) XRDs(ctx context.Context) ([]CompositeResourceDefinition, error) {
	resource, err := c.ResolveKind(ctx, "CompositeResourceDefinition", GroupAPIExtensions)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve CompositeResourceDefinition: is Crossplane installed? %w", err)
	}
	list, err := c.List(ctx, resource, ListOptions{})
	if err != nil {
		return nil, err
	}

	xrds := make([]CompositeResourceDefinition, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		xrd := CompositeResourceDefinition{
			Summary:       Summarize(obj),
			Group:         nestedString(obj, "spec", "group"),
			CompositeKind: nestedString(obj, "spec", "names", "kind"),
			ClaimKind:     nestedString(obj, "spec", "claimNames", "kind"),
			Scope:         nestedString(obj, "spec", "scope"),
		}
		// Crossplane reports Established/Offered rather than Ready on XRDs, so
		// surface whichever it publishes instead of a bare "-".
		if xrd.Ready == "-" {
			xrd.Ready = conditionStatus(xrd.Conditions, "Established")
		}
		for _, version := range nestedSlice(obj, "spec", "versions") {
			entry, ok := version.(map[string]any)
			if !ok {
				continue
			}
			name := stringField(entry, "name")
			xrd.Versions = append(xrd.Versions, name)
			if served, ok := entry["served"].(bool); ok && served {
				xrd.ServedVersions = append(xrd.ServedVersions, name)
			}
		}
		xrds = append(xrds, xrd)
	}
	sort.Slice(xrds, func(i, j int) bool { return xrds[i].Name < xrds[j].Name })
	return xrds, nil
}

// Composition is the summarised view of a Composition.
type Composition struct {
	Summary `json:",inline"`

	CompositeAPIVersion string `json:"compositeApiVersion"`
	CompositeKind       string `json:"compositeKind"`
	Mode                string `json:"mode"`
	// Pipeline lists the function step names for pipeline mode compositions.
	Pipeline []string `json:"pipeline,omitempty"`
	// Functions lists the functions referenced by the pipeline.
	Functions []string `json:"functions,omitempty"`
	// Resources counts the entries of a legacy Resources mode composition.
	Resources int `json:"resources,omitempty"`
}

// Compositions lists the Compositions installed on the control plane,
// optionally restricted to those that satisfy a given composite kind.
func (c *Client) Compositions(ctx context.Context, compositeKind string) ([]Composition, error) {
	resource, err := c.ResolveKind(ctx, "Composition", GroupAPIExtensions)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve Composition: is Crossplane installed? %w", err)
	}
	list, err := c.List(ctx, resource, ListOptions{})
	if err != nil {
		return nil, err
	}

	compositions := make([]Composition, 0, len(list.Items))
	for i := range list.Items {
		obj := &list.Items[i]
		composition := Composition{
			Summary:             Summarize(obj),
			CompositeAPIVersion: nestedString(obj, "spec", "compositeTypeRef", "apiVersion"),
			CompositeKind:       nestedString(obj, "spec", "compositeTypeRef", "kind"),
			Mode:                nestedString(obj, "spec", "mode"),
		}
		if compositeKind != "" && !strings.EqualFold(composition.CompositeKind, compositeKind) {
			continue
		}
		for _, step := range nestedSlice(obj, "spec", "pipeline") {
			entry, ok := step.(map[string]any)
			if !ok {
				continue
			}
			composition.Pipeline = append(composition.Pipeline, stringField(entry, "step"))
			if ref, ok := entry["functionRef"].(map[string]any); ok {
				composition.Functions = append(composition.Functions, stringField(ref, "name"))
			}
		}
		composition.Resources = len(nestedSlice(obj, "spec", "resources"))
		if composition.Mode == "" {
			// Crossplane v1 defaulted to Resources mode; v2 defaults to
			// Pipeline. Infer from the body rather than guessing.
			if len(composition.Pipeline) > 0 {
				composition.Mode = "Pipeline"
			} else {
				composition.Mode = "Resources"
			}
		}
		compositions = append(compositions, composition)
	}
	sort.Slice(compositions, func(i, j int) bool { return compositions[i].Name < compositions[j].Name })
	return compositions, nil
}

// CrossplaneVersion reports the version of the Crossplane core deployment, by
// reading the image tag of the crossplane Deployment.
func (c *Client) CrossplaneVersion(ctx context.Context) (namespace, version string, err error) {
	deployments, err := c.Core().AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", "", fmt.Errorf("cannot list deployments: %w", err)
	}
	for i := range deployments.Items {
		d := deployments.Items[i]
		if d.Labels["app"] != "crossplane" && d.Name != "crossplane" {
			continue
		}
		for _, container := range d.Spec.Template.Spec.Containers {
			if container.Name != "crossplane" && !containsFold(container.Image, "crossplane") {
				continue
			}
			return d.Namespace, imageTag(container.Image), nil
		}
	}
	return "", "", &ErrNotFound{What: "crossplane core deployment"}
}
