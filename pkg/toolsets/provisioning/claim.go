package provisioning

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

// createdByLabel records which manifests this server wrote, so an operator can
// find them later with a label selector and clean them up.
const createdByLabel = "app.kubernetes.io/created-by"

// namespaceProp is the create-time namespace argument. It is not api.
// NamespaceProp: that one means "where to search", and an omitted namespace on
// a write means the default, not everywhere.
var namespaceProp = api.StringProp("Namespace to create the resource in. Defaults to the server's default " +
	"namespace. Ignored by cluster scoped kinds, which Crossplane v1 composite resources are.")

// request is one instance of a platform API somebody asked for.
type request struct {
	// noun is what the caller thinks they are creating, used in prose.
	noun string
	// hints identify the platform API to look for, most specific first. They
	// are only used when the caller did not name a kind outright.
	hints []string

	name      string
	namespace string
	// kind and apiVersion pin the platform API down when discovery would
	// guess wrong, or when there is nothing to guess from.
	kind       string
	apiVersion string

	// values are the fields to set, each with the names a platform API might
	// plausibly have given it.
	values []field
	// parameters is a free-form spec override from the caller. It wins over
	// values, because it is the caller being explicit.
	parameters map[string]any

	dryRun bool
}

// field is one value to place in a composite's spec, along with the names the
// XRD might have used for it.
type field struct {
	names []string
	value any
}

// provision resolves the platform API, builds a manifest that fits its schema
// and applies it.
func provision(p api.Params, req request) (*api.Result, error) {
	resource, alternatives, err := resolve(p, req)
	if err != nil {
		return api.Error(err), nil
	}

	// The XRD is the only honest source for what the API accepts. Without it
	// we still create something, we are just guessing at field names.
	schema := schemaFor(p, resource)

	spec, ignored := buildSpec(schema, req)
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": resource.APIVersion(),
		"kind":       resource.Kind,
		"metadata": map[string]any{
			"name":   req.name,
			"labels": map[string]any{createdByLabel: crossplane.FieldManager},
		},
		"spec": spec,
	}}
	if resource.Namespaced && req.namespace != "" {
		obj.SetNamespace(req.namespace)
	}

	applied, err := p.Client.Apply(p, obj, crossplane.WriteOptions{DryRun: req.dryRun})
	if err != nil {
		return api.Error(err), nil
	}
	return describe(p, applied, describeOptions{
		noun:         req.noun,
		schema:       schema,
		alternatives: alternatives,
		ignored:      ignored,
	}), nil
}

// resolve finds the kind to create, and reports the other candidates so the
// answer can say what it did not pick.
func resolve(p api.Params, req request) (crossplane.APIResource, []string, error) {
	if req.kind != "" {
		group := ""
		// A core-group apiVersion is a bare version with no slash, and
		// treating "v1" as a group name would match nothing.
		if prefix, _, qualified := strings.Cut(req.apiVersion, "/"); qualified {
			group = prefix
		}
		resource, err := p.Client.ResolveKind(p, req.kind, group)
		return resource, nil, err
	}

	candidates, err := platformAPIs(p)
	if err != nil {
		return crossplane.APIResource{}, nil, err
	}
	if len(candidates) == 0 {
		return crossplane.APIResource{}, nil, fmt.Errorf(
			"this control plane offers no platform APIs: install an XRD and a Composition before asking for a %s, "+
				"or pass 'kind' and 'apiVersion' to create something that already exists", req.noun)
	}

	best, rest := pick(candidates, req.hints)
	if best.Kind == "" {
		return crossplane.APIResource{}, nil, fmt.Errorf(
			"no platform API on this control plane looks like a %s. Available claim and composite kinds: %s. "+
				"Call crossplane_xrds_list to see what each one offers, then pass 'kind' to choose",
			req.noun, strings.Join(names(candidates), ", "))
	}
	return best, rest, nil
}

// platformAPIs returns the kinds a user is meant to create: claims first,
// because on a v1 control plane they are the namespaced front door, then
// composite resources, which is all a v2 control plane has.
func platformAPIs(p api.Params) ([]crossplane.APIResource, error) {
	var candidates []crossplane.APIResource
	for _, category := range []string{crossplane.CategoryClaim, crossplane.CategoryComposite} {
		found, err := p.Client.ResourcesInCategory(p, category)
		if err != nil {
			return nil, fmt.Errorf("cannot look up the platform APIs of this control plane: %w", err)
		}
		candidates = append(candidates, found...)
	}
	return candidates, nil
}

