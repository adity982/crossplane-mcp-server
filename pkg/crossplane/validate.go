package crossplane

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ValidationFinding is one problem found in a Composition.
type ValidationFinding struct {
	// Severity is "error" for something that stops the Composition working,
	// and "warning" for something that merely looks wrong.
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// CompositionValidation reports whether a Composition can actually work on
// this control plane.
type CompositionValidation struct {
	Composition   string              `json:"composition"`
	CompositeKind string              `json:"compositeKind"`
	Mode          string              `json:"mode"`
	Functions     []string            `json:"functions,omitempty"`
	Findings      []ValidationFinding `json:"findings"`
	Valid         bool                `json:"valid"`
}

// ValidateComposition checks a Composition against the live control plane.
//
// This is a static check, not a render: it answers "could this Composition
// work here?" rather than "what would it produce?". It needs no container
// runtime, so it works in places rendering does not.
func (c *Client) ValidateComposition(ctx context.Context, name string) (*CompositionValidation, error) {
	resource, err := c.ResolveKind(ctx, "Composition", GroupAPIExtensions)
	if err != nil {
		return nil, err
	}
	obj, err := c.Get(ctx, resource, "", name)
	if err != nil {
		return nil, err
	}

	result := &CompositionValidation{
		Composition:   obj.GetName(),
		CompositeKind: nestedString(obj, "spec", "compositeTypeRef", "kind"),
		Mode:          nestedString(obj, "spec", "mode"),
		Findings:      []ValidationFinding{},
	}
	compositeAPIVersion := nestedString(obj, "spec", "compositeTypeRef", "apiVersion")

	result.checkCompositeType(ctx, c, compositeAPIVersion)
	result.checkPipeline(ctx, c, obj)

	result.Valid = true
	for _, finding := range result.Findings {
		if finding.Severity == "error" {
			result.Valid = false
		}
	}
	return result, nil
}

// checkCompositeType confirms the kind this Composition claims to satisfy
// actually exists. A typo here makes the Composition silently never match.
func (v *CompositionValidation) checkCompositeType(ctx context.Context, c *Client, apiVersion string) {
	if v.CompositeKind == "" || apiVersion == "" {
		v.fail("spec.compositeTypeRef is incomplete, so this Composition can never be selected")
		return
	}

	group := apiVersion
	if slash := strings.Index(apiVersion, "/"); slash >= 0 {
		group = apiVersion[:slash]
	}

	found, err := c.ResolveKind(ctx, v.CompositeKind, group)
	if err != nil {
		v.fail(fmt.Sprintf("compositeTypeRef names %s/%s, which is not installed on this control plane: %v",
			apiVersion, v.CompositeKind, err))
		return
	}
	if !found.HasCategory(CategoryComposite) {
		v.warn(fmt.Sprintf("%s/%s exists but is not a composite resource, so no XRD defines it",
			apiVersion, v.CompositeKind))
	}
}

// checkPipeline confirms every referenced function is installed and healthy,
// and that the step names are unique as Crossplane requires.
func (v *CompositionValidation) checkPipeline(ctx context.Context, c *Client, obj *unstructured.Unstructured) {
	steps := nestedSlice(obj, "spec", "pipeline")
	if len(steps) == 0 {
		if v.Mode == "Pipeline" {
			v.fail("mode is Pipeline but spec.pipeline is empty, so nothing will be composed")
		}
		return
	}

	installed, err := c.Packages(ctx, PackageFunction)
	if err != nil {
		v.warn("could not check the functions: " + err.Error())
		return
	}
	byName := make(map[string]Package, len(installed))
	for _, function := range installed {
		byName[function.Name] = function
	}

	seen := map[string]bool{}
	for _, item := range steps {
		step, ok := item.(map[string]any)
		if !ok {
			continue
		}

		name := stringField(step, "step")
		switch {
		case name == "":
			v.fail("a pipeline step has no name")
		case seen[name]:
			v.fail(fmt.Sprintf("pipeline step %q is defined more than once, step names must be unique", name))
		}
		seen[name] = true

		ref, _ := step["functionRef"].(map[string]any)
		function := stringField(ref, "name")
		if function == "" {
			v.fail(fmt.Sprintf("pipeline step %q has no functionRef", name))
			continue
		}
		v.Functions = append(v.Functions, function)

		found, installed := byName[function]
		switch {
		case !installed:
			v.fail(fmt.Sprintf("pipeline step %q calls function %q, which is not installed", name, function))
		case found.Healthy != StatusTrue:
			v.fail(fmt.Sprintf("pipeline step %q calls function %q, which is installed but not Healthy: %s",
				name, function, orUnknown(found.Message)))
		}
	}
}

func (v *CompositionValidation) fail(message string) {
	v.Findings = append(v.Findings, ValidationFinding{Severity: "error", Message: message})
}

func (v *CompositionValidation) warn(message string) {
	v.Findings = append(v.Findings, ValidationFinding{Severity: "warning", Message: message})
}

func orUnknown(message string) string {
	if message == "" {
		return "no reason reported"
	}
	return message
}
