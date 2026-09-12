# Roadmap

This is where the project is going. It is a statement of intent, not a promise
of dates. Everything here is open to discussion, and items move up the list
when somebody wants to work on them.

## Now

The read-only core, which is what ships today.

- [x] Managed resource counts, listings and status
- [x] Composite resources and claims
- [x] Composition tree traversal, the equivalent of `crossplane beta trace`
- [x] Providers, functions and configurations, with revisions
- [x] XRDs and Compositions
- [x] Whole-control-plane health and "what is failing" diagnostics
- [x] stdio and streamable HTTP transports
- [x] Crossplane v1 and v2 layouts

## Next

Things we believe are worth building, roughly in order.

- **MCP resources for XRD schemas.** Expose each XRD's OpenAPI schema as an MCP
  resource so a model can write a valid claim without being shown one first.
- **Prompts.** Ship reusable prompts for the workflows people repeat: "triage
  this control plane", "explain why this claim is stuck", "review this
  Composition".
- **Provider config awareness.** Report which `ProviderConfig` a managed
  resource uses and whether its credentials are resolving. Credential problems
  are one of the most common causes of `Synced=False` and are currently
  invisible.
- **`Usage` and dependency awareness.** Surface `protection.crossplane.io`
  usages so the model can explain why a delete is blocked.
- **Operations support.** Tools for `ops.crossplane.io` `Operation`,
  `CronOperation` and `WatchOperation` once they are widely used.
- **Composition rendering.** Run `crossplane render` style dry-runs so a model
  can answer "what would this Composition produce?" without applying anything.
- **Better large control plane behaviour.** Server-side pagination and
  streaming for control planes with tens of thousands of managed resources.

## Later

Ideas that need a design proposal before anyone starts.

- **Opt-in write tools.** Creating a claim, or annotating a resource to force
  reconciliation, behind an explicit flag and with elicitation-based consent.
  This needs a careful design; see [GOVERNANCE.md](GOVERNANCE.md#principles).
- **Multi-control-plane support.** Query several control planes in one session,
  in the style of a fleet view.
- **Metrics and traces.** OpenTelemetry output for people running the HTTP
  transport as a shared service.
- **Upbound Spaces awareness.** Treat a space as a first-class target.

## Not planned

- Replacing `kubectl` or a general purpose Kubernetes MCP server. If you want
  to inspect arbitrary Kubernetes objects, run one of those alongside this one.
- Hard-coded knowledge of specific providers. Everything must work through
  discovery and categories.
- A web UI.

## Contributing to the roadmap

Open an issue describing the problem you have, not the feature you want. The
best roadmap items come from somebody explaining what they could not answer
about their control plane.
