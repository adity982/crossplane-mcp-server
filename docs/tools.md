# Tool reference

Generated from the tool definitions. Run `crossplane-mcp-server tools` to print
the list from your own build.

Every tool is read-only.

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

---

## Toolset: `diagnostics`

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

### `crossplane_api_resources`

The Crossplane API surface, for discovering exact kinds and groups.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `category` | enum | no | `all`, `managed`, `composite`, `claim` |
| `group` | string | no | Restrict to one API group |
| `search` | string | no | Case insensitive substring of the kind or group |
