package resources

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

// pausedAnnotation stops Crossplane reconciling a resource. Drift on a paused
// resource is never corrected, which makes it far more interesting than drift
// on a resource the provider is still actively reconciling.
const pausedAnnotation = "crossplane.io/paused"

// maxDriftFields caps the fields reported per resource. A provider that
// late-initialises a large spec can report hundreds of differences.
const maxDriftFields = 25

func driftTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_drift_detect",
			Title: "Managed resources: detect drift",
			Description: "Compare what you asked for against what the provider reports is actually there. For " +
				"each managed resource this diffs spec.forProvider, the desired state, against " +
				"status.atProvider, the observed state, and reports the fields that do not match. It also " +
				"flags resources where drift will never be corrected: ones that are paused, ones whose " +
				"management policies stop Crossplane writing, and ones whose Synced condition is False so the " +
				"provider cannot apply your spec at all. Use this to answer 'has anyone changed my " +
				"infrastructure outside Crossplane?'.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"kind":          api.StringProp("Only inspect this managed resource kind, for example 'Bucket'. Omit to inspect every managed resource."),
				"group":         api.GroupProp,
				"namespace":     api.NamespaceProp,
				"labelSelector": api.LabelSelectorProp,
				"limit":         api.LimitProp,
				"driftedOnly":   api.BoolProp("Return only resources that have drifted or cannot be reconciled. Defaults to true."),
			}),
			Handler: driftDetect,
		},
	}
}

// drifted is one managed resource and what is out of step on it.
type drifted struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name"`
	Namespace  string       `json:"namespace,omitempty"`
	Synced     string       `json:"synced"`
	Ready      string       `json:"ready"`
	Paused     bool         `json:"paused"`
	Policies   []string     `json:"managementPolicies,omitempty"`
	Correcting bool         `json:"correcting"`
	Reason     string       `json:"reason,omitempty"`
	Fields     []fieldDrift `json:"fields,omitempty"`
}

// fieldDrift is one path under spec.forProvider whose observed value differs.
type fieldDrift struct {
	Path     string `json:"path"`
	Declared string `json:"declared"`
	Observed string `json:"observed"`
}

func driftDetect(p api.Params) (*api.Result, error) {
	query := crossplane.Query{
		Category:      crossplane.CategoryManaged,
		Kind:          p.Args.OptionalString("kind", ""),
		Group:         p.Args.OptionalString("group", ""),
		Namespace:     p.Args.OptionalString("namespace", ""),
		LabelSelector: p.Args.OptionalString("labelSelector", ""),
		Limit:         int64(p.Args.OptionalInt("limit", 500)),
	}
	driftedOnly := p.Args.OptionalBool("driftedOnly", true)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	result, err := p.Client.Query(p, query)
	if err != nil {
		return api.Error(err), nil
	}

	reports := make([]drifted, 0, len(result.Objects))
	for i := range result.Objects {
		report := inspectDrift(&result.Objects[i])
		if driftedOnly && report.Correcting && len(report.Fields) == 0 {
			continue
		}
		reports = append(reports, report)
	}

	return api.Structured(renderDrift(reports, len(result.Objects), driftedOnly, result.Warnings), map[string]any{
		"inspected": len(result.Objects),
		"reported":  len(reports),
		"resources": reports,
		"warnings":  result.Warnings,
	}), nil
}

// inspectDrift diffs one managed resource and works out whether Crossplane is
// still in a position to correct what it finds.
func inspectDrift(obj *unstructured.Unstructured) drifted {
	summary := crossplane.Summarize(obj)
	report := drifted{
		APIVersion: summary.APIVersion,
		Kind:       summary.Kind,
		Name:       summary.Name,
		Namespace:  summary.Namespace,
		Ready:      summary.Ready,
		Synced:     summary.Synced,
		Paused:     obj.GetAnnotations()[pausedAnnotation] == "true",
		Policies:   managementPolicies(obj),
		Correcting: true,
	}

	desired, _, _ := unstructured.NestedMap(obj.Object, "spec", "forProvider")
	observed, _, _ := unstructured.NestedMap(obj.Object, "status", "atProvider")
	report.Fields = diffFields("", desired, observed)

	switch {
	case report.Paused:
		report.Correcting = false
		report.Reason = "reconciliation is paused, so drift will not be corrected"
	case !writesToProvider(report.Policies):
		report.Correcting = false
		report.Reason = "management policies " + strings.Join(report.Policies, ",") +
			" stop Crossplane writing, so drift is observed but never corrected"
	case report.Synced != crossplane.StatusTrue:
		report.Correcting = false
		report.Reason = "Synced is " + report.Synced + ", so the provider cannot apply the spec: " +
			crossplane.FirstProblem(summary.Conditions)
	}
	return report
}