// pick scores the candidates against the hints and returns the winner plus the
// names of anything else that matched.
//
// Hints are ordered most specific first, and earlier hints are worth more: a
// kind called PostgreSQLInstance should beat one called Database when somebody
// asks for Postgres, and the other way round when they do not.
func pick(candidates []crossplane.APIResource, hints []string) (crossplane.APIResource, []string) {
	type scored struct {
		resource crossplane.APIResource
		score    int
	}

	ranked := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		if score := score(candidate.Kind, hints); score > 0 {
			ranked = append(ranked, scored{resource: candidate, score: score})
		}
	}
	if len(ranked) == 0 {
		return crossplane.APIResource{}, nil
	}

	// A stable sort keeps the claim-before-composite order for ties.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	others := make([]string, 0, len(ranked)-1)
	for _, entry := range ranked[1:] {
		others = append(others, entry.resource.Kind+" ("+entry.resource.APIVersion()+")")
	}
	return ranked[0].resource, others
}

func score(kind string, hints []string) int {
	lowered := strings.ToLower(kind)
	total := 0
	for i, hint := range hints {
		weight := len(hints) - i
		switch {
		// An exact match counts double, not ten times over: a kind called
		// Database should lose to PostgreSQLInstance, which matches several
		// more specific hints, and win against DatabaseInstance, which
		// matches the same one less exactly.
		case lowered == hint:
			total += weight * 2
		case strings.Contains(lowered, hint):
			total += weight
		}
	}
	return total
}

func names(resources []crossplane.APIResource) []string {
	out := make([]string, 0, len(resources))
	for _, r := range resources {
		out = append(out, r.Kind+" ("+r.APIVersion()+")")
	}
	sort.Strings(out)
	return out
}

// schemaFor finds the XRD that defines a kind and reads its spec schema.
//
// It is best effort on purpose. A control plane where XRDs cannot be listed,
// or a kind that comes from somewhere else entirely, should still be usable;
// the caller just loses the field checking.
func schemaFor(p api.Params, resource crossplane.APIResource) *crossplane.XRDSchema {
	xrds, err := p.Client.XRDs(p)
	if err != nil {
		return nil
	}
	for _, xrd := range xrds {
		if xrd.Group != resource.Group {
			continue
		}
		if xrd.ClaimKind != resource.Kind && xrd.CompositeKind != resource.Kind {
			continue
		}
		schema, err := p.Client.XRDSchemaFor(p, xrd.Name, resource.Version)
		if err != nil {
			return nil
		}
		return schema
	}
	return nil
}

// buildSpec places the requested values where the platform API declares them.
//
// XRDs vary: some put user input under spec.parameters, others at the top
// level, and the names differ between platform teams. Rather than guessing and
// having the API server silently prune what it does not recognise, read the
// schema and only set fields it actually declares. Anything with nowhere to go
// is reported instead of dropped.
func buildSpec(schema *crossplane.XRDSchema, req request) (map[string]any, []string) {
	spec := map[string]any{}
	properties := specProperties(schema)
	parameters := childProperties(properties, "parameters")

	var ignored []string
	for _, f := range req.values {
		if f.value == nil {
			continue
		}
		if !place(spec, properties, parameters, f) {
			ignored = append(ignored, f.names[0])
		}
	}

	// An explicit parameter is the caller telling us they know the API better
	// than we do, so it is set even when the schema does not mention it.
	for key, value := range req.parameters {
		if !place(spec, properties, parameters, field{names: []string{key}, value: value}) {
			setParameter(spec, key, value)
		}
	}
	return spec, ignored
}

// place writes a value at the first name the schema recognises, preferring
// spec.parameters because that is where most XRDs put user input.
func place(spec, properties, parameters map[string]any, f field) bool {
	for _, name := range f.names {
		if property, ok := parameters[name]; ok {
			setParameter(spec, name, coerce(property, f.value))
			return true
		}
		if property, ok := properties[name]; ok {
			spec[name] = coerce(property, f.value)
			return true
		}
	}
	// Without a schema, follow the convention the Crossplane documentation
	// uses and let the API server have the last word.
	if properties == nil {
		setParameter(spec, f.names[0], f.value)
		return true
	}
	return false
}

