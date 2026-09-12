# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tool names and their arguments are part of the public API. Renaming or removing
a tool, or making an optional argument required, is a breaking change.

## [Unreleased]

### Added

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

### Fixed

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

[Unreleased]: https://github.com/ravibagri5/crossplane-mcp-server/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ravibagri5/crossplane-mcp-server/releases/tag/v0.1.0
