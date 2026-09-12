package api

import "github.com/google/jsonschema-go/jsonschema"

// The helpers below keep tool declarations short and, more importantly,
// consistent: an argument called "namespace" should mean and be described the
// same way in every tool, otherwise the model has to relearn it each time.

// Object builds an object schema from a set of named properties.
func Object(properties map[string]*jsonschema.Schema, required ...string) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}
}

// StringProp declares a string argument.
func StringProp(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Description: description}
}

// EnumProp declares a string argument restricted to a closed set of values.
func EnumProp(description string, values ...string) *jsonschema.Schema {
	enum := make([]any, 0, len(values))
	for _, v := range values {
		enum = append(enum, v)
	}
	return &jsonschema.Schema{Type: "string", Description: description, Enum: enum}
}

// BoolProp declares a boolean argument.
func BoolProp(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "boolean", Description: description}
}

// IntProp declares an integer argument.
func IntProp(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "integer", Description: description}
}

// ClusterArg is the argument name used to select a cluster. It is added to
// every tool's schema by the MCP layer rather than declared tool by tool, so
// that it cannot drift.
const ClusterArg = "cluster"

// Shared argument schemas. Reused verbatim across tools.
var (
	// ClusterProp selects which control plane a call is addressed to.
	ClusterProp = StringProp("Which control plane to query. Omit to use the default one. " +
		"Call crossplane_clusters_list to see the available clusters.")

	// NamespaceProp is the namespace selector used by namespaced tools.
	NamespaceProp = StringProp("Namespace to search. Omit to search every namespace. " +
		"Crossplane v1 managed resources are cluster scoped and ignore this argument.")

	// GroupProp disambiguates kinds that several providers define.
	GroupProp = StringProp("API group of the kind, for example 'ec2.aws.upbound.io'. " +
		"Required only when the same kind exists in more than one group.")

	// LabelSelectorProp is the standard Kubernetes label selector.
	LabelSelectorProp = StringProp("Kubernetes label selector, for example 'app=backend,tier!=cache'.")

	// LimitProp caps how many objects a tool returns.
	LimitProp = IntProp("Maximum number of objects to return per kind. Defaults to 500.")
)
