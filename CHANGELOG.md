# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tool names and their arguments are part of the public API. Renaming or removing
a tool, or making an optional argument required, is a breaking change.

## [Unreleased]

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
