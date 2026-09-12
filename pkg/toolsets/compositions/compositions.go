package compositions

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"sigs.k8s.io/yaml"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func compositionTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_xrds_list",
			Title: "XRDs: list",
			Description: "List the CompositeResourceDefinitions installed on the control plane. An XRD defines " +
				"one of the APIs your platform offers, so this answers 'what can developers ask this control " +
				"plane for?' and shows which composite and claim kinds each XRD generates.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"unhealthyOnly": api.BoolProp("Return only XRDs that are not fully established and offered."),
			}),
			Handler: xrdsList,
		},
		{
			Name:  "crossplane_compositions_list",
			Title: "Compositions: list",
			Description: "List the Compositions installed on the control plane with the composite kind each one " +
				"satisfies and the function pipeline it runs. Use 'compositeKind' to find every Composition " +
				"that could satisfy a particular composite resource.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"compositeKind": api.StringProp("Only return Compositions for this composite kind, " +
					"for example 'XPostgreSQLInstance'."),
			}),
			Handler: compositionsList,
		},
		{
			Name:  "crossplane_composition_get",
			Title: "Composition: describe",
			Description: "Describe a single Composition, including its full YAML definition. Read this when you " +
				"need to know exactly which resources a Composition creates, or which function steps it runs " +
				"and in what order.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"name":     api.StringProp("Name of the Composition."),
				"manifest": api.BoolProp("Include the full YAML manifest. Defaults to true."),
			}, "name"),
			Handler: compositionGet,
		},
	}
}

func xrdsList(p api.Params) (*api.Result, error) {
	unhealthyOnly := p.Args.OptionalBool("unhealthyOnly", false)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	xrds, err := p.Client.XRDs(p)
	if err != nil {
		return api.Error(err), nil
	}

	kept := make([]crossplane.CompositeResourceDefinition, 0, len(xrds))
	for _, xrd := range xrds {
		if unhealthyOnly && xrd.Healthy() {
			continue
		}
		kept = append(kept, xrd)
	}
	if len(kept) == 0 {
		message := "No CompositeResourceDefinitions are installed."
		if unhealthyOnly {
			message = fmt.Sprintf("All %d CompositeResourceDefinitions are healthy.", len(xrds))
		}
		return api.Structured(message, map[string]any{"count": 0, "xrds": kept}), nil
	}

	rows := make([][]string, 0, len(kept))
	for _, xrd := range kept {
		rows = append(rows, []string{
			xrd.Name,
			xrd.Group,
			xrd.CompositeKind,
			orDash(xrd.ClaimKind),
			orDash(strings.Join(xrd.Versions, ",")),
			xrd.Ready,
			xrd.Age,
			orDash(xrd.Message),
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d CompositeResourceDefinition(s).\n", len(kept))
	text.WriteString(api.Table(
		[]string{"NAME", "GROUP", "COMPOSITE", "CLAIM", "VERSIONS", "ESTABLISHED", "AGE", "MESSAGE"}, rows))

	return api.Structured(text.String(), map[string]any{"count": len(kept), "xrds": kept}), nil
}

func compositionsList(p api.Params) (*api.Result, error) {
	compositeKind := p.Args.OptionalString("compositeKind", "")
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	found, err := p.Client.Compositions(p, compositeKind)
	if err != nil {
		return api.Error(err), nil
	}
	if len(found) == 0 {
		if compositeKind != "" {
			return api.Structured(fmt.Sprintf("No Compositions satisfy composite kind %q.", compositeKind),
				map[string]any{"count": 0, "compositions": found}), nil
		}
		return api.Structured("No Compositions are installed.",
			map[string]any{"count": 0, "compositions": found}), nil
	}

	rows := make([][]string, 0, len(found))
	for _, c := range found {
		detail := fmt.Sprintf("%d resource(s)", c.Resources)
		if len(c.Pipeline) > 0 {
			detail = strings.Join(c.Pipeline, " -> ")
		}
		rows = append(rows, []string{c.Name, c.CompositeKind, c.CompositeAPIVersion, c.Mode, detail, c.Age})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d Composition(s).\n", len(found))
	text.WriteString(api.Table([]string{"NAME", "COMPOSITE", "APIVERSION", "MODE", "PIPELINE", "AGE"}, rows))

	return api.Structured(text.String(), map[string]any{"count": len(found), "compositions": found}), nil
}

func compositionGet(p api.Params) (*api.Result, error) {
	name := p.Args.String("name")
	includeManifest := p.Args.OptionalBool("manifest", true)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	resource, err := p.Client.ResolveKind(p, "Composition", crossplane.GroupAPIExtensions)
	if err != nil {
		return api.Error(err), nil
	}
	obj, err := p.Client.Get(p, resource, "", name)
	if err != nil {
		return api.Error(err), nil
	}

	summary := crossplane.Summarize(obj)
	payload := map[string]any{
		"name":       summary.Name,
		"age":        summary.Age,
		"conditions": summary.Conditions,
		"labels":     obj.GetLabels(),
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Composition %s\nage: %s", summary.Name, summary.Age)

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

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
