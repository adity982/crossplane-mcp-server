package mcp

import "strings"

// instructions are sent to the client during initialisation. They are the one
// chance we get to teach a model how a Crossplane control plane is put
// together, before it starts guessing which tool to call.
//
// What the server will let the model do changes with the mode it was started
// in, so the closing paragraph changes with it. Telling a model it can create
// resources when those tools are withheld only produces confident failures.
func instructions(allowWrite bool) string {
	if allowWrite {
		return strings.Join([]string{background, readingGuide, writingGuide}, "\n\n")
	}
	return strings.Join([]string{background, readingGuide, readOnlyNote}, "\n\n")
}

const background = `This server exposes a Crossplane control plane.

Crossplane in one paragraph: a control plane is a Kubernetes cluster with
Crossplane installed. Providers add managed resources, which are Kubernetes
objects that each represent one piece of real external infrastructure. A
CompositeResourceDefinition (XRD) defines a custom platform API; a Composition
says which resources that API should create. Instances of a platform API are
composite resources (XRs), and in Crossplane v1 they can be requested through
a namespaced claim. Composition functions run the pipeline that produces the
composed resources.

How to read status: nearly every Crossplane object publishes a Ready condition
(is the thing it represents actually working?) and a Synced condition (did
Crossplane manage to reconcile its desired state?). Packages publish Installed
and Healthy instead. Synced=False almost always means a problem with the
Crossplane configuration; Ready=False with Synced=True usually means the
external system rejected or is still creating the resource.`

const readingGuide = `Suggested approach:
- Start with crossplane_status for a general picture of the control plane.
- For "what is broken?", call crossplane_unhealthy_resources.
- For "why is this claim not ready?", call crossplane_resource_tree on the
  claim, find the deepest resource that is not Ready, then call
  crossplane_resource_get and crossplane_resource_events on it. The real error
  from the cloud provider is usually in an event, not in a condition.
- If a kind is missing, check crossplane_providers_list: an unhealthy provider
  never installs its CRDs.
- If you do not know the exact kind or API group to pass, call
  crossplane_api_resources first rather than guessing.`

const readOnlyNote = `Every tool here is read-only. This server cannot create or update anything, so
it is safe to explore. If the user asks you to provision something, say that
the server is running read-only and that an operator has to restart it with
--read-only=false. Nothing deletes in either mode.`

const writingGuide = `This server is running with writes enabled, so some tools change the control
plane. They are annotated: readOnlyHint is false on every one of them.

Writes here only create and update. There is no tool that deletes anything, and
no way to make one of these tools delete. If the user asks you to remove
something, say so and offer crossplane_impact, which reports what a deletion
would destroy, so they can run it themselves.

Never provision by writing a managed resource directly. Ask the platform API
for what you want instead and let the Composition decide which managed
resources that means. crossplane_database_create and crossplane_workload_create
do exactly that: they find the claim or composite kind this control plane
offers, fill in the fields its XRD declares, and apply the result.

When creating something:
- Call the tool with dryRun=true first if the user was not specific. The answer
  shows the exact manifest that would be submitted, and which of the requested
  values the platform API has no field for.
- Then call it again without dryRun and follow with crossplane_resource_tree on
  what you created. Provisioning is asynchronous: a resource that exists is not
  yet a resource that is Ready.
- If no platform API matches what was asked for, say so and offer
  crossplane_xrds_list. Do not invent an apiVersion.

crossplane_resource_apply updates in place. Applying over a resource somebody
else manages is how you break their configuration, so read it with
crossplane_resource_get first and say what you are changing.`
