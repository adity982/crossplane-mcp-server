# crossplane-mcp-server

[![CI](https://github.com/crossplane-contrib/crossplane-mcp-server/actions/workflows/ci.yaml/badge.svg)](https://github.com/crossplane-contrib/crossplane-mcp-server/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/crossplane-contrib/crossplane-mcp-server)](https://goreportcard.com/report/github.com/crossplane-contrib/crossplane-mcp-server)
[![Go Reference](https://pkg.go.dev/badge/github.com/crossplane-contrib/crossplane-mcp-server.svg)](https://pkg.go.dev/github.com/crossplane-contrib/crossplane-mcp-server)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A [Model Context Protocol](https://modelcontextprotocol.io) server that lets AI
assistants understand a [Crossplane](https://crossplane.io) control plane.

Ask your assistant *"how many managed resources do I have and is anything
broken?"* and it will answer from your actual control plane, with the reason for
every failure, instead of guessing.

```text
> Why is the app-db claim not ready?

  crossplane_resource_tree(kind="PostgreSQLInstance", name="app-db")

  PostgreSQLInstance/app-db  READY=False SYNCED=True
  └─ XPostgreSQLInstance/app-db-x7k2p  READY=False SYNCED=True
     ├─ Instance/app-db-rds  READY=False SYNCED=False  create failed: InvalidParameterValue
     └─ SecurityGroup/app-db-sg  READY=True SYNCED=True

  The RDS Instance app-db-rds is failing. Let me look at its events.

  crossplane_resource_events(kind="Instance", name="app-db-rds")

  The instance class db.t2.mega does not exist. Fix the instanceClass field in
  your Composition and the claim will reconcile.
```

Every tool is **read-only**. This server cannot create, update or delete
anything on your control plane.

## Contents

- [Why](#why)
- [Quick start](#quick-start)
- [Installation](#installation)
- [Client configuration](#client-configuration)
- [Tools](#tools)
- [Configuration](#configuration)
- [Running in a cluster](#running-in-a-cluster)
- [Required RBAC](#required-rbac)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)

## Why

A general purpose Kubernetes MCP server can list the objects on a Crossplane
control plane, but it does not know what they mean. It cannot tell you that a
`Bucket` is a managed resource, that `Synced=False` points at your composition
rather than at AWS, or that a claim's real problem is three levels down the
composition tree.

This server encodes that knowledge:

- It discovers resources by Crossplane **category** (`managed`, `composite`,
  `claim`), so it works with every provider without being taught about any of
  them.
- It reads `Ready`/`Synced` on resources and `Installed`/`Healthy` on packages,
  and explains the difference to the model.
- It walks `resourceRefs` to build the composition tree, the same view as
  `crossplane beta trace`.
- It supports both Crossplane v1 and v2 layouts, including namespaced composite
  resources and the `spec.crossplane` reference location.

## Quick start

```shell
go install github.com/crossplane-contrib/crossplane-mcp-server/cmd/crossplane-mcp-server@latest

# Check it can see your control plane
crossplane-mcp-server tools
```

Then add it to your MCP client (see [Client configuration](#client-configuration))
and ask it about your control plane.

## Installation

### Go install

```shell
go install github.com/crossplane-contrib/crossplane-mcp-server/cmd/crossplane-mcp-server@latest
```

### Homebrew

```shell
brew install crossplane-contrib/tap/crossplane-mcp-server
```

### Container image

```shell
docker run --rm -i \
  -v "${HOME}/.kube:/home/nonroot/.kube:ro" \
  ghcr.io/crossplane-contrib/crossplane-mcp-server:latest
```

### Binaries

Pre-built binaries for Linux, macOS and Windows are attached to every
[release](https://github.com/crossplane-contrib/crossplane-mcp-server/releases).

## Client configuration

### Claude Desktop, Claude Code, Cursor, Windsurf

```json
{
  "mcpServers": {
    "crossplane": {
      "command": "crossplane-mcp-server",
      "args": ["--context", "my-control-plane"]
    }
  }
}
```

### VS Code

Add to `.vscode/mcp.json` in your workspace:

```json
{
  "servers": {
    "crossplane": {
      "type": "stdio",
      "command": "crossplane-mcp-server",
      "args": []
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
        "ghcr.io/crossplane-contrib/crossplane-mcp-server:latest"
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
| `crossplane_compositions_list` | Which Compositions exist and what pipeline they run |
| `crossplane_composition_get` | The full definition of one Composition |

### `diagnostics`

| Tool | What it answers |
| --- | --- |
| `crossplane_status` | The overall health of the control plane in one call |
| `crossplane_unhealthy_resources` | Everything that is currently failing, and why |
| `crossplane_api_resources` | The Crossplane API surface, to find exact kinds and groups |

Expose a subset with `--toolsets`:

```shell
crossplane-mcp-server --toolsets diagnostics,packages
```

## Configuration

| Flag | Default | Description |
| --- | --- | --- |
| `--kubeconfig` | `$KUBECONFIG`, then `~/.kube/config`, then in-cluster | Path to a kubeconfig file |
| `--context` | current context | Kubeconfig context to use |
| `--namespace` | context namespace, else `default` | Default namespace for namespaced resources |
| `--toolsets` | all | Comma separated toolsets to expose |
| `--http-address` | *(unset)* | Serve streamable HTTP on this address instead of stdio |
| `--log-level` | `info` | `debug`, `info`, `warn` or `error`. Logs always go to stderr |
| `--tool-timeout` | `2m` | Maximum time a single tool call may run. `0` disables |
| `--version` | | Print the version and exit |

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

## Contributing

Contributions are very welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md),
which covers the development workflow, how to add a tool, and the sign-off
requirement. Good first issues are labelled
[`good first issue`](https://github.com/crossplane-contrib/crossplane-mcp-server/labels/good%20first%20issue).

This project follows the [Crossplane Code of Conduct](CODE_OF_CONDUCT.md) and is
governed as described in [GOVERNANCE.md](GOVERNANCE.md).

## Security

Please report vulnerabilities privately. See [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).

`crossplane-mcp-server` is a community project and is not an official
Crossplane or CNCF project. Crossplane is a registered trademark of The Linux
Foundation.
