package crossplane

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
)

// Event is a trimmed Kubernetes event. Crossplane records the real reason a
// resource is stuck as an event on that resource, so this is essential
// debugging context.
type Event struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int32  `json:"count"`
	Age       string `json:"age"`
	Source    string `json:"source,omitempty"`
	Object    string `json:"object,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// EventsFor returns the events recorded against a single object, newest last.
func (c *Client) EventsFor(ctx context.Context, obj *unstructured.Unstructured) ([]Event, error) {
	// Cluster scoped objects live in the empty namespace, which the events
	// API treats as "all namespaces" — exactly what we want for them.
	list, err := c.core.CoreV1().Events(obj.GetNamespace()).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("involvedObject.uid", string(obj.GetUID())).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list events for %s %q: %w", obj.GetKind(), obj.GetName(), err)
	}
	return convertEvents(list.Items), nil
}

// WarningEvents returns recent Warning events across a namespace, or the whole
// cluster when namespace is empty.
func (c *Client) WarningEvents(ctx context.Context, namespace string, limit int64) ([]Event, error) {
	list, err := c.core.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("type", corev1.EventTypeWarning).String(),
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list warning events: %w", err)
	}
	return convertEvents(list.Items), nil
}

func convertEvents(items []corev1.Event) []Event {
	sort.Slice(items, func(i, j int) bool {
		return eventTime(items[i]).Time.Before(eventTime(items[j]).Time)
	})
	events := make([]Event, 0, len(items))
	for _, e := range items {
		events = append(events, Event{
			Type:      e.Type,
			Reason:    e.Reason,
			Message:   truncate(e.Message, 400),
			Count:     e.Count,
			Age:       eventAge(e),
			Source:    e.Source.Component,
			Object:    e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Namespace: e.InvolvedObject.Namespace,
		})
	}
	return events
}

func eventTime(e corev1.Event) metav1.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp
	}
	if !e.EventTime.IsZero() {
		return metav1.Time{Time: e.EventTime.Time}
	}
	return e.CreationTimestamp
}

func eventAge(e corev1.Event) string {
	t := eventTime(e)
	if t.IsZero() {
		return "<unknown>"
	}
	return Duration(time.Since(t.Time))
}
