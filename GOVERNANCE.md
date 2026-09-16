# Governance

This document describes how `crossplane-mcp-server` is run. It is modelled on
the [Crossplane governance](https://github.com/crossplane/crossplane/blob/main/GOVERNANCE.md)
so that the project is easy to donate to `crossplane-contrib`, and eventually
to propose as part of the Crossplane project proper.

## Principles

- **Open.** Design discussion happens in public issues and pull requests. If a
  decision was made somewhere else, it gets written down here.
- **Read-only by default.** Tools that mutate a control plane are withheld
  unless an operator explicitly enables them, and adding one requires an
  accepted design proposal that covers consent and auditability. The default
  is a load-bearing property, not a temporary limitation.
- **Small dependency footprint.** Every dependency is a supply chain risk for
  everyone who installs the binary.
- **Works with any provider.** Nothing in this codebase may hard-code knowledge
  of a specific Crossplane provider. Resources are discovered by category.

## Roles

### Contributor

Anyone who files an issue, comments on one, or opens a pull request. No
onboarding required.

### Reviewer

A contributor with a track record of good reviews. Reviewers are listed in
[MAINTAINERS.md](MAINTAINERS.md) and their approval counts toward merging, but
they cannot merge themselves.

Becoming a reviewer: have several non-trivial pull requests merged, then ask a
maintainer, or be nominated by one. Maintainers decide by lazy consensus.

### Maintainer

A reviewer who additionally has write access and is responsible for the health
of the project: triage, releases, and security response. Maintainers are listed
in [MAINTAINERS.md](MAINTAINERS.md).

Becoming a maintainer: sustained contribution over at least three months,
demonstrated good judgement in reviews, nominated by an existing maintainer, and
approved by lazy consensus of the existing maintainers over a seven day period.

Maintainers who have been inactive for six months move to emeritus. This is not
a judgement; people's circumstances change. Emeritus maintainers can return by
asking.

## Decision making

Most decisions are made by **lazy consensus**: a proposal is made in an issue or
pull request, and if no maintainer objects within a reasonable period it is
accepted. In practice this means most changes are merged after one approving
review.

Changes that need more than lazy consensus:

- Adding a dependency.
- Adding, removing or renaming a tool, or making a breaking change to a tool's
  arguments. These are breaking changes for every user's prompts.
- Anything that would let the server mutate a control plane.
- Changes to this document, to `MAINTAINERS.md`, or to the licence.

For these, a maintainer opens an issue describing the change, and it needs
approval from a majority of maintainers with no outstanding objections.

If consensus cannot be reached, the maintainers vote. Each maintainer has one
vote and a simple majority decides. Ties mean the proposal does not proceed.

## Design proposals

Substantial changes start with a short document in `design/`, following the
pattern Crossplane uses: the problem, the proposal, the alternatives that were
considered, and what is explicitly out of scope. A proposal is accepted when it
is merged.

## Code of Conduct

Code of Conduct violations are handled by the maintainers in the first
instance, escalating to the CNCF Code of Conduct Committee as described in
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Licensing

All contributions are made under the Apache License 2.0 and must be signed off
under the [Developer Certificate of Origin](https://developercertificate.org/).
There is no CLA.

## Upstream relationship

`crossplane-mcp-server` is a community project. It is not an official
Crossplane or CNCF project, and it does not speak for the Crossplane
maintainers. The long term aim is to donate the project to the Crossplane
organisation once it has proven useful and has a maintainer group that is not
dependent on any single person or employer. Nothing in this project should
create an obligation on the Crossplane maintainers in the meantime.
