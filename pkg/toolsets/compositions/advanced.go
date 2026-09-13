package compositions

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"sigs.k8s.io/yaml"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func advancedTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_xrd_schema",
			Title: "XRD: schema",
			Description: "Show the API contract of a platform API: the exact apiVersion and kind to use, the " +
				"shape of spec, which fields are required, and a minimal example manifest. Read this before " +
				"writing a composite resource or a claim, and before calling crossplane_composition_render, " +
				"rather than guessing the fields.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"name": api.StringProp("Name of the XRD, for example 'xpostgresqlinstances.example.org'. " +
					"Use crossplane_xrds_list to find it."),
				"version": api.StringProp("Which version of the API to describe. " +
					"Defaults to the first served version."),
			}, "name"),
			Handler: xrdSchema,
		},
		{
			Name:  "crossplane_composition_validate",
			Title: "Composition: validate",
			Description: "Check whether a Composition can work on this control plane: that the composite kind " +
				"it claims to satisfy exists, that every function its pipeline calls is installed and Healthy, " +
				"and that its step names are unique. This is a static check that needs no container runtime, " +
				"so prefer it over crossplane_composition_render when the question is 'why does this " +
				"Composition not work?' rather than 'what does it produce?'.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"name": api.StringProp("Name of the Composition to validate."),
			}, "name"),
			Handler: compositionValidate,
		},
		{
			Name:  "crossplane_composition_render",
			Title: "Composition: render (dry run)",
			Description: "Run a Composition's function pipeline against a composite resource and return the " +
				"resources it would create, without touching the control plane. This is the equivalent of " +
				"'crossplane render' and answers 'what would this Composition actually produce?'. The " +
				"Composition and its functions are read from the live control plane, so the result reflects " +
				"this cluster. Requires the crossplane CLI and a container runtime on the machine running " +
				"this server; use crossplane_composition_validate instead if rendering is unavailable.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"composition": api.StringProp("Name of the Composition to render."),
				"xr": api.StringProp("YAML of the composite resource to render, including apiVersion, kind, " +
					"metadata.name and spec. Call crossplane_xrd_schema first to learn the required fields."),
				"manifests": api.BoolProp("Include the full rendered YAML. Defaults to true. " +
					"Set false to get only the summary of what would be created."),
			}, "composition", "xr"),
			Handler: compositionRender,
		},
	}
}

func xrdSchema(p api.Params) (*api.Result, error) {
	name := p.Args.String("name")
	version := p.Args.OptionalString("version", "")
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	schema, err := p.Client.XRDSchemaFor(p, name, version)
	if err != nil {
		return api.Error(err), nil
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%s\napiVersion: %s\ncomposite kind: %s",
		schema.Name, schema.APIVersion, schema.CompositeKind)
	if schema.ClaimKind != "" {
		fmt.Fprintf(&text, "\nclaim kind: %s", schema.ClaimKind)
	}
	if schema.Scope != "" {
		fmt.Fprintf(&text, "\nscope: %s", schema.Scope)
	}
	if len(schema.Required) > 0 {
		fmt.Fprintf(&text, "\nrequired spec fields: %s", strings.Join(schema.Required, ", "))
	}

	if schema.Spec != nil {
		encoded, err := yaml.Marshal(schema.Spec)
		if err != nil {
			return api.Error(fmt.Errorf("cannot render the schema: %w", err)), nil
		}
		api.Section(&text, "spec schema:", string(encoded))
	}
	api.Section(&text, "Example:", schema.Example)

	return api.Structured(text.String(), schema), nil
}

func compositionValidate(p api.Params) (*api.Result, error) {
	name := p.Args.String("name")
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	validation, err := p.Client.ValidateComposition(p, name)
	if err != nil {
		return api.Error(err), nil
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Composition %s satisfies %s, mode %s.",
		validation.Composition, api.OrDash(validation.CompositeKind), api.OrDash(validation.Mode))
	if len(validation.Functions) > 0 {
		fmt.Fprintf(&text, "\nPipeline: %s", strings.Join(validation.Functions, " -> "))
	}

	if len(validation.Findings) == 0 {
		text.WriteString("\n\nNo problems found.")
		return api.Structured(text.String(), validation), nil
	}

	rows := make([][]string, 0, len(validation.Findings))
	for _, finding := range validation.Findings {
		rows = append(rows, []string{strings.ToUpper(finding.Severity), finding.Message})
	}
	api.Section(&text, fmt.Sprintf("%d finding(s):", len(validation.Findings)),
		api.Table([]string{"SEVERITY", "PROBLEM"}, rows))

	return api.Structured(text.String(), validation), nil
}

func compositionRender(p api.Params) (*api.Result, error) {
	composition := p.Args.String("composition")
	composite := p.Args.String("xr")
	includeManifests := p.Args.OptionalBool("manifests", true)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	rendered, err := p.Client.Render(p, composition, composite)
	switch {
	case errors.Is(err, crossplane.ErrRenderUnavailable):
		return api.Errorf("%w. Use crossplane_composition_validate to check the Composition statically instead",
			err), nil
	case err != nil:
		return api.Error(err), nil
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Rendered %s through %d function(s): %s.\n",
		rendered.Composition, len(rendered.Functions), strings.Join(rendered.Functions, " -> "))

	rows := make([][]string, 0, len(rendered.Resources))
	for _, resource := range rendered.Resources {
		rows = append(rows, []string{resource.Kind, resource.APIVersion, resource.Name, api.OrDash(resource.ComposedBy)})
	}
	fmt.Fprintf(&text, "%d resource(s) would be created.\n", len(rendered.Resources))
	text.WriteString(api.Table([]string{"KIND", "APIVERSION", "NAME", "COMPOSED AS"}, rows))

	if rendered.Warnings != "" {
		api.Section(&text, "Warnings:", rendered.Warnings)
	}
	if includeManifests {
		api.Section(&text, "Rendered manifests:", rendered.Manifests)
	}

	return api.Structured(text.String(), rendered), nil
}
