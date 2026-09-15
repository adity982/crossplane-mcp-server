# Community

How this project is organised on GitHub: where conversations happen, how issues
are labelled, and the one-time repository settings that cannot live in a file.

## Discussions

[Discussions](https://github.com/ravibagri5/crossplane-mcp-server/discussions)
are for working out whether something is worth doing. Issues are for things we
have agreed to do, and for defects. A maintainer converts a discussion into an
issue when it is ready, so nothing is lost by starting in the right place.

| Category | Format | Use it for |
| --- | --- | --- |
| **Announcements** | Announcement | Releases, breaking changes, roadmap updates. Maintainers post; anyone comments. |
| **Q&A** | Question and answer | "How do I...", "Why does it...". Answers get marked, so the category becomes a knowledge base. |
| **Ideas** | Open-ended | Anything not yet formed enough to be an issue. Upvotes here feed the roadmap. |
| **Show and tell** | Open-ended | Workflows, prompts, integrations, transcripts. The most useful signal we get about tool descriptions. |
| **Compositions and patterns** | Open-ended | Crossplane patterns that are awkward to inspect, and how people work around them. |
| **Contributors** | Open-ended | Design discussion between contributors, release coordination, triage questions. |

Templates for Ideas, Q&A and Show and tell live in
[.github/DISCUSSION_TEMPLATE](../.github/DISCUSSION_TEMPLATE).

What does **not** belong in Discussions:

- Security vulnerabilities. Use
  [private reporting](https://github.com/ravibagri5/crossplane-mcp-server/security/advisories/new).
- Reproducible bugs. Open a
  [bug report](https://github.com/ravibagri5/crossplane-mcp-server/issues/new?template=bug_report.yml).

## Issue triage

Every new issue arrives with `needs triage`. A maintainer removes it once the
issue has:

1. a **type** label — `bug`, `enhancement`, `proposal`, `documentation`, and so
   on;
2. an **area** label — `area/diagnostics`, `area/mcp`, and so on;
3. a **priority** label;
4. a **theme** label, if it maps to a roadmap theme;
5. a milestone, if it is accepted for a specific release.

Other conventions:

- `needs information` is applied when we are waiting on the reporter. Issues
  with it are closed after 30 days of silence, and reopening one is welcome.
- `good first issue` is only applied once the issue states what to change and
  where. "Add pagination" is not a good first issue; "fall back to Established
  on XRDs without Ready, in `pkg/crossplane/conditions.go`" is.
- `help wanted` means no maintainer is working on it and a pull request will be
  reviewed promptly.
- `breaking change` on an issue or pull request means the tool contract changes
  and existing user prompts will break. These are called out first in release
  notes.

The label catalogue is [.github/labels.yml](../.github/labels.yml) and is
applied by the labels workflow. Change labels there in a pull request, not in
the UI: labels created by hand are removed on the next sync.

## Repository settings

These cannot be committed, so they are recorded here. Maintainers with admin
access apply them.

**General**

- Default branch: `develop`.
- Merge button: squash only for `develop`; merge commits allowed on `main` so
  release merges keep their history.
- Automatically delete head branches after merge: on.
- Discussions: on, with the categories above.
- Issues: on, with blank issues disabled.
- Private vulnerability reporting: on.

**Branch protection**

See [docs/branching.md](branching.md#protection-rules) for the full rules on
`main`, `develop` and `release/*`.

**Projects**

One project board, with columns backed by the milestones in
[ROADMAP.md](../ROADMAP.md). Issues are added to it at triage.

## Becoming a maintainer

See [GOVERNANCE.md](../GOVERNANCE.md). In short: sustained, good quality
contributions and review, then nomination by an existing maintainer.
