# crossplane-mcp-server

[![CI](https://github.com/ravibagri5/crossplane-mcp-server/actions/workflows/ci.yaml/badge.svg)](https://github.com/ravibagri5/crossplane-mcp-server/actions/workflows/ci.yaml)
[![Release](https://github.com/ravibagri5/crossplane-mcp-server/actions/workflows/release.yaml/badge.svg)](https://github.com/ravibagri5/crossplane-mcp-server/actions/workflows/release.yaml)
[![CodeQL](https://github.com/ravibagri5/crossplane-mcp-server/actions/workflows/codeql.yaml/badge.svg)](https://github.com/ravibagri5/crossplane-mcp-server/actions/workflows/codeql.yaml)
[![GitHub release](https://img.shields.io/github/v/release/ravibagri5/crossplane-mcp-server?sort=semver)](https://github.com/ravibagri5/crossplane-mcp-server/releases/latest)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-00ADD8?logo=go&logoColor=white)](https://golangci-lint.run/)
[![Go version](https://img.shields.io/github/go-mod/go-version/ravibagri5/crossplane-mcp-server)](go.mod)
[![Crossplane](https://img.shields.io/badge/crossplane-v1%20%7C%20v2-%23f2a72c)](https://crossplane.io)
[![Downloads](https://img.shields.io/github/downloads/ravibagri5/crossplane-mcp-server/total)](https://github.com/ravibagri5/crossplane-mcp-server/releases)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A [Model Context Protocol](https://modelcontextprotocol.io) server that lets AI
assistants understand a [Crossplane](https://crossplane.io) control plane.

Ask your assistant *"how many managed resources do I have and is anything
broken?"* and it will answer from your actual control plane, with the reason for
every failure, instead of guessing.

```text
> Why is the app-db claim not ready?

  crossplane_diagnose(kind="PostgreSQLInstance", name="app-db")

  PostgreSQLInstance app-db is NOT READY.

  Verdict: Instance/app-db-rds is the deepest failure: InvalidParameterValue:
  the instance class db.t2.mega does not exist

  Root causes (deepest failing resources):
  KIND       NAME          READY   SYNCED   REASON          DETAIL
  Instance   app-db-rds    False   False    ApplyFailure    InvalidParameterValue...

  Control plane checks:
  CHECK         STATE   DETAIL
  composition   OK      Composition "postgres-aws" exists.
  providers     OK      all 3 provider(s) are Installed and Healthy

  Fix the instanceClass field in your Composition and the claim will reconcile.
```

The server is **read-only by default**: the tools that create and update are
not registered at all unless you start it with `--read-only=false`, so a client
cannot see them, let alone call them. Nothing deletes in either mode. Turn
writes on and the same assistant can provision from your platform APIs:

```text
> Create a small Postgres database called orders-db.

  crossplane_database_create(name="orders-db", engine="postgresql", size="small")

  Created PostgreSQLInstance default/orders-db on kind-crossplane.

  apiVersion: demo.crossplane.io/v1alpha1
  kind: PostgreSQLInstance
  metadata:
    name: orders-db
    namespace: default
  spec:
    parameters:
      engine: postgresql
      size: small
      version: "16"

  Next: provisioning is asynchronous. Call crossplane_resource_tree with
  {"kind":"PostgreSQLInstance","name":"orders-db"} to watch it come up.
```

Nothing there names a Kubernetes kind. The tool found the platform API this
control plane offers, read the schema its XRD declares and filled it in.
See [examples/demo](examples/demo) for a control plane you can try it on
without a cloud account.

## Contents

- [Why not a generic Kubernetes MCP server?](#why-not-a-generic-kubernetes-mcp-server)
- [Quick start](#quick-start)
- [Installation](#installation)
- [Client configuration](#client-configuration)
- [Tools](#tools)
- [Read-only and write modes](#read-only-and-write-modes)
- [Prompts](#prompts)
- [Calling a tool directly](#calling-a-tool-directly)
- [Configuration](#configuration)
- [Multiple control planes](#multiple-control-planes)
- [Running in a cluster](#running-in-a-cluster)
- [Required RBAC](#required-rbac)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)

## Why not a generic Kubernetes MCP server?

A general purpose Kubernetes MCP server can already reach every object on a
Crossplane control plane: Crossplane resources are Kubernetes resources, and a
generic `resources_list` with an `apiVersion` and a `kind` will happily return
your XRDs. Access was never the problem.

The problem is that it does not know what any of it **means**, and it will
happily delete it.

| | Generic Kubernetes MCP | This server |
| --- | --- | --- |
| Reach Crossplane CRDs | Yes | Yes |
| Follow a claim to the infrastructure it created | No. It returns objects; the model has to guess which field to follow at each hop | `crossplane_resource_tree` walks `resourceRefs` for you |
| Say which resource is actually at fault | No | `crossplane_diagnose` finds the deepest failure, not the symptom at the top |
| Notice infrastructure changed outside Crossplane | No. It can return `spec` and `status` but has no idea they are meant to match | `crossplane_drift_detect` diffs desired against observed |
| Explain why a delete is hanging | No | `crossplane_deleting_resources` names the Usage, finalizer or provider holding it |
| Say what a delete would destroy first | No | `crossplane_impact` reports the blast radius before you act |
| Simulate a change without touching the cluster | No | `crossplane_composition_render` runs the function pipeline offline |
| Write to your cluster | Yes, always: create, update, delete, exec | Only if you ask. Off by default, never deletes, limited to Crossplane kinds, and it provisions through your platform APIs rather than writing managed resources by hand |

That last row matters more here than it does for ordinary Kubernetes work. On a
Crossplane control plane a deleted object is not a pod that a ReplicaSet will
recreate, it is a production database. Point this server at production and it
cannot mutate anything; point it at a development control plane with
`--read-only=false` and it still cannot delete a resource, and cannot touch a
Secret, a Deployment or an RBAC rule at all.

Underneath, the Crossplane knowledge this server encodes is:

- It discovers resources by Crossplane **category** (`managed`, `composite`,
  `claim`), so it works with every provider without being taught about any of
  them.
- It reads `Ready`/`Synced` on resources and `Installed`/`Healthy` on packages,
  and explains the difference to the model.
- It walks `resourceRefs` to build the composition tree, the same view as
  `crossplane beta trace`.
- It knows that drift on a paused or `Observe`-only resource is never corrected,
  which is the difference between a warning and a non-event.
- It supports both Crossplane v1 and v2 layouts, including namespaced composite
  resources and the `spec.crossplane` reference location.

## Quick start

```shell
go install github.com/ravibagri5/crossplane-mcp-server/cmd/crossplane-mcp-server@latest

# See what this build exposes
crossplane-mcp-server tools

# Check it can reach your control plane
crossplane-mcp-server call crossplane_status
```

Then add it to your MCP client (see [Client configuration](#client-configuration))
and ask it about your control plane.

## Installation

### Requirements

- **Go 1.26 or newer**, if you install from source or with `go install`. The
  pre-built binaries and the container image have no such requirement.
- Access to a Kubernetes cluster with Crossplane installed. Any version of
  Crossplane v1 or v2 works.
- Optional: the [crossplane CLI](https://docs.crossplane.io/latest/cli) and a
  container runtime, used only by `crossplane_composition_render`. Rendering
  executes the composition function pipeline, which cannot be done through the
  Kubernetes API. Every other tool needs nothing beyond API access, and
  `crossplane_composition_validate` covers most of the same ground without a
  container runtime.

### Go install

```shell
go install github.com/ravibagri5/crossplane-mcp-server/cmd/crossplane-mcp-server@latest
```

### Container image

```shell
docker run --rm -i \
  -v "${HOME}/.kube:/home/nonroot/.kube:ro" \
  ghcr.io/ravibagri5/crossplane-mcp-server:latest
```

### Binaries

Pre-built binaries for Linux, macOS and Windows are attached to every
[release](https://github.com/ravibagri5/crossplane-mcp-server/releases).
These are the easiest option if you do not have a recent Go toolchain.

### From source

```shell
git clone https://github.com/ravibagri5/crossplane-mcp-server.git
cd crossplane-mcp-server
make build
./bin/crossplane-mcp-server tools
```

### Registries

This server is listed in:

- [Official MCP Registry](https://registry.modelcontextprotocol.io/v0.1/servers?search=io.github.ravibagri5/crossplane-mcp-server)
  as `io.github.ravibagri5/crossplane-mcp-server`, which is where MCP clients
  look it up.
- [Smithery](https://smithery.ai/servers/ravibagri5/crossplane-mcp-server), which
  also offers one-click installation into a client.
- [pkg.go.dev](https://pkg.go.dev/github.com/ravibagri5/crossplane-mcp-server)
  for the Go package documentation.

## Client configuration

### Claude Desktop, Claude Code, Cursor, Windsurf

```json
{
  "mcpServers": {
    "crossplane": {
      "command": "crossplane-mcp-server",
      "args": ["--clusters", "staging,production", "--context", "staging"],
      "env": {
        "PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
        "HOME": "/Users/you"
      }
    }
  }
}
```

`PATH` and `HOME` matter whenever a kubeconfig context authenticates through an
exec plugin such as `kubelogin` or `aws`. Desktop applications launch servers
with a near-empty environment, so without them the plugin is either not found
or cannot read its token cache.

### Goose

In `~/.config/goose/config.yaml`:

```yaml
extensions:
  crossplane:
    enabled: true
    type: stdio
    cmd: /path/to/crossplane-mcp-server
    args: ["--clusters", "staging,production", "--context", "staging"]
    envs:
      PATH: /opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin
      HOME: /Users/you
    timeout: 300
```

Add `--read-only=false` to `args` to let goose provision as well as inspect.
[examples/demo](examples/demo) walks through that end to end on a throwaway
cluster.

### VS Code

Add to `.vscode/mcp.json` in your workspace:

```json
{
  "servers": {
    "crossplane": {
      "type": "stdio",
      "command": "crossplane-mcp-server",
      "args": ["--clusters", "staging,production"]
    }
  }
}
```

### Container based clients

```json
{
  "mcpServers": {
    "crossplane": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i",
        "-v", "${HOME}/.kube:/home/nonroot/.kube:ro",
        "ghcr.io/ravibagri5/crossplane-mcp-server:latest"
      ]
    }
  }
}
```

## Tools

Run `crossplane-mcp-server tools` to print this list from your build.

### `resources`

Managed resources, composite resources and claims.

| Tool | What it answers |
| --- | --- |
| `crossplane_managed_resources_summary` | How many managed resources exist, by kind, and how many are Ready and Synced |
| `crossplane_managed_resources_list` | Which managed resources exist, optionally only the failing ones |
| `crossplane_composite_resources_list` | Which composite resources (XRs) exist and which Composition each selected |
| `crossplane_claims_list` | Which claims exist and which composite each is bound to |
| `crossplane_resource_get` | Everything about one resource: conditions, external name, events, manifest |
| `crossplane_resource_tree` | The composition tree below a claim or composite, with per-resource status |
| `crossplane_resource_events` | The events Crossplane recorded against one resource |
| `crossplane_diagnose` | Why a resource is not Ready, and which resource is actually at fault |
| `crossplane_drift_detect` | Which infrastructure no longer matches its declared spec, and whether that will be corrected |

### `packages`

| Tool | What it answers |
| --- | --- |
| `crossplane_providers_list` | Which providers are installed and healthy |
| `crossplane_functions_list` | Which composition functions are installed and healthy |
| `crossplane_configurations_list` | Which configurations are installed and healthy |
| `crossplane_package_get` | One package plus its revisions, where image pull and dependency errors appear |

### `compositions`

| Tool | What it answers |
| --- | --- |
| `crossplane_xrds_list` | Which platform APIs this control plane offers |
| `crossplane_xrd_schema` | The fields a platform API takes, with a ready-to-edit example manifest |
| `crossplane_compositions_list` | Which Compositions exist and what pipeline they run |
| `crossplane_composition_get` | The full definition of one Composition |
| `crossplane_composition_validate` | Why a Composition does not work, without running anything |
| `crossplane_composition_render` | What a Composition would actually create, as a dry run |

### `config`

How the control plane itself is configured.

| Tool | What it answers |
| --- | --- |
| `crossplane_environment_configs_list` | Which EnvironmentConfigs exist and what data they hold |
| `crossplane_deployment_runtime_configs_list` | Which runtime configs exist and which packages use them |
| `crossplane_managed_resource_definitions_list` | Which managed resource kinds are Active, on Crossplane v2 |
| `crossplane_managed_resource_activation_policies_list` | Which policies activate those definitions |

### `diagnostics`

| Tool | What it answers |
| --- | --- |
| `crossplane_clusters_list` | Which control planes this server can reach |
| `crossplane_status` | The overall health of the control plane in one call |
| `crossplane_unhealthy_resources` | Everything that is currently failing, and why |
| `crossplane_deleting_resources` | What is stuck deleting, and what is holding it up |
| `crossplane_usages_list` | What is protected from deletion, and what needs it |
| `crossplane_impact` | What a deletion would destroy, and whether it would be blocked |
| `crossplane_api_resources` | The Crossplane API surface, to find exact kinds and groups |

### `provisioning`

Withheld unless the server runs with `--read-only=false`. See
[Read-only and write modes](#read-only-and-write-modes).

| Tool | What it does |
| --- | --- |
| `crossplane_database_create` | Asks the control plane's own database API for a database, filling in the fields its XRD declares |
| `crossplane_workload_create` | The same for a workload, app or service |
| `crossplane_resource_apply` | Applies a Crossplane manifest, for XRDs, Compositions and specs you built yourself |

Expose a subset with `--toolsets`:

```shell
crossplane-mcp-server --toolsets diagnostics,packages
```

## Read-only and write modes

The server starts read-only. In that mode the write tools are never registered,
so `tools/list` does not mention them and a call to one comes back as "no tool
named": there is nothing for a model to be talked into.

```shell
# Read-only. The default, and what to use against production.
crossplane-mcp-server

# Writes enabled, for a development control plane.
crossplane-mcp-server --read-only=false
```

What stays true even with writes enabled:

- **Nothing deletes.** Writes create and update, and that is the whole list.
  There is no delete tool and no `Delete` call anywhere in the codebase, so the
  worst outcome of a confused assistant is a resource you did not want, not one
  you did. `crossplane_impact` still tells you what a deletion *would* destroy,
  and you run the deletion yourself.
- **Only Crossplane kinds can be written.** The write path checks the resource
  against Crossplane's categories and API groups and refuses everything else,
  so the server cannot create a Secret, edit a Deployment or grant itself RBAC
  no matter what it is asked.
- **Provisioning goes through your platform APIs.** `crossplane_database_create`
  reads the XRD and sets the fields it declares; it does not invent a managed
  resource and it does not set fields the API has never heard of. Values with
  nowhere to go are reported back rather than dropped.
- **Everything is a server-side apply**, under the field manager
  `crossplane-mcp-server`, so a retry updates rather than duplicates and
  `kubectl apply` keeps working alongside it. Every object gets the label
  `app.kubernetes.io/created-by=crossplane-mcp-server`.
- **`dryRun` is available on every write tool**, which asks the API server to
  validate the manifest without persisting it.
- **RBAC still decides.** `--read-only=false` cannot grant permissions the
  credentials do not have. `deploy/rbac-write.yaml` is a starting point that
  allows create and update on your platform APIs and nothing else — not even
  `delete`.

## Prompts

Tools tell a model what it *can* do. Prompts tell it the order an experienced
operator would do things in, so it does not have to rediscover on every
conversation that diagnosing a claim starts at the claim and not at the managed
resource that looks angriest.

Most clients surface these as slash commands or a prompt picker.

| Prompt | What it does |
| --- | --- |
| `diagnose_resource` | Walks a failing resource down to the provider error and proposes the fix |
| `control_plane_review` | Produces a health report ordered by what needs attention first |
| `explain_platform_api` | Explains what a platform API offers and how to ask for one |
| `assess_deletion` | Works out the blast radius of a deletion before anyone runs it |

## Calling a tool directly

`call` runs one tool and prints what it returns, without an MCP client in the
way. Use it to check the server can reach your cluster, and to see what a tool
really returns rather than what a model says it returned.

```shell
# No arguments
crossplane-mcp-server call crossplane_status

# Arguments are the same JSON an MCP client would send
crossplane-mcp-server call crossplane_managed_resources_list '{"status":"not-ready"}'
crossplane-mcp-server call crossplane_diagnose '{"kind":"Bucket","name":"app-data"}'

# The structured payload the model receives, instead of the text rendering
crossplane-mcp-server call crossplane_status --json

# Against another control plane
crossplane-mcp-server call crossplane_status --context prod

# Write tools need the mode as well, and dryRun shows what would be submitted
crossplane-mcp-server --read-only=false call crossplane_database_create \
  '{"name":"orders-db","size":"small","dryRun":true}'
```

Run `crossplane-mcp-server tools --json` to see the exact arguments a tool
accepts.

## Configuration

| Flag | Default | Description |
| --- | --- | --- |
| `--kubeconfig` | `$KUBECONFIG`, then `~/.kube/config`, then in-cluster | Path to a kubeconfig file |
| `--context` | current context | Kubeconfig context used when a tool does not name a cluster |
| `--clusters` | every context | Comma separated contexts to expose as targets |
| `--namespace` | context namespace, else `default` | Default namespace for namespaced resources |
| `--toolsets` | all | Comma separated toolsets to expose |
| `--read-only` | `true` | Withhold every tool that changes the control plane. `--read-only=false` enables create and update; nothing deletes in either mode |
| `--http-address` | *(unset)* | Serve streamable HTTP on this address instead of stdio |
| `--log-level` | `info` | `debug`, `info`, `warn` or `error`. Logs always go to stderr |
| `--tool-timeout` | `2m` | Maximum time a single tool call may run. `0` disables |
| `--version` | | Print the version and exit |

## Multiple control planes

One server can talk to several control planes. Every tool takes an optional
`cluster` argument naming one of them, and `crossplane_clusters_list` tells a
model which are available.

```shell
crossplane-mcp-server --clusters staging,production --context staging
```

> Ask your assistant *"is anything failing in production?"* and it passes
> `cluster: "production"`; omit the cluster and it uses `--context`.

**Use `--clusters`.** Without it every context in your kubeconfig becomes a
target, which on a machine with a few hundred contexts means an assistant could
reach a production cluster when you meant a sandbox. Naming the handful you
work with is both faster and safer.

Clients are created lazily and cached, so an unreachable cluster does not stop
the others from working, and listing clusters costs nothing.

### Credentials

| Source | How it works |
| --- | --- |
| Kubeconfig context | Used as-is, including contexts that authenticate through an exec plugin |
| Cloud identity (AKS, EKS, GKE) | Works through the exec plugin the kubeconfig already declares, such as `kubelogin` or `aws` |
| Service account | Used automatically when there is no kubeconfig, which is the case for the in-cluster deployment |

Exec plugins are ordinary executables, so a server launched by a desktop
application needs `PATH` to include them, and `HOME` so they can find their
own token cache. Most MCP clients start servers with a near-empty environment,
which is the usual reason a cluster works in a terminal but not in the client:

```json
{
  "command": "crossplane-mcp-server",
  "args": ["--clusters", "staging,production"],
  "env": {
    "PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
    "HOME": "/Users/you"
  }
}
```

## Running in a cluster

Serve the streamable HTTP transport when the server runs inside the control
plane it inspects:

```shell
crossplane-mcp-server --http-address :8080
```

The MCP endpoint is `/mcp` and a liveness endpoint is served at `/healthz`. The
server uses the pod's service account when no kubeconfig is present. Manifests
are in [deploy/](deploy/).

> The HTTP transport has no built-in authentication. Put it behind an
> authenticating proxy, or keep it on a private network. See [SECURITY.md](SECURITY.md).

## Required RBAC

The server only ever reads. A cluster role that covers every tool:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: crossplane-mcp-server
rules:
  # Discovery, so the server can find managed and composite resource kinds.
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list"]
  # Everything Crossplane owns.
  - apiGroups: ["*.crossplane.io"]
    resources: ["*"]
    verbs: ["get", "list"]
  # Managed resources, which live in provider-specific API groups.
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list"]
```

If you would rather not grant a cluster-wide read, `deploy/rbac-minimal.yaml`
narrows the permissions at the cost of some tools returning warnings.

A server started with `--read-only=false` needs write verbs as well, and should
only be given them on the platform APIs an assistant is meant to use.
`deploy/rbac-write.yaml` grants `create`, `update` and `patch` on a named list
of API groups. It grants no `delete`, because no tool deletes, and leaves out
packages: installing a Provider runs somebody else's code in your cluster.

## Contributing

Contributions are very welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md),
which covers the development workflow, how to add a tool, and the sign-off
requirement. Good first issues are labelled
[`good first issue`](https://github.com/ravibagri5/crossplane-mcp-server/labels/good%20first%20issue).

Two things to know before you open a pull request:

- Pull requests target `develop`, never `main`. `main` only receives `release/*`
  and `hotfix/*` branches, so that every change ships through a release
  candidate first. See [docs/branching.md](docs/branching.md).
- Questions and half-formed ideas belong in
  [Discussions](https://github.com/ravibagri5/crossplane-mcp-server/discussions),
  not the issue tracker. A maintainer will open the issue once the shape is
  agreed.

Where the project is going, including write support, auditing and scanning, is
in [ROADMAP.md](ROADMAP.md).

This project follows the [Crossplane Code of Conduct](CODE_OF_CONDUCT.md) and is
governed as described in [GOVERNANCE.md](GOVERNANCE.md).

## Security

Please report vulnerabilities privately. See [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).

`crossplane-mcp-server` is a community project and is not an official
Crossplane or CNCF project. Crossplane is a registered trademark of The Linux
Foundation.
