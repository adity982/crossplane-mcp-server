# Tool reference

Generated from the tool definitions. Run `crossplane-mcp-server tools` to print
the list from your own build.

Every tool is read-only except the `provisioning` toolset, which is withheld
unless the server was started with `--read-only=false`. Those tools are marked
**write** below, and `crossplane-mcp-server tools` marks them `[write]`. No
tool in this server deletes anything, in either mode.

## The `cluster` argument

Every tool accepts an optional `cluster` argument naming the control plane to
query. Omit it to use the server's default, set by `--context`. It is added to
every tool's schema by the server rather than declared tool by tool, so it
behaves identically everywhere and is not repeated in the tables below.

Call `crossplane_clusters_list` to discover the valid names.

## Common workflows

The tools are designed to chain. These are the paths worth knowing.

**Triage a control plane**

```text
crossplane_status  →  crossplane_unhealthy_resources  →  crossplane_resource_get
```

**Work out why a claim is not ready**

```text
crossplane_resource_tree     find the deepest resource that is not Ready
crossplane_resource_get      read its conditions
crossplane_resource_events   the provider's real error usually lives here
```

**A kind you expect does not exist**

```text
crossplane_providers_list                      is the provider Healthy?
crossplane_managed_resource_definitions_list   is the definition Active?  (v2)
crossplane_api_resources                       what is actually installed
```

**Author against a platform API**

```text
crossplane_xrds_list  →  crossplane_xrd_schema  →  crossplane_composition_render
```

**A delete is hanging**

```text
crossplane_deleting_resources  →  crossplane_usages_list
```

**Provision something** (needs `--read-only=false`)

```text
crossplane_database_create   with dryRun: true, to see the manifest
crossplane_database_create   again, without it
crossplane_resource_tree     watch it come up; creating is not the same as Ready
```

**Ask about another control plane**

```text
crossplane_clusters_list  →  any tool, with cluster: "<name>"
```

---

## Toolset: `resources`

Managed resources, composite resources (XRs) and claims.

### `crossplane_managed_resources_summary`

Count the managed resources in the control plane, broken down by kind, with how
many are Ready and Synced.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `group` | string | no | API group, to look at one provider |
| `namespace` | string | no | Namespace, for namespaced managed resources |

Use this first for "how many managed resources do I have?". It returns counts,
not individual resources.

### `crossplane_managed_resources_list`

List managed resources with their conditions and failure reasons.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | no | A single kind, e.g. `Bucket` |
| `group` | string | no | API group, to disambiguate a kind |
| `namespace` | string | no | Namespace to search |
| `labelSelector` | string | no | Standard Kubernetes label selector |
| `status` | enum | no | `any`, `ready`, `not-ready`, `not-synced` |
| `limit` | integer | no | Per-kind cap, default 500 |

### `crossplane_composite_resources_list`

List composite resources (XRs) with the Composition each one selected. Same
arguments as `crossplane_managed_resources_list`.

### `crossplane_claims_list`

List claims with the composite each is bound to. Same arguments as
`crossplane_managed_resources_list`.

On a Crossplane v2 control plane an empty result is expected: v2 deprecates
claims in favour of namespaced composite resources.

### `crossplane_resource_get`

Describe one resource: conditions, external name, Composition, events and
optionally the full manifest.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | **yes** | Kind of the resource |
| `name` | string | **yes** | Name of the resource |
| `group` | string | no | API group, to disambiguate |
| `namespace` | string | no | Namespace, for namespaced resources |
| `manifest` | boolean | no | Include the full YAML, default `false` |

### `crossplane_resource_tree`

The composition tree below a claim or composite, with per-resource status. The
equivalent of `crossplane beta trace`.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | **yes** | Kind of the root claim or composite |
| `name` | string | **yes** | Name of the root |
| `group` | string | no | API group, to disambiguate |
| `namespace` | string | no | Namespace of the root |

Returns the tree plus a flat list of everything in it that is not Ready.

### `crossplane_resource_events`

