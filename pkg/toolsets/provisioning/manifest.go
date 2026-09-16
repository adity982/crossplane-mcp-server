package provisioning

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func manifestTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_resource_apply",
			Title: "Resource: apply a manifest",
			Description: "Apply a Crossplane manifest, creating it or updating it in place. Accepts YAML or JSON, " +
				"and several documents separated by '---'. Use this for the things the other tools do not cover: " +
				"XRDs, Compositions, EnvironmentConfigs, or a composite resource whose spec you built yourself. " +
				"Only Crossplane kinds are accepted, so this cannot be used to write Secrets, RBAC or Deployments. " +
				"Pass 'dryRun' first to have the API server validate the manifest without persisting it.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"manifest": api.StringProp("The manifest to apply, as YAML or JSON. Several documents may be " +
					"separated by '---'; they are applied in order."),
				"namespace": api.StringProp("Namespace for documents that do not name one. " +
					"Defaults to the server's default namespace."),
				"force": api.BoolProp("Take ownership of fields another controller or user already owns. " +
					"Without it, applying over somebody else's field is reported as a conflict. Defaults to false."),
				"dryRun": api.DryRunProp,
			}, "manifest"),
			Write:   true,
			Handler: resourceApply,
		},
	}
}

func resourceApply(p api.Params) (*api.Result, error) {
	manifest := p.Args.String("manifest")
	namespace := p.Args.OptionalString("namespace", "")
	options := crossplane.WriteOptions{
		Force:  p.Args.OptionalBool("force", false),
		DryRun: p.Args.OptionalBool("dryRun", false),
	}
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	documents, err := parse(manifest, namespace)
	if err != nil {
		return api.Error(err), nil
	}

	rows := make([][]string, 0, len(documents))
	payload := make([]map[string]any, 0, len(documents))
	for _, obj := range documents {
		applied, err := p.Client.Apply(p, obj, options)
		if err != nil {
			// Reporting what already went in matters: a half-applied set of
			// documents is a different situation to a rejected one.
			return api.Error(fmt.Errorf("%w. %s", err, progress(rows))), nil
		}
		rows = append(rows, []string{applied.Action(), applied.Object.GetKind(),
			api.OrDash(applied.Object.GetNamespace()), applied.Object.GetName()})
		payload = append(payload, map[string]any{
			"action":     applied.Action(),
			"apiVersion": applied.Object.GetAPIVersion(),
			"kind":       applied.Object.GetKind(),
			"name":       applied.Object.GetName(),
			"namespace":  applied.Object.GetNamespace(),
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Applied %d document(s) to %s.", len(documents), p.Client.Target())
	if options.DryRun {
		text.WriteString("\nNothing was persisted: this was a dry run.")
	}
	api.Section(&text, "Documents:", api.Table([]string{"ACTION", "KIND", "NAMESPACE", "NAME"}, rows))

	return api.Structured(text.String(), map[string]any{
		"cluster":   p.Client.Target(),
		"dryRun":    options.DryRun,
		"count":     len(payload),
		"resources": payload,
	}), nil
}

// parse splits a manifest into objects, rejecting anything that could not be
// applied before the first write goes out.
func parse(manifest, namespace string) ([]*unstructured.Unstructured, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)

	var documents []*unstructured.Unstructured
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cannot parse the manifest as YAML or JSON: %w", err)
		}
		// A document that is only comments decodes to nothing.
		if len(obj.Object) == 0 {
			continue
		}
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" || obj.GetName() == "" {
			return nil, fmt.Errorf("document %d needs apiVersion, kind and metadata.name", len(documents)+1)
		}
		if obj.GetNamespace() == "" && namespace != "" {
			obj.SetNamespace(namespace)
		}
		documents = append(documents, obj)
	}

	if len(documents) == 0 {
		return nil, fmt.Errorf("the manifest contains no documents")
	}
	return documents, nil
}

// progress summarises the documents that made it in before an error, so the
// model knows whether to retry the whole manifest or only the rest of it.
func progress(rows [][]string) string {
	if len(rows) == 0 {
		return "Nothing was applied."
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row[1]+" "+row[3])
	}
	return "Already applied before the failure: " + strings.Join(names, ", ")
}
