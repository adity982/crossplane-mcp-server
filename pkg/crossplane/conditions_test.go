package crossplane

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestConditions(t *testing.T) {
	cases := map[string]struct {
		object *unstructured.Unstructured
		want   []Condition
	}{
		"NoStatus": {
			object: newObject(map[string]any{}),
			want:   nil,
		},
		"EmptyConditions": {
			object: newObject(map[string]any{"status": map[string]any{"conditions": []any{}}}),
			want:   []Condition{},
		},
		"ReadyAndSynced": {
			object: newObject(map[string]any{"status": map[string]any{"conditions": []any{
				map[string]any{"type": "Ready", "status": "True", "reason": "Available"},
				map[string]any{"type": "Synced", "status": "False", "reason": "ReconcileError", "message": "boom"},
			}}}),
			want: []Condition{
				{Type: "Ready", Status: "True", Reason: "Available"},
				{Type: "Synced", Status: "False", Reason: "ReconcileError", Message: "boom"},
			},
		},
		"MalformedEntriesAreSkipped": {
			object: newObject(map[string]any{"status": map[string]any{"conditions": []any{
				"not-a-condition",
				map[string]any{"type": "Ready", "status": "True"},
			}}}),
			want: []Condition{{Type: "Ready", Status: "True"}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, Conditions(tc.object))
		})
	}
}

func TestSummarize(t *testing.T) {
	obj := newObject(map[string]any{
		"apiVersion": "s3.aws.upbound.io/v1beta1",
		"kind":       "Bucket",
		"metadata": map[string]any{
			"name":              "example",
			"creationTimestamp": metav1.NewTime(time.Now().Add(-2 * time.Hour)).UTC().Format(time.RFC3339),
		},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "False", "reason": "Creating"},
			map[string]any{"type": "Synced", "status": "True"},
		}},
	})

	got := Summarize(obj)

	assert.Equal(t, "Bucket", got.Kind)
	assert.Equal(t, "example", got.Name)
	assert.Equal(t, "False", got.Ready)
	assert.Equal(t, "True", got.Synced)
	assert.Equal(t, "Creating", got.Message)
	assert.Equal(t, "2h0m", got.Age)
	assert.False(t, got.Healthy(), "a resource that is not Ready must not be reported as healthy")
}

func TestSummaryHealthy(t *testing.T) {
	cases := map[string]struct {
		summary Summary
		want    bool
	}{
		"NoConditionsIsNotHealthy": {
			summary: Summary{},
			want:    false,
		},
		"AllTrueIsHealthy": {
			summary: Summary{Conditions: []Condition{
				{Type: "Ready", Status: "True"},
				{Type: "Synced", Status: "True"},
			}},
			want: true,
		},
		"AnyFalseIsNotHealthy": {
			summary: Summary{Conditions: []Condition{
				{Type: "Ready", Status: "True"},
				{Type: "Synced", Status: "False"},
			}},
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.summary.Healthy())
		})
	}
}

func TestCountByKind(t *testing.T) {
	summaries := []Summary{
		{APIVersion: "s3.aws.upbound.io/v1beta1", Kind: "Bucket", Ready: "True", Synced: "True"},
		{APIVersion: "s3.aws.upbound.io/v1beta1", Kind: "Bucket", Ready: "False", Synced: "True"},
		{APIVersion: "ec2.aws.upbound.io/v1beta1", Kind: "VPC", Ready: "True", Synced: "False"},
	}

	got := CountByKind(summaries)

	assert.Len(t, got, 2)
	// The busiest kind sorts first, because that is the one an operator is
	// most likely to be asking about.
	assert.Equal(t, "Bucket", got[0].Kind)
	assert.Equal(t, 2, got[0].Total)
	assert.Equal(t, 1, got[0].Ready)
	assert.Equal(t, 1, got[0].NotReady)
	assert.Equal(t, "VPC", got[1].Kind)
	assert.Equal(t, 1, got[1].NotSynced)
}

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		-time.Second:         "0s",
		42 * time.Second:     "42s",
		5 * time.Minute:      "5m",
		90 * time.Minute:     "1h30m",
		50 * time.Hour:       "2d",
		400 * 24 * time.Hour: "1y35d",
	}

	for input, want := range cases {
		assert.Equal(t, want, Duration(input), "Duration(%s)", input)
	}
}

func TestImageTag(t *testing.T) {
	cases := map[string]string{
		"xpkg.upbound.io/upbound/provider-aws-s3:v1.21.1": "v1.21.1",
		"registry.example.com:5000/provider-foo":          "latest",
		"registry.example.com:5000/provider-foo:v1.0.0":   "v1.0.0",
		"provider@sha256:abc123":                          "sha256:abc123",
	}

	for image, want := range cases {
		assert.Equal(t, want, imageTag(image), "imageTag(%q)", image)
	}
}

func newObject(content map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: content}
}
