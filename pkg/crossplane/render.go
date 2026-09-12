package crossplane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ErrRenderUnavailable is returned when the crossplane CLI is not installed.
// Rendering runs the composition function pipeline, which cannot be done
// through the Kubernetes API, so the CLI is required.
var ErrRenderUnavailable = errors.New("the crossplane CLI is not on PATH, " +
	"install it from https://docs.crossplane.io/latest/cli to render compositions")

// renderTimeout bounds a render. Functions run as containers, so a cold image
// pull can be slow, but a pipeline that takes longer than this is stuck.
const renderTimeout = 2 * time.Minute

// RenderResult is the outcome of rendering a composite resource.
type RenderResult struct {
	// Composition is the name of the Composition that was rendered.
	Composition string `json:"composition"`
	// Functions lists the functions the pipeline ran, in order.
	Functions []string `json:"functions"`
	// Manifests is the rendered YAML: the composite resource followed by
	// every resource the pipeline produced.
	Manifests string `json:"manifests"`
	// Resources summarises what was produced, which is usually the part a
	// caller actually wants to reason about.
	Resources []RenderedResource `json:"resources"`
	// Warnings carries anything the CLI wrote to stderr on success.
	Warnings string `json:"warnings,omitempty"`
}

// RenderedResource identifies one resource produced by a render.
type RenderedResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	// ComposedBy is the composition-resource-name annotation, which is the
	// pipeline's own name for this resource.
	ComposedBy string `json:"composedBy,omitempty"`
}

// Render runs a Composition's function pipeline against a composite resource
// without touching the control plane.
//
// The Composition and the Functions it references are read from the live
// control plane, so the result reflects what this cluster would actually
// produce rather than whatever happens to be on the caller's disk.
func (c *Client) Render(ctx context.Context, compositionName, compositeYAML string) (*RenderResult, error) {
	cli, err := exec.LookPath("crossplane")
	if err != nil {
		return nil, ErrRenderUnavailable
	}

	composition, err := c.compositionManifest(ctx, compositionName)
	if err != nil {
		return nil, err
	}

	functions, names, err := c.functionManifests(ctx, composition)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("composition %q runs no functions, only pipeline compositions can be rendered",
			compositionName)
	}

	dir, err := os.MkdirTemp("", "crossplane-render-")
	if err != nil {
		return nil, fmt.Errorf("cannot create a working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	files := map[string]string{
		"xr.yaml":          compositeYAML,
		"composition.yaml": composition,
		"functions.yaml":   functions,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return nil, fmt.Errorf("cannot write %s: %w", name, err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	// Arguments are passed as a slice and never through a shell, so the
	// caller supplied manifest cannot become a command.
	cmd := exec.CommandContext(ctx, cli, "render", "xr.yaml", "composition.yaml", "functions.yaml")
	cmd.Dir = dir
	cmd.Env = renderEnvironment()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("render timed out after %s, a function may be stuck or an image slow to pull: %s",
				renderTimeout, detail)
		}
		return nil, fmt.Errorf("render failed: %s", detail)
	}

	return &RenderResult{
		Composition: compositionName,
		Functions:   names,
		Manifests:   stdout.String(),
		Resources:   summariseRendered(stdout.Bytes()),
		Warnings:    strings.TrimSpace(stderr.String()),
	}, nil
}

