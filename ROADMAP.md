# Roadmap

This is where the project is going. It is a statement of intent, not a promise
of dates. Everything here is open to discussion, and items move up the list
when somebody wants to work on them.

Roadmap items are tracked as [milestones][milestones] and on the [project
board][board]. Shape changes happen in [Discussions][ideas] before they become
issues.

## Contents

- [Guiding principles](#guiding-principles)
- [Themes](#themes)
- [Now — shipped](#now--shipped)
- [v0.5 — Deep diagnosis](#v05--deep-diagnosis)
- [v0.6 — Best practices and policy](#v06--best-practices-and-policy)
- [v0.7 — Security and supply chain scanning](#v07--security-and-supply-chain-scanning)
- [v0.8 — Opt-in write capabilities](#v08--opt-in-write-capabilities)
- [v0.9 — Fleet, scale and observability](#v09--fleet-scale-and-observability)
- [v1.0 — Stability](#v10--stability)
- [Later](#later)
- [Not planned](#not-planned)
- [How to influence this roadmap](#how-to-influence-this-roadmap)

## Guiding principles

Every item below is judged against these. A feature that violates one of them
does not ship, however useful it seems.

1. **Answer questions, do not replace tools.** The server exists so that a model
   can explain a control plane, not so that it can drive one.
2. **Read-only by default, always.** Anything that mutates a control plane is
   opt-in, explicit, auditable and revocable.
3. **Discovery over hard-coding.** No provider-specific knowledge. Everything
   works through the API server, XRDs, categories and conditions.
4. **The description is the API.** Tool descriptions are read by a model far
   more often than our docs are read by a human.
5. **Safe under load.** A control plane with fifty thousand managed resources
   must not be knocked over by a curious assistant.

## Themes

| Theme | What it means | Milestone |
| --- | --- | --- |
| **Diagnose** | Go from "it is broken" to "here is the cause and the fix" | v0.5 |
| **Advise** | Encode community best practice as auditable checks | v0.6 |
| **Secure** | Surface RBAC, credential, provenance and CVE risk | v0.7 |
| **Act** | Carefully scoped, consent-gated mutation | v0.8 |
| **Scale** | Fleets, pagination, streaming, telemetry | v0.9 |
| **Stabilise** | Frozen tool contract, compatibility guarantees | v1.0 |

## Now — shipped

The read-only core, which is what ships today, plus a first tier of opt-in
writes. Six toolsets; see the tool table in [README.md](README.md).

- [x] Managed resource counts, listings and status
- [x] Composite resources and claims
- [x] Composition tree traversal, the equivalent of `crossplane beta trace`
- [x] Providers, functions and configurations, with revisions
- [x] XRDs, Compositions, XRD schemas and static Composition validation
- [x] Whole-control-plane health and "what is failing" diagnostics
- [x] Single-resource diagnosis with event correlation
- [x] `Usage` and deletion-protection awareness, with deletion impact analysis
- [x] Composition rendering, the equivalent of `crossplane render`
- [x] Drift detection between desired and observed state
- [x] Prompts for diagnosis, control plane review, platform API explanation and
      deletion assessment
- [x] Per-call cluster selection, so one server can reach every context in a
      kubeconfig
- [x] stdio and streamable HTTP transports
- [x] Crossplane v1 and v2 layouts
- [x] Opt-in provisioning behind `--read-only=false`: create a database or a
      workload through the control plane's own platform APIs, and apply a
      Crossplane manifest. Withheld by default, restricted to Crossplane kinds,
      server-side apply throughout, and no deletion at all. See the first tier
      of [v0.8](#v08--opt-in-write-capabilities) for what is still missing.

## v0.5 — Deep diagnosis

**Goal:** an assistant can explain *why* a resource is stuck, not just *that* it
is stuck, and can do so without a human pasting YAML.

- [ ] **Root-cause chains.** Walk from a failing claim down through the XR, the
      Composition, the composed resources and the provider pod, and return the
      first cause rather than a list of symptoms.
- [ ] **ProviderConfig and credential resolution.** Report which
      `ProviderConfig` a managed resource uses, where its credentials come from,
      and whether that secret or workload-identity binding actually resolves.
      Credential failure is the most common cause of `Synced=False` and is
      invisible today.
- [ ] **Reconcile-loop detection.** Spot resources that flap between conditions
      or are rewritten every interval, using `resourceVersion` churn and event
      rates, and name the field causing the fight.
- [ ] **Drift explanation.** `crossplane_drift_detect` reports that a field
      differs; it should attribute each difference to the Composition, a patch,
      or an out-of-band change.
- [ ] **Provider pod correlation.** Match a failing managed resource to the
      provider pod that owns it, its restart count, its limits and the relevant
      log lines.
- [ ] **Stuck deletion analysis.** `crossplane_deleting_resources` and
      `crossplane_impact` answer this in pieces. Explain finalizers, `Usage`
      blocks and orphaned external resources as one answer.
- [ ] **Package dependency failures.** Explain why a `Configuration` will not
      install: version constraint conflicts, missing dependencies, registry
      auth.
- [ ] **XRD schemas as MCP resources.** `crossplane_xrd_schema` already returns
      the API contract as a tool call. Exposing the same thing through the MCP
      resources primitive lets a client attach it as context without spending a
      tool call, which matters once a model is writing claims.
- [ ] **Prompts for the v0.5 workflows.** Four prompts ship today. Root cause
      and upgrade readiness will need their own.

## v0.6 — Best practices and policy

**Goal:** the server can audit a control plane against practices the Crossplane
community already agrees on, and explain every finding.

- [ ] **Best-practice audit.** A tool that runs a rule set over the control
      plane and returns findings with severity, evidence and a remediation.
      Every rule cites a reason; no rule is a matter of taste.
- [ ] **Rule catalogue,** covering at least:
  - XRDs without printer columns, descriptions or a default Composition
  - Compositions not pinned by `compositionRevisionRef` where the update policy
    is `Automatic` in production
  - Managed resources with `deletionPolicy: Delete` and no `Usage` protection
  - Missing requests and limits on provider deployments
  - Providers pinned to a floating tag rather than a digest
  - Claims living in the `default` namespace
  - One `ProviderConfig` credential shared across unrelated environments
  - Patch chains that silently no-op
- [ ] **Upgrade readiness.** Report what would break on a Crossplane v1 to v2
      upgrade: namespaced XR changes, removed APIs, deprecated fields.
- [ ] **Composition review.** `crossplane_composition_validate` checks a
      Composition statically. Extend it to review a Function pipeline against
      its rendered output.
- [ ] **Custom rules.** Let operators supply a rule file so an organisation's
      own conventions are audited alongside the built-in ones.
- [ ] **Machine-readable findings.** SARIF output, so audits run in CI and
      surface in the GitHub Security tab.

## v0.7 — Security and supply chain scanning

**Goal:** answer "is this control plane safe?" with evidence, still without
mutating anything.

- [ ] **RBAC exposure scan.** Report what the server itself can see, what
      Crossplane's service accounts can do, and any `cluster-admin` grants that
      exist because somebody was in a hurry.
- [ ] **Secret exposure scan.** Find connection secrets written to
      over-permissive namespaces, and `writeConnectionSecretToRef` targets
      readable by workloads that should not read them. Locations are reported;
      values never are.
- [ ] **Package provenance.** Verify signatures and attestations on installed
      packages, report unsigned or unverifiable ones, and flag packages pulled
      from unexpected registries.
- [ ] **Vulnerability surfacing.** Correlate known CVEs for installed provider
      and function images from an external scanner's results. The server
      explains; it does not become a scanner.
- [ ] **Redaction guarantees.** A tested, documented redaction layer with an
      explicit allow-list for anything resembling a credential, plus a
      strictness flag.
- [ ] **Tenancy isolation review.** For namespaced XRs, report cross-namespace
      references and Compositions that let one tenant reach another's
      resources.
- [ ] **Audit trail.** Structured, exportable log of every tool call, its
      arguments and its caller, for people running the HTTP transport as a
      shared service.

## v0.8 — Opt-in write capabilities

**Goal:** an assistant can fix what it diagnosed, under controls an operator
would sign off on. This is the largest design risk in the project, and each tier
needs a design proposal before implementation.

The first step has shipped: `--read-only=false` registers the `provisioning`
toolset, which creates through platform APIs and applies Crossplane manifests.
It does not delete, and tier 3 below is not scheduled. Write support is still
**off by default** and the remaining tiers are still layered, enabled
independently, with enabling one never implying the next.

| Tier | Flag | Capability | Reversible |
| --- | --- | --- | --- |
| 0 | *(default)* | Read-only | n/a |
| 1 | `--allow-write=annotate` | Force reconcile, pause and resume, label and annotate | Yes |
| 2 | `--read-only=false` | Create and update claims, XRs, Compositions *(shipped)* | Mostly |
| 3 | none | Delete resources, uninstall packages. **Not planned**: the blast radius of a mistaken delete on a control plane is a production database | No |

Cross-cutting requirements, all of which must land before the tiers are split
out properly:

- [ ] **Dry run first.** Every mutating tool takes a `dryRun` argument, but it
      defaults to false and returns the submitted manifest rather than a
      server-side-apply diff of what would change.
- [ ] **Elicitation-based consent.** The client shows the human the rendered
      diff before anything is applied. No consent, no write. Today the tool
      annotations leave the decision to the client.
- [ ] **Least-privilege RBAC.** `deploy/rbac-write.yaml` is a starting point.
      Still missing: a `Role` per tier, and the server refusing to advertise a
      tool it does not have permission to perform.
- [ ] **Allow-lists.** Restrict writes by namespace, by group and kind, and by
      label selector, so a read-mostly deployment can permit exactly one
      workflow. Today the only restriction is "Crossplane kinds only".
- [x] **Field management.** All writes use server-side apply with the field
      manager `crossplane-mcp-server`, so ownership stays visible and
      reversible.
- [ ] **Audit and idempotency.** Every write is logged with its diff and carries
      a client-supplied idempotency key. Applies are idempotent today, but
      there is no key and no diff in the log.
- [ ] **Blast-radius caps.** A hard limit on objects touched per call, and a
      refusal above it.

Tier by tier:

- [ ] Tier 1: force reconcile, pause and resume — the operations that today are
      an annotation and a copy-pasted `kubectl` command.
- [ ] Tier 1: retry a failed package revision.
- [ ] Tier 2: create a claim from an XRD schema, validated against the XRD
      before submission.
- [ ] Tier 2: apply a Composition or Function pipeline change, gated on a
      successful render of the affected XRs.
- [ ] Tier 2: install, upgrade or configure a package.
- [ ] Tier 3: delete, with `Usage` and orphan analysis presented as part of
      consent.
- [ ] **Guided remediation.** Close the loop: a diagnosis from v0.5 produces a
      concrete proposed change, which enters the tier 2 consent flow.

## v0.9 — Fleet, scale and observability

**Goal:** the server is usable against real production estates, not just a
laptop cluster.

- [ ] **Fleet-wide queries.** Tools already take a `cluster` argument and
      `crossplane_clusters_list` enumerates the targets, but every call hits one
      control plane. Answer "which of my twelve control planes has a failing
      provider?" in a single call, with partial results when one is
      unreachable.
- [ ] **Upbound Spaces awareness.** Treat a space as a first-class target
      alongside kubeconfig contexts.
- [ ] **Server-side pagination and streaming** for control planes with tens of
      thousands of managed resources.
- [ ] **Caching and informers.** An optional watch-backed cache for the HTTP
      transport, so a shared server does not re-list the world per call.
- [ ] **OpenTelemetry.** Traces and metrics for people running the HTTP
      transport as a shared service.
- [ ] **Result budgeting.** Tools degrade into summaries rather than returning
      results that blow a model's context window.
- [ ] **Deployment hardening.** Helm chart, `NetworkPolicy`, distroless non-root
      image, documented HA.

## v1.0 — Stability

**Goal:** people can build on the tool contract.

- [ ] Tool names, arguments and result shapes frozen, with a deprecation policy
      of at least one minor release of overlap.
- [ ] Documented compatibility matrix across Crossplane, Kubernetes and MCP
      protocol versions.
- [ ] Conformance suite run against v1 and v2 control planes in CI.
- [ ] Reference configuration for every major MCP client.
- [ ] Performance budget enforced in CI against a synthetic fifty-thousand
      resource control plane.

## Later

Ideas that need a design proposal before anyone starts. Open an [Ideas
discussion][ideas] if you want to pick one up.

- **Operations support.** Tools for `ops.crossplane.io` `Operation`,
  `CronOperation` and `WatchOperation` once they are widely used.
- **Cost awareness.** Correlate managed resources with cloud cost data, so a
  model can answer "what is this platform costing us?".
- **Time travel.** Query historical condition and event data from an external
  store, to answer "when did this start failing?".
- **GitOps awareness.** Recognise that a resource is managed by Argo CD or Flux,
  and route a proposed change to a pull request rather than to the API server.
- **Policy engine integration.** Surface the Kyverno or Gatekeeper decision that
  blocked a Crossplane resource.
- **Pluggable rules.** Load the v0.6 rule catalogue from OCI artifacts.

## Not planned

- Replacing `kubectl` or a general purpose Kubernetes MCP server. If you want
  to inspect arbitrary Kubernetes objects, run one of those alongside this one.
- Hard-coded knowledge of specific providers. Everything must work through
  discovery and categories.
- Autonomous, unsupervised remediation. There is always a human in the loop for
  a write.
- Becoming a vulnerability scanner, a cost platform or a CI system. We integrate
  with those; we do not reimplement them.
- A web UI.

## How to influence this roadmap

Open an issue describing the problem you have, not the feature you want. The
best roadmap items come from somebody explaining what they could not answer
about their control plane.

- A question, or an idea that is not fully formed → [Discussions][discussions]
- Something concrete and small → a [feature request][feature]
- Something large, or anything that writes to a control plane → a [design
  proposal][proposal]

Items labelled [`help wanted`][help-wanted] are ready for a contributor to pick
up, and [`good first issue`][good-first-issue] items are scoped so that a first
contribution does not require deep knowledge of the codebase.

[board]: https://github.com/ravibagri5/crossplane-mcp-server/projects
[discussions]: https://github.com/ravibagri5/crossplane-mcp-server/discussions
[feature]: https://github.com/ravibagri5/crossplane-mcp-server/issues/new?template=feature_request.yml
[good-first-issue]: https://github.com/ravibagri5/crossplane-mcp-server/labels/good%20first%20issue
[help-wanted]: https://github.com/ravibagri5/crossplane-mcp-server/labels/help%20wanted
[ideas]: https://github.com/ravibagri5/crossplane-mcp-server/discussions/categories/ideas
[milestones]: https://github.com/ravibagri5/crossplane-mcp-server/milestones
[proposal]: https://github.com/ravibagri5/crossplane-mcp-server/issues/new?template=design_proposal.yml