The Kubernetes events recorded against one resource. This is usually where the
real cloud provider error lives when the conditions only say `ReconcileError`.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `kind` | string | **yes** | Kind of the resource |
| `name` | string | **yes** | Name of the resource |
| `group` | string | no | API group, to disambiguate |
| `namespace` | string | no | Namespace of the resource |

---

## Toolset: `packages`

### `crossplane_providers_list`

Installed providers with their Installed and Healthy conditions and active
revision.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `unhealthyOnly` | boolean | no | Only providers that are not fully healthy |

### `crossplane_functions_list`

Installed composition functions. Same arguments as
`crossplane_providers_list`.

### `crossplane_configurations_list`

Installed configurations. Same arguments as `crossplane_providers_list`.

### `crossplane_package_get`

One package plus all of its revisions. Image pull failures and unmet
dependencies surface on the revision, not on the package.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `type` | enum | **yes** | `Provider`, `Function` or `Configuration` |
| `name` | string | **yes** | Name of the package |

---

## Toolset: `compositions`

### `crossplane_xrds_list`

The CompositeResourceDefinitions installed, with the composite and claim kinds
each generates.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `unhealthyOnly` | boolean | no | Only XRDs that are not established |

### `crossplane_xrd_schema`

The API contract of one platform API: apiVersion, kind, the shape of `spec`,
which fields are required, and a minimal example manifest.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | **yes** | Name of the XRD, e.g. `xpostgresqlinstances.example.org` |
| `version` | string | no | Which version to describe. Defaults to the first served version |

Call this before writing a composite resource, and before
`crossplane_composition_render`, rather than guessing field names.

### `crossplane_compositions_list`

Compositions with the composite kind each satisfies and the function pipeline
it runs.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `compositeKind` | string | no | Only Compositions for this composite kind |

### `crossplane_composition_get`

One Composition, including its full YAML.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | **yes** | Name of the Composition |
| `manifest` | boolean | no | Include the full YAML, default `true` |

### `crossplane_composition_validate`

Static checks against the live control plane: does the composite kind exist, is
every function in the pipeline installed and Healthy, are step names unique.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | **yes** | Name of the Composition |

Needs no container runtime, so prefer it for *"why does this Composition not
work?"*. Use `crossplane_composition_render` for *"what does it produce?"*.

### `crossplane_composition_render`

Runs the function pipeline against a composite resource and returns what it
would create, without touching the control plane. The equivalent of
`crossplane render`.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `composition` | string | **yes** | Name of the Composition to render |
| `xr` | string | **yes** | YAML of the composite resource, including apiVersion, kind, metadata.name and spec |
| `manifests` | boolean | no | Include the full rendered YAML, default `true` |

The Composition and its functions are read from the **live control plane**, so
the result reflects this cluster rather than local files.

**Requires** the `crossplane` CLI and a container runtime on the machine running
the server. If either is missing the tool says so and points at
`crossplane_composition_validate`.

---

## Toolset: `config`

How the control plane itself is configured.

### `crossplane_environment_configs_list`

EnvironmentConfigs and the data keys each holds. Compositions read these at
render time for values that differ between environments.

No arguments.

### `crossplane_deployment_runtime_configs_list`

DeploymentRuntimeConfigs and which providers or functions reference each one. A
config nothing references is a common reason settings appear not to apply.

No arguments.

### `crossplane_managed_resource_definitions_list`

ManagedResourceDefinitions on a Crossplane v2 control plane. An `Inactive`
definition installs no CRD, so the kind does not exist — check here first when a
provider is Healthy but an expected kind is missing.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `state` | enum | no | `Active` or `Inactive` |

### `crossplane_managed_resource_activation_policies_list`

Which policies activate which ManagedResourceDefinitions, by name pattern.

No arguments.

---

## Toolset: `diagnostics`

### `crossplane_clusters_list`

The control planes this server can reach, with the credential source and API
server of each. The default is marked.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `search` | string | no | Case insensitive substring of the cluster name |
| `limit` | integer | no | Maximum number to return, default 50 |

Call this before naming a cluster you have not used yet, rather than guessing.

### `crossplane_status`

The whole control plane in one call: Crossplane version, package health, XRD
and Composition counts, and resource counts with readiness.