// renderEnvironment returns the environment for the render subprocess.
//
// Functions run as containers, and the Docker library the CLI uses only reads
// DOCKER_HOST. The docker CLI itself resolves the socket through its contexts,
// so on Colima, Podman and Rancher Desktop the daemon is running but the
// default socket path does not exist. Resolve the endpoint the same way the
// docker CLI would and pass it through.
func renderEnvironment() []string {
	environment := os.Environ()
	if os.Getenv("DOCKER_HOST") != "" {
		return environment
	}

	docker, err := exec.LookPath("docker")
	if err != nil {
		return environment
	}
	out, err := exec.Command(docker, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output()
	if err != nil {
		return environment
	}
	host := strings.TrimSpace(string(out))
	if host == "" {
		return environment
	}
	return append(environment, "DOCKER_HOST="+host)
}

// compositionManifest fetches a Composition and strips the fields the API
// server added, which the CLI does not want to see.
func (c *Client) compositionManifest(ctx context.Context, name string) (string, error) {
	resource, err := c.ResolveKind(ctx, "Composition", GroupAPIExtensions)
	if err != nil {
		return "", err
	}
	obj, err := c.Get(ctx, resource, "", name)
	if err != nil {
		return "", err
	}

	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(obj.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	unstructured.RemoveNestedField(obj.Object, "metadata", "generation")
	unstructured.RemoveNestedField(obj.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(obj.Object, "status")

	manifest, err := yaml.Marshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("cannot render composition %q: %w", name, err)
	}
	return string(manifest), nil
}

// functionManifests builds the functions file the CLI needs, containing only
// the functions this composition's pipeline references.
func (c *Client) functionManifests(ctx context.Context, compositionYAML string) (manifests string, names []string, err error) {
	var composition map[string]any
	if err := yaml.Unmarshal([]byte(compositionYAML), &composition); err != nil {
		return "", nil, fmt.Errorf("cannot read the composition: %w", err)
	}

	wanted := map[string]bool{}
	if spec, ok := composition["spec"].(map[string]any); ok {
		if steps, ok := spec["pipeline"].([]any); ok {
			for _, step := range steps {
				entry, ok := step.(map[string]any)
				if !ok {
					continue
				}
				if ref, ok := entry["functionRef"].(map[string]any); ok {
					if name := stringField(ref, "name"); name != "" {
						wanted[name] = true
						names = append(names, name)
					}
				}
			}
		}
	}
	if len(wanted) == 0 {
		return "", nil, nil
	}

	resource, err := c.ResolveKind(ctx, "Function", GroupPkg)
	if err != nil {
		return "", nil, err
	}
	list, err := c.List(ctx, resource, ListOptions{})
	if err != nil {
		return "", nil, err
	}

	var b strings.Builder
	found := map[string]bool{}
	for i := range list.Items {
		obj := &list.Items[i]
		if !wanted[obj.GetName()] {
			continue
		}
		found[obj.GetName()] = true

		// The CLI only needs the identity and the package to pull.
		function := map[string]any{
			"apiVersion": obj.GetAPIVersion(),
			"kind":       obj.GetKind(),
			"metadata":   map[string]any{"name": obj.GetName()},
			"spec":       map[string]any{"package": nestedString(obj, "spec", "package")},
		}
		encoded, err := yaml.Marshal(function)
		if err != nil {
			return "", nil, fmt.Errorf("cannot render function %q: %w", obj.GetName(), err)
		}
		if b.Len() > 0 {
			b.WriteString("---\n")
		}
		b.Write(encoded)
	}

	for name := range wanted {
		if !found[name] {
			return "", nil, fmt.Errorf("the composition references function %q, which is not installed", name)
		}
	}
	return b.String(), names, nil
}

// summariseRendered lists what the pipeline produced. The first document is
// the composite resource itself; the rest are the composed resources.
func summariseRendered(manifests []byte) []RenderedResource {
	documents := strings.Split(string(manifests), "\n---\n")
	out := make([]RenderedResource, 0, len(documents))
	for _, document := range documents {
		if strings.TrimSpace(document) == "" {
			continue
		}
		var parsed map[string]any
		if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
			continue
		}
		metadata, _ := parsed["metadata"].(map[string]any)
		annotations, _ := metadata["annotations"].(map[string]any)
		out = append(out, RenderedResource{
			APIVersion: stringField(parsed, "apiVersion"),
			Kind:       stringField(parsed, "kind"),
			Name:       stringField(metadata, "name"),
			ComposedBy: stringField(annotations, "crossplane.io/composition-resource-name"),
		})
	}
	return out
}