// diffFields walks the declared spec and reports every leaf whose observed
// value differs. Only declared fields are compared: status.atProvider carries
// many computed fields that were never asked for and are not drift.
func diffFields(prefix string, desired, observed map[string]any) []fieldDrift {
	var drifts []fieldDrift

	keys := make([]string, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		want := desired[key]
		got, present := observed[key]
		if !present {
			// The provider does not report this field back. That is normal for
			// write-only fields such as credentials, so it is not drift.
			continue
		}

		wantMap, wantIsMap := want.(map[string]any)
		gotMap, gotIsMap := got.(map[string]any)
		if wantIsMap && gotIsMap {
			drifts = append(drifts, diffFields(path, wantMap, gotMap)...)
			continue
		}

		if !reflect.DeepEqual(want, got) {
			drifts = append(drifts, fieldDrift{
				Path:     path,
				Declared: render(want),
				Observed: render(got),
			})
		}
	}

	if len(drifts) > maxDriftFields {
		drifts = drifts[:maxDriftFields]
	}
	return drifts
}

func managementPolicies(obj *unstructured.Unstructured) []string {
	policies, found, err := unstructured.NestedStringSlice(obj.Object, "spec", "managementPolicies")
	if err != nil || !found {
		return nil
	}
	return policies
}

// writesToProvider reports whether the policies still let Crossplane push the
// spec out. An empty list means the default, which is full management.
func writesToProvider(policies []string) bool {
	if len(policies) == 0 {
		return true
	}
	for _, policy := range policies {
		switch policy {
		case "*", "Create", "Update", "LateInitialize":
			return true
		}
	}
	return false
}

func renderDrift(reports []drifted, inspected int, driftedOnly bool, warnings []string) string {
	var text strings.Builder

	if len(reports) == 0 {
		fmt.Fprintf(&text, "Inspected %d managed resource(s). None have drifted and all are being reconciled.", inspected)
		return text.String()
	}

	fmt.Fprintf(&text, "Inspected %d managed resource(s), reporting %d.", inspected, len(reports))
	if driftedOnly {
		text.WriteString(" Resources that match their declared spec and are reconciling normally are omitted.")
	}

	rows := make([][]string, 0, len(reports))
	for _, r := range reports {
		state := "correcting"
		if !r.Correcting {
			state = "NOT CORRECTING"
		}
		rows = append(rows, []string{
			r.Kind,
			resourcePath(r.Namespace, r.Name),
			itoa(len(r.Fields)),
			r.Synced,
			state,
			orDash(r.Reason),
		})
	}
	api.Section(&text, "Summary:",
		api.Table([]string{"KIND", "NAME", "DRIFTED FIELDS", "SYNCED", "STATE", "NOTE"}, rows))

	for _, r := range reports {
		if len(r.Fields) == 0 {
			continue
		}
		fieldRows := make([][]string, 0, len(r.Fields))
		for _, f := range r.Fields {
			fieldRows = append(fieldRows, []string{f.Path, f.Declared, f.Observed})
		}
		api.Section(&text, fmt.Sprintf("%s %s:", r.Kind, resourcePath(r.Namespace, r.Name)),
			api.Table([]string{"FIELD", "DECLARED", "OBSERVED"}, fieldRows))
	}

	if len(warnings) > 0 {
		api.Section(&text, "Kinds that could not be listed:", "- "+strings.Join(warnings, "\n- "))
	}
	return strings.TrimRight(text.String(), "\n")
}

// render turns a JSON value into something that fits in a table cell.
func render(value any) string {
	switch v := value.(type) {
	case nil:
		return "-"
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, render(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		return fmt.Sprintf("{%d field(s)}", len(v))
	default:
		return fmt.Sprintf("%v", v)
	}
}
