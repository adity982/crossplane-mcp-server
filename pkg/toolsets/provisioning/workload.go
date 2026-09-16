package provisioning

import (
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
)

const (
	sizeSmall  = "small"
	sizeMedium = "medium"
	sizeLarge  = "large"
)

func workloadTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_workload_create",
			Title: "Workload: create",
			Description: "Create a workload, app or service by asking this control plane's own platform API for " +
				"one. Works the same way as crossplane_database_create: it finds the claim or composite kind " +
				"that offers workloads, fills in the fields its XRD declares and applies it. " +
				"Pass 'dryRun' to see the manifest without creating anything.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"name":      api.StringProp("Name for the workload, for example 'checkout-api'."),
				"namespace": namespaceProp,
				"image":     api.StringProp("Container image to run, for example 'ghcr.io/acme/checkout:v1.2.0'."),
				"replicas":  api.IntProp("How many instances to run."),
				"port":      api.IntProp("Port the container listens on."),
				"size":      api.EnumProp("How much the workload should be given, in the sizes platform APIs usually offer.", sizeSmall, sizeMedium, sizeLarge),
				"kind": api.StringProp("Kind to create, for example 'App'. " +
					"Omit to let the server find the workload API this control plane offers."),
				"apiVersion": api.StringProp("API version of the kind, for example 'platform.example.org/v1alpha1'. " +
					"Only needed when the same kind exists in more than one group."),
				"parameters": api.ObjectProp("Extra fields to set, for the parts of the platform API this tool " +
					"does not know about. Call crossplane_xrd_schema to see what the API accepts."),
				"dryRun": api.DryRunProp,
			}, "name"),
			Write:   true,
			Handler: workloadCreate,
		},
	}
}

func workloadCreate(p api.Params) (*api.Result, error) {
	req := request{
		noun:       "workload",
		hints:      []string{"workload", "application", "app", "service", "deployment", "container", "api"},
		name:       p.Args.String("name"),
		namespace:  p.Args.OptionalString("namespace", ""),
		kind:       p.Args.OptionalString("kind", ""),
		apiVersion: p.Args.OptionalString("apiVersion", ""),
		parameters: p.Args.OptionalMap("parameters"),
		dryRun:     p.Args.OptionalBool("dryRun", false),
	}
	image := p.Args.OptionalString("image", "")
	size := p.Args.OptionalEnum("size", sizeSmall, sizeSmall, sizeMedium, sizeLarge)
	replicas := p.Args.OptionalInt("replicas", 0)
	port := p.Args.OptionalInt("port", 0)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	req.values = []field{
		{names: []string{"image", "containerImage", "imageRef"}, value: optional(image)},
		{names: []string{"replicas", "count", "instances", "scale"}, value: optionalInt(replicas)},
		{names: []string{"port", "containerPort", "targetPort", "servicePort"}, value: optionalInt(port)},
		{names: []string{"size", "plan", "class", "tier"}, value: size},
	}
	return provision(p, req)
}
