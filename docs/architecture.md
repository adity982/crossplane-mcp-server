# Architecture

This document explains how the code is laid out and why. Read it before making
a structural change; read [CONTRIBUTING.md](../CONTRIBUTING.md) if you just
want to add a tool.

## Layers

```text
cmd/crossplane-mcp-server   the binary, nothing but a call into internal/cli
internal/cli                flags, logging, wiring, transport selection
pkg/mcp                     the only package that imports the MCP SDK
pkg/toolsets/*              the tools, grouped by subject
pkg/api                     the contract between pkg/mcp and pkg/toolsets
pkg/crossplane              everything that knows what Crossplane means
pkg/kube                    kubeconfig loading
pkg/version                 build information
```

Dependencies point downwards only. In particular:

- `pkg/toolsets/*` never imports the MCP SDK. A tool is an ordinary Go function
  that takes an `api.Params` and returns an `api.Result`, which means it can be
  tested without a protocol round trip.
- `pkg/crossplane` never imports `pkg/api`. It knows about Crossplane, not
  about tools, so it stays reusable by anything else that wants to read a
  control plane.
- `pkg/mcp` never imports `pkg/toolsets/*`. It is handed a list of
  `api.Toolset` values and does not care where they came from.

## Why everything is dynamic

Crossplane has no fixed API surface. A provider adds hundreds of managed
resource kinds at install time, and an XRD adds a composite kind and a claim
kind. Compiling against generated Go types would mean the server only worked
with the providers we happened to know about.

So `pkg/crossplane` uses the discovery API plus the dynamic client
throughout, and identifies resources by the **categories** Crossplane stamps
onto the CRDs it manages:

| Category | Applied by | Meaning |
| --- | --- | --- |
| `crossplane` | everything Crossplane installs | the object belongs to Crossplane |
| `managed` | providers | one piece of external infrastructure |
| `composite` | the XRD controller | an instance of a platform API |
| `claim` | the XRD controller | a namespaced request for a composite |

This is the single most important design decision in the project. It is why
`crossplane_managed_resources_summary` works against a control plane running a
provider that did not exist when this code was written.

## Discovery caching

Discovery is expensive: it is one request per API group version, and a tool
like `crossplane_status` needs the complete picture. `Client.APIResources`
caches the flattened result for 30 seconds.

Thirty seconds is a compromise. Installing a provider rewrites the API surface,
and a user who has just installed one will ask about it immediately. A longer
TTL would make the server look broken; a shorter one would make every tool call
pay for discovery.

Partial discovery failures are kept rather than discarded. A control plane with
one broken aggregated API server is common, and answering "here is what I could
see, plus these warnings" is far more useful than refusing to answer.

## Listing in parallel

`Client.Query` fans out across every kind in a category, bounded to 12
concurrent list calls. One at a time is unusably slow on a control plane with
several hundred managed resource kinds; unbounded trips the API server's
priority and fairness limits and makes things worse.

Failures of individual kinds become warnings on the result rather than failing
the whole call, for the same reason as above.

## Text and structured results

Every tool returns both a human readable rendering and a structured payload:

```go
return api.Structured(text, payload), nil
```

The MCP specification asks servers returning structured content to also return
its text form, and in practice a model answers better from a compact table than
from a wall of JSON. The text is what appears in the conversation transcript,
so it is also what a human reviewing the session reads.

## Error handling

There are two kinds of failure and they take different paths:

| Failure | Path | Why |
| --- | --- | --- |
| Bad argument, missing resource, unreachable cluster | `api.Error(err)` in the `Result` | The model can read it and try something else |
| A bug in the server | the Go `error` return | The model cannot fix it, and it should not look like a control plane problem |

In MCP terms, the first becomes a tool result with `isError: true` and the
second becomes a protocol error. Getting this wrong makes the assistant give up
when it should retry, or retry forever when it should give up.

## Argument parsing

`api.Args` accumulates errors instead of returning them:

```go
kind := p.Args.String("kind")
name := p.Args.String("name")
limit := p.Args.OptionalInt("limit", 500)
if err := p.Args.Err(); err != nil {
    return api.Error(err), nil
}
```

A model that passes three bad arguments gets told about all three at once,
rather than discovering them one round trip at a time.

## The write path

Writes create and update. There is no delete: `pkg/crossplane` has no `Delete`
method and no tool calls one, so the worst an assistant can do is leave a
resource behind. That is a deliberate ceiling, not an unimplemented feature —
on a Crossplane control plane a deleted object is a production database, and
the only safe amount of "the model decided to remove it" is none.

What writes there are are off by default and gated in three places. Anything
that changes a control plane should be hard to enable by accident:

1. **Declaration.** A tool sets `api.Tool.Write`, which drives the
   `readOnlyHint` annotation. A test asserts that a tool carries the flag if
   and only if it lives in the `provisioning` toolset, so a write cannot hide
   inside a toolset people think of as read-only.
2. **Registration.** `mcp.Config.AllowWrite` decides whether those tools are
   registered at all. A withheld tool is absent from `tools/list`, so the model
   never learns it exists. `Server.invoke` checks again, in case a handler is
   reached another way.
3. **The client.** `Client.Apply` rejects any resource that is not a Crossplane
   one, checked against the categories and the `*.crossplane.io` groups. RBAC
   should stop the same thing, but the server should not depend on somebody
   else's ClusterRole being correct.

Everything goes out as a server-side apply under the field manager
`crossplane-mcp-server`. Apply rather than create because a model that retries
a call should update rather than collide, and because it leaves `kubectl apply`
working on the same object. It is also why every tool can honestly declare
`idempotentHint: true` and `destructiveHint: false`.

`pkg/toolsets/provisioning` does one more thing worth knowing about: it does
not assume what a platform API looks like. `crossplane_database_create` finds
the claim or composite kind whose name reads most like a database, reads the
schema from the XRD that defines it, and only sets fields that schema declares.
Requested values with nowhere to go are reported in the answer instead of being
silently pruned by the API server, which is the difference between "your
platform API has no storageGB field" and a database that quietly came up with
the wrong disk.

## Transports

`pkg/mcp` exposes two:

- **stdio**, the default, used by editors and desktop clients that launch the
  binary themselves. Note that stdout carries the protocol, so logs go to
  stderr unconditionally. Writing anything to stdout will corrupt the session.
- **streamable HTTP**, for running the server inside the cluster it inspects.
  It has no authentication of its own; see [SECURITY.md](../SECURITY.md).

## Crossplane v1 and v2

The two lines differ in ways this code has to absorb:

| | v1 | v2 |
| --- | --- | --- |
| Composite resources | cluster scoped | namespaced |
| Claims | the namespaced entry point | deprecated |
| Composition reference | `spec.compositionRef` | `spec.crossplane.compositionRef` |
| Composed resource refs | `spec.resourceRefs` | `status.resourceRefs` |
| Composition default mode | `Resources` | `Pipeline` |

The code reads both locations and deduplicates, rather than branching on a
detected version. A control plane in the middle of an upgrade genuinely has
objects in both shapes.