func setParameter(spec map[string]any, name string, value any) {
	parameters, ok := spec["parameters"].(map[string]any)
	if !ok {
		parameters = map[string]any{}
		spec["parameters"] = parameters
	}
	parameters[name] = value
}

func specProperties(schema *crossplane.XRDSchema) map[string]any {
	if schema == nil {
		return nil
	}
	properties, ok := schema.Spec["properties"].(map[string]any)
	if !ok {
		return nil
	}
	return properties
}

func childProperties(properties map[string]any, name string) map[string]any {
	child, ok := properties[name].(map[string]any)
	if !ok {
		return nil
	}
	nested, ok := child["properties"].(map[string]any)
	if !ok {
		return nil
	}
	return nested
}

// coerce converts a value to the type the schema declares. Platform teams
// disagree about whether a size is 20 or "20Gi", and a type mismatch is
// rejected by the API server for no good reason when the fix is this small.
func coerce(property any, value any) any {
	declared, _ := property.(map[string]any)
	wanted, _ := declared["type"].(string)

	switch wanted {
	case "string":
		if n, ok := value.(int); ok {
			return strconv.Itoa(n)
		}
	case "integer", "number":
		if s, ok := value.(string); ok {
			if n, err := strconv.Atoi(s); err == nil {
				return n
			}
		}
	}
	return value
}

// describeOptions carries the prose bits of an answer that only the calling
// tool knows about.
type describeOptions struct {
	noun         string
	schema       *crossplane.XRDSchema
	alternatives []string
	ignored      []string
}

// describe renders what a write did, and what to call next. A created resource
// is not a working one: Crossplane has only just started.
func describe(p api.Params, applied *crossplane.ApplyResult, opts describeOptions) *api.Result {
	obj := applied.Object
	manifest, err := crossplane.Manifest(obj)
	if err != nil {
		return api.Error(err)
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%s %s %s on %s.",
		capitalise(applied.Action()), obj.GetKind(), api.Path(obj.GetNamespace(), obj.GetName()), p.Client.Target())
	if applied.DryRun {
		text.WriteString("\nNothing was persisted: this was a dry run.")
	}
	if opts.schema == nil {
		text.WriteString("\nThe XRD for this kind could not be read, so the fields below are a best guess.")
	}

	api.Section(&text, "Manifest:", manifest)

	if len(opts.ignored) > 0 {
		api.Section(&text, "Not set, the API declares no field for them:", "- "+strings.Join(opts.ignored, "\n- "))
	}
	if len(opts.alternatives) > 0 {
		api.Section(&text, fmt.Sprintf("Other platform APIs that could have served this %s:", opts.noun),
			"- "+strings.Join(opts.alternatives, "\n- ")+"\nPass 'kind' to pick one of them instead.")
	}
	if !applied.DryRun {
		api.Section(&text, "Next:", fmt.Sprintf(
			"Provisioning is asynchronous. Call crossplane_resource_tree with %s to watch it come up.",
			arguments(obj)))
	}

	return api.Structured(text.String(), map[string]any{
		"action":     applied.Action(),
		"cluster":    p.Client.Target(),
		"dryRun":     applied.DryRun,
		"apiVersion": obj.GetAPIVersion(),
		"kind":       obj.GetKind(),
		"name":       obj.GetName(),
		"namespace":  obj.GetNamespace(),
		"manifest":   manifest,
		"ignored":    opts.ignored,
		"resource":   crossplane.Summarize(obj),
	})
}

// arguments renders the JSON a follow-up tool call needs, which saves the
// model from reassembling it out of the prose.
func arguments(obj *unstructured.Unstructured) string {
	args := fmt.Sprintf(`{"kind":%q,"name":%q`, obj.GetKind(), obj.GetName())
	if namespace := obj.GetNamespace(); namespace != "" {
		args += fmt.Sprintf(`,"namespace":%q`, namespace)
	}
	return args + "}"
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
