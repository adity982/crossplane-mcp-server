# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tool names and their arguments are part of the public API. Renaming or removing
a tool, or making an optional argument required, is a breaking change.

## [Unreleased]

### Added

- XRD versions are exposed as live MCP resources at stable
  `crossplane://xrd/{group}/{kind}/{version}` URIs, with list-change
  notifications when definitions appear or disappear.

## [0.2.0] - 2026-09-16

### Added

- Opt-in write support. The new `provisioning` toolset creates a database or a
  workload through the control plane's own platform APIs
  (`crossplane_database_create`, `crossplane_workload_create`) and applies a
  Crossplane manifest (`crossplane_resource_apply`). The create tools find the
  claim or composite kind the control plane offers, read the schema from the
  XRD that defines it and set only the fields it declares, reporting anything
  with nowhere to go. Writes create and update only: no tool deletes, and there
  is no delete call in the codebase.
- `--read-only`, defaulting to `true`. The write tools are not registered at
  all unless it is set to `false`, so a client does not see them and cannot
  call them. Even with writes enabled the server refuses to touch anything that
  is not a Crossplane kind, all writes are server-side applies under the field
  manager `crossplane-mcp-server`, and every write tool takes `dryRun`.
- `examples/demo`, a control plane built on provider-nop that provisions fake
  databases and workloads, with a script for demonstrating the write tools
  through goose. No cloud account needed.
- `deploy/rbac-write.yaml`, a least-privilege ClusterRole for a server running
  with writes enabled.
- One server can now target several control planes. Every tool takes an
  optional `cluster` argument, `--clusters` chooses which kubeconfig contexts
  are exposed, and `crossplane_clusters_list` reports what is available.
  Clients are built lazily and cached, so an unreachable cluster does not stop
  the others from working.
- `crossplane_xrd_schema` reports the fields a platform API takes, with an
  example manifest, so a composite resource can be written without guessing.
- `crossplane_composition_validate` checks a Composition against the live
  control plane without running anything: that its composite kind exists, that
  every function it calls is installed and Healthy, and that its step names are
  unique.
- `crossplane_composition_render` runs a Composition's function pipeline as a
  dry run, reading the Composition and its functions from the live control
  plane. Requires the crossplane CLI and a container runtime.
- `crossplane_deleting_resources` finds resources stuck deleting and explains
  what is holding each one up.
- `crossplane_usages_list` reports what is protected from deletion and what
  needs it.
- New `config` toolset covering EnvironmentConfigs, DeploymentRuntimeConfigs,
  ManagedResourceDefinitions and ManagedResourceActivationPolicies.

### Changed

- The global flags (`--kubeconfig`, `--context`, `--clusters`, `--namespace`,
  `--toolsets`, `--read-only`, `--log-level`, `--tool-timeout`) are now
  persistent, so `call`, `tools` and `prompts` accept them too. Previously they
  were only accepted by the root command, which meant `call` always used the
  current context.

### Fixed

- Composition trees were empty on Crossplane v2. Composed resource references
  moved to `spec.crossplane.resourceRefs`, so `crossplane_resource_tree`
  reported only the root resource.
- A client closing its end of the stdio pipe is no longer reported as an error.
  Every MCP client does this on shutdown, so the server exited non-zero on a
  normal disconnect.
- The container image now runs as a numeric UID, so it starts under a pod
  security context that requires `runAsNonRoot`.

## [0.1.0] - 2026-09-12

First release. Everything below is new.

### Added

- `resources` toolset: `crossplane_managed_resources_summary`,
  `crossplane_managed_resources_list`, `crossplane_composite_resources_list`,
  `crossplane_claims_list`, `crossplane_resource_get`,
  `crossplane_resource_tree` and `crossplane_resource_events`.
- `packages` toolset: `crossplane_providers_list`,
  `crossplane_functions_list`, `crossplane_configurations_list` and
  `crossplane_package_get`.
- `compositions` toolset: `crossplane_xrds_list`,
  `crossplane_compositions_list` and `crossplane_composition_get`.
- `diagnostics` toolset: `crossplane_status`,
  `crossplane_unhealthy_resources` and `crossplane_api_resources`.
- stdio and streamable HTTP transports.
- `--toolsets` for exposing a subset of the tools.
- `tools` subcommand for listing the tool surface without a cluster.

### Notes

- Requires Go 1.26 or newer to build from source, which `k8s.io/client-go`
  v0.37 depends on. The released binaries and container image are unaffected.

[Unreleased]: https://github.com/ravibagri5/crossplane-mcp-server/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/ravibagri5/crossplane-mcp-server/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/ravibagri5/crossplane-mcp-server/releases/tag/v0.1.0
