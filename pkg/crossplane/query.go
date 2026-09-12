package crossplane

import (
	"context"
	"sort"
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// listConcurrency bounds how many resource types we list in parallel. A busy
// control plane can expose several hundred managed resource kinds; listing
// them one at a time is unusably slow, and listing them all at once trips the
// API server's priority and fairness limits.
const listConcurrency = 12

// Query describes a search across one or more Crossplane resource types.
type Query struct {
	// Category restricts the search to resources tagged with it, e.g.
	// CategoryManaged. Ignored when Kind is set.
	Category string
	// Kind optionally narrows the search to a single kind, plural name or
	// short name.
	Kind string
	// Group narrows an ambiguous Kind to one API group.
	Group string
	// Namespace restricts namespaced resources. Empty means all namespaces.
	Namespace string
	// LabelSelector is a standard Kubernetes label selector.
	LabelSelector string
	// Limit caps the number of objects returned per resource type. Zero means
	// no cap.
	Limit int64
}

// QueryResult carries the objects that matched together with the kinds that
// could not be listed. Partial failures are normal on a control plane where a
// provider is mid-upgrade, and hiding them would make the answers misleading.
type QueryResult struct {
	Objects []unstructured.Unstructured
	// Kinds lists the resource types that were searched.
	Kinds []APIResource
	// Warnings describes resource types that could not be listed.
	Warnings []string
}

// Query lists Crossplane objects matching q.
func (c *Client) Query(ctx context.Context, q Query) (*QueryResult, error) {
	kinds, err := c.kindsFor(ctx, q)
	if err != nil {
		return nil, err
	}

	var (
		mu       sync.Mutex
		objects  []unstructured.Unstructured
		warnings []string
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, listConcurrency)

	for _, kind := range kinds {
		wg.Add(1)
		go func(r APIResource) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			list, listErr := c.List(ctx, r, ListOptions{
				Namespace:     q.Namespace,
				LabelSelector: q.LabelSelector,
				Limit:         q.Limit,
			})

			mu.Lock()
			defer mu.Unlock()
			if listErr != nil {
				// A kind that vanished between discovery and the list call is
				// not worth reporting; anything else is.
				if !apierrors.IsNotFound(listErr) && !isNoMatchError(listErr) {
					warnings = append(warnings, r.APIVersion()+"/"+r.Kind+": "+listErr.Error())
				}
				return
			}
			objects = append(objects, list.Items...)
		}(kind)
	}
	wg.Wait()

	sort.Slice(objects, func(i, j int) bool {
		a, b := objects[i], objects[j]
		if a.GetKind() != b.GetKind() {
			return a.GetKind() < b.GetKind()
		}
		if a.GetNamespace() != b.GetNamespace() {
			return a.GetNamespace() < b.GetNamespace()
		}
		return a.GetName() < b.GetName()
	})
	sort.Strings(warnings)

	return &QueryResult{Objects: objects, Kinds: kinds, Warnings: warnings}, nil
}

func (c *Client) kindsFor(ctx context.Context, q Query) ([]APIResource, error) {
	if q.Kind != "" {
		r, err := c.ResolveKind(ctx, q.Kind, q.Group)
		if err != nil {
			return nil, err
		}
		if q.Category != "" && !r.HasCategory(q.Category) {
			return nil, &ErrNotFound{What: r.Kind + " is not a Crossplane " + q.Category + " resource"}
		}
		return []APIResource{r}, nil
	}

	kinds, err := c.ResourcesInCategory(ctx, q.Category)
	if err != nil {
		return nil, err
	}
	if q.Group != "" {
		filtered := kinds[:0]
		for _, k := range kinds {
			if strings.EqualFold(k.Group, q.Group) {
				filtered = append(filtered, k)
			}
		}
		kinds = filtered
	}
	return kinds, nil
}

// Summaries converts a query result into summaries, optionally keeping only
// the objects that are not fully healthy.
func Summaries(objects []unstructured.Unstructured, unhealthyOnly bool) []Summary {
	out := make([]Summary, 0, len(objects))
	for i := range objects {
		s := Summarize(&objects[i])
		if unhealthyOnly && s.Healthy() {
			continue
		}
		out = append(out, s)
	}
	return out
}

// KindCount aggregates objects per kind, which is what most "how many do I
// have" questions actually want.
type KindCount struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Total      int    `json:"total"`
	Ready      int    `json:"ready"`
	NotReady   int    `json:"notReady"`
	Synced     int    `json:"synced"`
	NotSynced  int    `json:"notSynced"`
}

// CountByKind groups summaries by apiVersion and kind.
func CountByKind(summaries []Summary) []KindCount {
	index := map[string]*KindCount{}
	for _, s := range summaries {
		key := s.APIVersion + "/" + s.Kind
		count, ok := index[key]
		if !ok {
			count = &KindCount{APIVersion: s.APIVersion, Kind: s.Kind}
			index[key] = count
		}
		count.Total++
		if s.Ready == StatusTrue {
			count.Ready++
		} else {
			count.NotReady++
		}
		if s.Synced == StatusTrue {
			count.Synced++
		} else {
			count.NotSynced++
		}
	}

	counts := make([]KindCount, 0, len(index))
	for _, c := range index {
		counts = append(counts, *c)
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Total != counts[j].Total {
			return counts[i].Total > counts[j].Total
		}
		return counts[i].Kind < counts[j].Kind
	})
	return counts
}

func isNoMatchError(err error) bool {
	if meta.IsNoMatchError(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "no matches for kind") ||
		strings.Contains(msg, "could not find the requested resource")
}
