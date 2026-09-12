package mcp

// instructions are sent to the client during initialisation. They are the one
// chance we get to teach a model how a Crossplane control plane is put
// together, before it starts guessing which tool to call.
const instructions = `This server gives read-only access to a Crossplane control plane.

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
external system rejected or is still creating the resource.

Suggested approach:
- Start with crossplane_status for a general picture of the control plane.
- For "what is broken?", call crossplane_unhealthy_resources.
- For "why is this claim not ready?", call crossplane_resource_tree on the
  claim, find the deepest resource that is not Ready, then call
  crossplane_resource_get and crossplane_resource_events on it. The real error
  from the cloud provider is usually in an event, not in a condition.
- If a kind is missing, check crossplane_providers_list: an unhealthy provider
  never installs its CRDs.
- If you do not know the exact kind or API group to pass, call
  crossplane_api_resources first rather than guessing.

Every tool here is read-only. This server cannot create, update or delete
anything, so it is safe to explore.`
