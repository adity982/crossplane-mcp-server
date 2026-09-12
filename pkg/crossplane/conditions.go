package crossplane

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The condition types Crossplane uses. Managed and composite resources carry
// Ready and Synced; packages carry Installed and Healthy.
const (
	TypeReady     = "Ready"
	TypeSynced    = "Synced"
	TypeHealthy   = "Healthy"
	TypeInstalled = "Installed"
)

// Condition is a trimmed down status condition. We deliberately do not reuse
// metav1.Condition because Crossplane omits observedGeneration on most of its
// conditions and we want the JSON we hand to the model to stay small.
type Condition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// IsTrue reports whether the condition is currently satisfied.
func (c Condition) IsTrue() bool { return c.Status == string(metav1.ConditionTrue) }

// Conditions extracts status.conditions from any object. Objects without
// conditions yield an empty slice rather than an error: a managed resource
// that was just created has not been reconciled yet and that is a normal,
// reportable state.
func Conditions(obj *unstructured.Unstructured) []Condition {
	raw, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return nil
	}
	conditions := make([]Condition, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		conditions = append(conditions, Condition{
			Type:               stringField(entry, "type"),
			Status:             stringField(entry, "status"),
			Reason:             stringField(entry, "reason"),
			Message:            stringField(entry, "message"),
			LastTransitionTime: stringField(entry, "lastTransitionTime"),
		})
	}
	return conditions
}

// ConditionOfType returns the condition with the given type, if present.
func ConditionOfType(conditions []Condition, conditionType string) (Condition, bool) {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c, true
		}
	}
	return Condition{}, false
}

// conditionStatus renders a condition as a short column value. "-" means the
// object does not report that condition at all, which is different from
// "False" and worth showing.
func conditionStatus(conditions []Condition, conditionType string) string {
	if c, ok := ConditionOfType(conditions, conditionType); ok {
		return c.Status
	}
	return "-"
}

// Summary is the compact, uniform view of a Crossplane object that every list
// tool returns. Keeping one shape across tools means the model only has to
// learn it once.
type Summary struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Name       string      `json:"name"`
	Namespace  string      `json:"namespace,omitempty"`
	Ready      string      `json:"ready"`
	Synced     string      `json:"synced"`
	Age        string      `json:"age"`
	Message    string      `json:"message,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
}

// Healthy reports whether every condition the object publishes is True. A
// resource that reports nothing yet is not considered healthy, because from an
// operator's point of view "no status" means "not working yet".
func (s Summary) Healthy() bool {
	if len(s.Conditions) == 0 {
		return false
	}
	for _, c := range s.Conditions {
		if !c.IsTrue() {
			return false
		}
	}
	return true
}

// Summarize builds a Summary from an arbitrary object.
func Summarize(obj *unstructured.Unstructured) Summary {
	conditions := Conditions(obj)
	return Summary{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
		Ready:      conditionStatus(conditions, TypeReady),
		Synced:     conditionStatus(conditions, TypeSynced),
		Age:        Age(obj),
		Message:    FirstProblem(conditions),
		Conditions: conditions,
	}
}

// SummarizePackage builds a Summary for a package (Provider, Function,
// Configuration), which reports Installed/Healthy instead of Ready/Synced.
func SummarizePackage(obj *unstructured.Unstructured) Summary {
	conditions := Conditions(obj)
	s := Summarize(obj)
	s.Ready = conditionStatus(conditions, TypeHealthy)
	s.Synced = conditionStatus(conditions, TypeInstalled)
	return s
}

// FirstProblem returns the message of the first condition that is not True.
// This is what an operator reads first when something is broken, so it earns
// its place in the summary line.
func FirstProblem(conditions []Condition) string {
	for _, c := range conditions {
		if c.IsTrue() {
			continue
		}
		switch {
		case c.Message != "":
			return truncate(c.Message, 160)
		case c.Reason != "":
			return c.Reason
		default:
			return fmt.Sprintf("%s=%s", c.Type, c.Status)
		}
	}
	return ""
}

// Age renders the object's age the way kubectl does: 3d, 5h12m, 42s.
func Age(obj *unstructured.Unstructured) string {
	created := obj.GetCreationTimestamp()
	if created.IsZero() {
		return "<unknown>"
	}
	return Duration(time.Since(created.Time))
}

// Duration formats a duration using kubectl's coarse, human friendly rules.
func Duration(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		years := int(d.Hours() / 24 / 365)
		days := int(d.Hours()/24) % 365
		return fmt.Sprintf("%dy%dd", years, days)
	}
}

// ExternalName returns the crossplane.io/external-name annotation, which is
// how a managed resource records its identity in the external system.
func ExternalName(obj *unstructured.Unstructured) string {
	return obj.GetAnnotations()["crossplane.io/external-name"]
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