No arguments.

### `crossplane_unhealthy_resources`

Everything that is currently failing, with the reason.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `namespace` | string | no | Restrict to one namespace |
| `scope` | enum | no | `all`, `packages`, `managed`, `composite`, `claims` |
| `limit` | integer | no | Cap on reported failures, default 100 |

### `crossplane_deleting_resources`

Resources asked to delete that have not gone away, and what is holding each
up: a Usage protecting it, composed resources still being removed, or a
provider that has not confirmed the external resource is gone.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `namespace` | string | no | Restrict to one namespace |
| `includeRecent` | boolean | no | Include deletions younger than 30s, default `false` |

Use this whenever a delete appears to hang — `kubectl` reports success and then
nothing happens, which makes this failure hard to spot any other way.

### `crossplane_usages_list`

Usage and ClusterUsage objects: what is protected from deletion, and what needs
it.

No arguments.

### `crossplane_api_resources`

The Crossplane API surface, for discovering exact kinds and groups.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `category` | enum | no | `all`, `managed`, `composite`, `claim` |
| `group` | string | no | Restrict to one API group |
| `search` | string | no | Case insensitive substring of the kind or group |

---

## Toolset: `provisioning`

**Every tool in this toolset writes.** None of them is registered unless the
server runs with `--read-only=false`.

Two limits hold whatever the arguments say:

- **Nothing here deletes.** These tools create and update. There is no delete
  tool, and no argument that turns one of these into one. Use
  `crossplane_impact` to work out what a deletion would destroy, then run it
  yourself.
- **Only Crossplane kinds can be written.** Anything not tagged `managed`,
  `composite`, `claim` or `crossplane`, and not in a `*.crossplane.io` group,
  is refused.

All three accept `dryRun`, which asks the API server to validate the change
without persisting it.

### `crossplane_database_create`

Ask this control plane's own platform API for a database.

The tool finds the claim or composite kind whose name reads most like a
database, reads the schema from the XRD that defines it, and sets only the
fields that schema declares — under `spec.parameters` if the API puts them
there, at the top level of `spec` if not. Values the API has no field for are
reported back rather than dropped.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | **yes** | Name for the database |
| `namespace` | string | no | Namespace, defaults to the server's default |
| `engine` | enum | no | `postgresql`, `mysql`, `mariadb`, `redis` |
| `version` | string | no | Engine version, e.g. `16` |
| `size` | enum | no | `small`, `medium`, `large` |
| `storageGB` | integer | no | Storage to request, in gigabytes |
| `kind` | string | no | Kind to create, to override the discovered one |
| `apiVersion` | string | no | API version of that kind, if it is ambiguous |
| `parameters` | object | no | Extra spec fields, for the parts of the API this tool does not know about |
| `dryRun` | boolean | no | Validate without creating, default `false` |

If nothing matches, the answer lists the platform APIs that do exist. Pass
`kind` rather than guessing an `apiVersion`.

### `crossplane_workload_create`

The same, for a workload, app or service.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | **yes** | Name for the workload |
| `namespace` | string | no | Namespace, defaults to the server's default |
| `image` | string | no | Container image to run |
| `replicas` | integer | no | How many instances |
| `port` | integer | no | Port the container listens on |
| `size` | enum | no | `small`, `medium`, `large` |
| `kind`, `apiVersion`, `parameters`, `dryRun` | | no | As above |

### `crossplane_resource_apply`

Apply a manifest, creating it or updating it in place. For the things the tools
above do not cover: XRDs, Compositions, EnvironmentConfigs, or a composite
resource whose spec you wrote yourself.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `manifest` | string | **yes** | YAML or JSON. Several documents may be separated by `---` |
| `namespace` | string | no | Namespace for documents that do not name one |
| `force` | boolean | no | Take over fields another field manager owns, default `false` |
| `dryRun` | boolean | no | Validate without persisting, default `false` |

Applies are server-side, under the field manager `crossplane-mcp-server`.
Documents are applied in order, and a failure reports which of them already
went in. An apply can overwrite fields on a resource that already exists, so
read it with `crossplane_resource_get` first.
