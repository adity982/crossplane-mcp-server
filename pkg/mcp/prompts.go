// Package mcp adapts this server's tools to the Model Context Protocol.
package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// prompt is a named workflow offered to the user.
//
// Tools say what a model can do; prompts say the order to do it in, so it does
// not rediscover on every conversation that diagnosing a claim starts at the
// claim rather than at the managed resource that looks angriest.
type prompt struct {
	name        string
	title       string
	description string
	arguments   []*sdk.PromptArgument
	render      func(args map[string]string) string
}

func requiredArg(name, description string) *sdk.PromptArgument {
	return &sdk.PromptArgument{Name: name, Description: description, Required: true}
}

func optionalArg(name, description string) *sdk.PromptArgument {
	return &sdk.PromptArgument{Name: name, Description: description}
}

func prompts() []prompt {
	return []prompt{
		{
			name:        "diagnose_resource",
			title:       "Diagnose a failing resource",
			description: "Work out why a claim, composite resource or managed resource is not Ready, and say what to do about it.",
			arguments: []*sdk.PromptArgument{
				requiredArg("kind", "Kind of the resource, for example 'PostgreSQLInstance'."),
				requiredArg("name", "Name of the resource."),
				optionalArg("namespace", "Namespace of the resource, if it is namespaced."),
				optionalArg("cluster", "Control plane to query. Omit for the default."),
			},
			render: func(args map[string]string) string {
				return "Diagnose why " + describe(args) + " is not Ready.\n\n" +
					"Start with crossplane_diagnose, which walks the whole chain and finds the deepest " +
					"failure rather than the symptom at the top. Then:\n" +
					"1. If it names a failing managed resource, call crossplane_resource_events on that " +
					"resource. Providers put the real cloud API error in an event.\n" +
					"2. If the verdict blames a provider or function, call crossplane_providers_list or " +
					"crossplane_functions_list to confirm, and crossplane_package_get for its revisions.\n" +
					"3. If the Composition looks wrong, call crossplane_composition_validate, then " +
					"crossplane_composition_render to see what the pipeline would produce.\n\n" +
					"Finish with a short answer: the one resource at fault, the underlying error in plain " +
					"words, and the change that would fix it. Do not paste raw YAML unless it is needed to " +
					"make the fix clear."
			},
		},
		{
			name:        "control_plane_review",
			title:       "Review control plane health",
			description: "Produce a health report for the whole control plane, ordered by what needs attention first.",
			arguments: []*sdk.PromptArgument{
				optionalArg("cluster", "Control plane to review. Omit for the default."),
			},
			render: func(args map[string]string) string {
				return "Review the health of " + clusterPhrase(args) + ".\n\n" +
					"Gather evidence in this order:\n" +
					"1. crossplane_status for the overall picture and the Crossplane version.\n" +
					"2. crossplane_unhealthy_resources for everything currently failing.\n" +
					"3. crossplane_deleting_resources for deletes that are stuck, which are easy to miss " +
					"because kubectl reports success and then nothing happens.\n" +
					"4. crossplane_drift_detect for infrastructure that no longer matches its declared spec, " +
					"paying particular attention to resources that are paused or not Synced, because their " +
					"drift will never be corrected.\n\n" +
					"Report findings grouped by severity: things that are broken now, things that will break, " +
					"and things that are merely untidy. For each, name the resource and the reason. Be brief."
			},
		},
		{
			name:        "explain_platform_api",
			title:       "Explain a platform API",
			description: "Explain what a platform API offers and how to ask for one, from its XRD and Composition.",
			arguments: []*sdk.PromptArgument{
				optionalArg("kind", "Composite or claim kind to explain, for example 'PostgreSQLInstance'. Omit to survey everything on offer."),
				optionalArg("cluster", "Control plane to query. Omit for the default."),
			},
			render: func(args map[string]string) string {
				if args["kind"] == "" {
					return "Survey what this platform offers its users.\n\n" +
						"Call crossplane_xrds_list to see every platform API, then crossplane_api_resources " +
						"to see which composite and claim kinds exist. For the handful that look most " +
						"useful, call crossplane_xrd_schema.\n\n" +
						"Summarise what a developer can ask this control plane for, in their language rather " +
						"than Crossplane's. Do not explain what an XRD is unless asked."
				}
				return "Explain the " + args["kind"] + " platform API.\n\n" +
					"1. crossplane_xrd_schema for the exact apiVersion and kind to use, the shape of spec, " +
					"which fields are required, and a minimal example.\n" +
					"2. crossplane_compositions_list with compositeKind set to " + args["kind"] + " to see " +
					"which implementations satisfy it and what each one builds.\n" +
					"3. crossplane_composition_render if it helps to show what a request would actually create.\n\n" +
					"Answer with a worked example manifest the user can adapt, and a sentence on what they " +
					"will get when they apply it."
			},
		},
		{
			name:        "assess_deletion",
			title:       "Assess a deletion before doing it",
			description: "Work out the blast radius of deleting a resource, including the real infrastructure behind it.",
			arguments: []*sdk.PromptArgument{
				requiredArg("kind", "Kind of the resource being considered for deletion."),
				requiredArg("name", "Name of the resource."),
				optionalArg("namespace", "Namespace of the resource, if it is namespaced."),
				optionalArg("cluster", "Control plane to query. Omit for the default."),
			},
			render: func(args map[string]string) string {
				return "Assess what would happen if " + describe(args) + " were deleted.\n\n" +
					"Call crossplane_impact first: it lists everything that would be torn down with it and " +
					"every Usage that would block the delete or be left dangling. Call " +
					"crossplane_usages_list if the protection picture needs more detail.\n\n" +
					"Report: how many resources go, which of them have real infrastructure behind them " +
					"(they have an external name), whether the delete would be blocked, and anything that " +
					"would be left in a broken state afterwards. State clearly whether this looks safe. " +
					"This server cannot delete anything, so make it explicit that the user must run the " +
					"delete themselves."
			},
		},
	}
}

// PromptDeclarations renders the prompts as a client sees them in a
// prompts/list reply. Callers that need the wire shape without running a
// server, such as bundle packaging, use this.
func PromptDeclarations() []*sdk.Prompt {
	defined := prompts()
	declarations := make([]*sdk.Prompt, 0, len(defined))
	for _, p := range defined {
		declarations = append(declarations, &sdk.Prompt{
			Name:        p.name,
			Title:       p.title,
			Description: p.description,
			Arguments:   p.arguments,
		})
	}
	return declarations
}

// registerPrompts wires the workflows onto the SDK server.
func (s *Server) registerPrompts() {
	for _, p := range prompts() {
		definition := &sdk.Prompt{
			Name:        p.name,
			Title:       p.title,
			Description: p.description,
			Arguments:   p.arguments,
		}
		render := p.render
		s.sdk.AddPrompt(definition, func(_ context.Context, request *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			return &sdk.GetPromptResult{
				Description: definition.Description,
				Messages: []*sdk.PromptMessage{{
					Role:    "user",
					Content: &sdk.TextContent{Text: render(request.Params.Arguments)},
				}},
			}, nil
		})
	}
}

// describe names the resource a prompt is about, in the form a tool call needs.
func describe(args map[string]string) string {
	subject := args["kind"] + " " + args["name"]
	if namespace := args["namespace"]; namespace != "" {
		subject += " in namespace " + namespace
	}
	if cluster := args["cluster"]; cluster != "" {
		subject += " on cluster " + cluster
	}
	return subject
}

func clusterPhrase(args map[string]string) string {
	if cluster := args["cluster"]; cluster != "" {
		return "the " + cluster + " control plane"
	}
	return "the default control plane"
}
