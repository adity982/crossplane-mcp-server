# Contributing

Thanks for wanting to help. This project aims to be the most useful way for an
AI assistant to understand a Crossplane control plane, and that only happens
with contributions from people running real control planes.

## Table of contents

- [Code of Conduct](#code-of-conduct)
- [Ways to contribute](#ways-to-contribute)
- [Where to say what](#where-to-say-what)
- [Branching](#branching)
- [Development setup](#development-setup)
- [Adding a tool](#adding-a-tool)
- [Writing tool descriptions](#writing-tool-descriptions)
- [Code style](#code-style)
- [Testing](#testing)
- [Commit messages and sign-off](#commit-messages-and-sign-off)
- [Pull requests](#pull-requests)
- [Releases](#releases)

## Code of Conduct

This project follows the [CNCF Code of Conduct](CODE_OF_CONDUCT.md). By
participating you are expected to uphold it.

## Ways to contribute

- **Report what your control plane does that we get wrong.** Crossplane setups
  vary enormously. A bug report that says "on my control plane `X` reports
  `Ready` in an unusual place" is genuinely valuable.
- **Add a tool.** See [Adding a tool](#adding-a-tool).
- **Improve a tool description.** The descriptions are the API the model sees.
  Making one clearer is a real improvement, not a cosmetic one.
- **Improve the docs.** Especially client configuration for MCP clients we do
  not cover yet.

Before starting anything large, open an issue so we can agree on the shape of
it first. Nobody enjoys having a pull request turned down after a weekend of
work.

## Where to say what

| You have | Go to |
| --- | --- |
| A question about how something works | [Discussions → Q&A](https://github.com/ravibagri5/crossplane-mcp-server/discussions/categories/q-a) |
| An idea that is not fully formed | [Discussions → Ideas](https://github.com/ravibagri5/crossplane-mcp-server/discussions/categories/ideas) |
| A workflow, prompt or transcript to share | [Discussions → Show and tell](https://github.com/ravibagri5/crossplane-mcp-server/discussions/categories/show-and-tell) |
| Something reproducibly broken | [Bug report](https://github.com/ravibagri5/crossplane-mcp-server/issues/new?template=bug_report.yml) |
| A specific, scoped capability | [Feature request](https://github.com/ravibagri5/crossplane-mcp-server/issues/new?template=feature_request.yml) |
| A large change, or anything that writes to a control plane | [Design proposal](https://github.com/ravibagri5/crossplane-mcp-server/issues/new?template=design_proposal.yml) |
| Wrong, missing or confusing docs, including tool descriptions | [Documentation issue](https://github.com/ravibagri5/crossplane-mcp-server/issues/new?template=documentation.yml) |
| A security vulnerability | [Private report](https://github.com/ravibagri5/crossplane-mcp-server/security/advisories/new), never a public issue |

Discussions are for working out whether something is worth doing. Issues are
for things we have agreed to do, or defects. A maintainer will convert a
discussion into an issue when it is ready. How the categories, labels and
triage work is described in [docs/community.md](docs/community.md).

## Branching

**Open your pull request against `develop`, not `main`.**

`main` holds released code and only ever receives `release/*` and `hotfix/*`
branches, so that nothing reaches a user without having been through a release
candidate. A pull request that targets `main` is failed by the branch guard
with instructions for retargeting it; you do not need to open a new one.

```shell
git fetch upstream
git switch -c feat/provider-config-credentials upstream/develop
```

Name your branch `<type>/<short-description>`, where the type matches the one
you will use in the pull request title. See [Commit messages and
sign-off](#commit-messages-and-sign-off) for the types.

The full model, including how releases are cut and how hotfixes work, is in
[docs/branching.md](docs/branching.md).

## Development setup

You need Go 1.26 or newer and access to a Kubernetes cluster with Crossplane
installed. A local control plane is enough:

```shell
kind create cluster --name crossplane-mcp
helm repo add crossplane-stable https://charts.crossplane.io/stable
helm install crossplane crossplane-stable/crossplane \
  --namespace crossplane-system --create-namespace --wait
```

Then:

```shell
make build      # build the binary into ./bin
make test       # run the unit tests
make lint       # run golangci-lint
make tools      # print the tool list without touching a cluster
```

To try the server against your cluster without an MCP client, use the MCP
Inspector:

```shell
npx @modelcontextprotocol/inspector ./bin/crossplane-mcp-server
```

## Adding a tool

Tools live in `pkg/toolsets/<toolset>/`. A tool is a value of `api.Tool`:

```go
{
    Name:  "crossplane_widgets_list",
    Title: "Widgets: list",
    Description: "List the widgets on the control plane with their Ready condition. " +
        "Use this when asked how many widgets exist or which ones are broken.",
    InputSchema: api.Object(map[string]*jsonschema.Schema{
        "namespace": api.NamespaceProp,
    }),
    Handler: widgetsList,
}
```

and a handler:

```go
func widgetsList(p api.Params) (*api.Result, error) {
    namespace := p.Args.OptionalString("namespace", "")
    if err := p.Args.Err(); err != nil {
        return api.Error(err), nil
    }

    widgets, err := p.Client.Widgets(p, namespace)
    if err != nil {
        return api.Error(err), nil
    }
    return api.Structured(renderWidgets(widgets), widgets), nil
}
```

Three rules that are easy to get wrong:

1. **Return errors in the `Result`, not as the Go error.** The Go error is
   reserved for bugs in the server itself. Anything the model could react to —
   a missing resource, an unreachable cluster, a bad argument — belongs in
   `api.Error(err)` so the model can read it and try something else.
2. **Read every argument before checking `p.Args.Err()`.** The accessors
   accumulate errors so a single call can report every mistake at once.
3. **Return both text and structured content.** `api.Structured(text, payload)`
   gives the model a compact table to reason over and a JSON payload to extract
   values from.

New toolsets register themselves from an `init` function and are imported for
their side effect in `internal/cli/cli.go`.

## Writing tool descriptions

The description is the only thing the model has when deciding whether to call
your tool. Treat it as production code.

- Say what question the tool answers, not what API it calls. "Count the managed
  resources, broken down by kind" beats "List resources with category managed".
- Say when to prefer it over a neighbouring tool. If two tools overlap, the
  model will pick badly unless you tell it which is which.
- Mention Crossplane behaviour the model cannot infer. "Crossplane v2 deprecates
  claims, so an empty result on a v2 control plane is expected" prevents a whole
  class of wrong conclusions.
- Keep argument descriptions concrete, with an example value.

## Code style

- Run `make check` before pushing. It runs the same formatting, vet, lint and
  test steps CI does, and the linter is stricter than `go vet` alone: `prealloc`
  wants `make([]T, 0, n)` rather than `var x []T` when you append in a loop, and
  `unparam` rejects parameters nothing reads.
- Comments explain *why*, not *what*. If a line needs a comment to say what it
  does, rewrite the line. Most of the comments in this repo exist because
  Crossplane does something surprising and the next reader deserves a warning.
- Wrap errors with context using `%w`: `fmt.Errorf("cannot list %s: %w", kind, err)`.
- Error strings are lowercase and describe what failed, not what the caller
  should do.
- Prefer small, named functions over long ones with section comments.
- Do not add a dependency without discussing it in an issue first. The
  dependency footprint is deliberately small.

### Conventions a tool is expected to follow

These are enforced by review rather than by the compiler, so they are worth
stating:

- **Return a map, not a struct.** Every handler ends with
  `api.Structured(text, map[string]any{...})`. The values inside may be typed —
  `crossplane.Summary`, `crossplane.Usage` and friends carry their own json
  tags — but the envelope is a map. A named payload struct reads fine in
  isolation and then makes this tool the odd one out.
- **Report failures as a result, not an error.** `return api.Error(err), nil`.
  A returned error means the tool itself is broken and becomes a protocol
  error; an unreachable cluster or a missing resource is an answer the model
  should get to read.
- **Use the shared rendering helpers** in `pkg/api`: `Table`, `Section`,
  `Warnings`, `OrDash`, `Path`. These were once copied into five packages.
  Reach for `strconv.Itoa` rather than writing another `itoa`.
- **Name handlers after the thing then the verb**, matching the tool name:
  `resourceGet`, `usagesList`, `driftDetect`. Group constructors end in
  `Tools()`.
- **Read-only unless you say otherwise.** A tool that changes the control plane
  sets `api.Tool.Write` and lives in the `provisioning` toolset. The server
  withholds those tools unless it was started with `--read-only=false`, and a
  test enforces that a tool carries the flag if and only if it is in that
  toolset. Everything else stays `get` and `list`.
- **No tool deletes.** Writes create and update, and `pkg/crossplane` has no
  `Delete` method to call. This is a project decision rather than a gap; a pull
  request that adds deletion needs an accepted design proposal first, covering
  consent, and it is not a small one.
- **Write through the client, not around it.** `crossplane.Client.Apply` is the
  only write path, and it refuses anything that is not a Crossplane kind. Do
  not reach for the dynamic client directly.
- **Provision through platform APIs.** A tool that creates infrastructure asks
  the control plane's own XRD-defined API for it and sets only fields that
  API's schema declares. Writing a managed resource directly bypasses the
  platform team, and guessing at field names makes the API server prune them
  silently.

## Testing

- Unit tests use the standard `testing` package with
  [testify](https://github.com/stretchr/testify) assertions.
- Table-driven tests use a `map[string]struct{...}` keyed by a descriptive
  case name, matching the convention in the Crossplane codebase.
- Test names describe the behaviour being asserted:
  `TestChildReferencesDeduplicates`, not `TestChildReferences2`.
- Anything that talks to a cluster is tested against fakes. There are no tests
  that require a live control plane in CI.
- Test the pure logic. Handlers mostly fetch and format, but the decisions
  inside them — which failure is the root cause, whether drift will be
  corrected, which Usage blocks a delete — are ordinary functions over plain
  values, and those are where the bugs live. `deepestUnready`, `diffFields` and
  `classifyUsages` are the pattern to copy.

```shell
make test
make test-coverage   # writes coverage.out and prints a summary
make check           # everything CI runs: verify, vet, lint, test
```

A quick way to see what a tool really returns, without a client:

```shell
make build
./bin/crossplane-mcp-server call crossplane_status
./bin/crossplane-mcp-server call crossplane_diagnose '{"kind":"Bucket","name":"data"}' --json
```

## Commit messages and sign-off

Write the summary line as a [Conventional
Commit](https://www.conventionalcommits.org/): a type, an optional scope, and
an imperative description. The release notes are grouped by these prefixes, so
a commit without one lands under "Other".

```text
fix(compositions): report Established on XRDs that do not publish Ready

Crossplane reports Established and Offered on an XRD rather than Ready, so
the summary line showed "-" for every XRD. Fall back to Established when
Ready is absent.
```

The body matters more than the summary: explain why the change is needed, not
what the diff does.

Use `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `build`, `ci`
or `deps`, and scope it with the toolset or package you touched. Append `!`
after the scope for a change to a tool contract, which breaks existing user
prompts:

```text
feat(resources)!: rename crossplane_list_managed to crossplane_managed_resources_list
```

Because pull requests are squash-merged, the pull request title is the commit
message that lands. Individual commits on your branch do not need to follow the
convention, though it helps reviewers if they do.

All commits must be signed off under the
[Developer Certificate of Origin](https://developercertificate.org/):

```shell
git commit --signoff
```

## Pull requests

- Target `develop`. See [Branching](#branching).
- One logical change per pull request.
- Give it a Conventional Commit title; it becomes the squashed commit message.
- Add or update tests for behaviour you change.
- Update the README tool table if you add or rename a tool.
- Rebase on `upstream/develop` rather than merging it in, and push with
  `--force-with-lease`.
- CI must be green. It runs build, vet, lint and tests on Linux, macOS and
  Windows.

A maintainer will review within a few days. If nobody has looked after a week,
please ping the pull request; it is much more likely that we missed it than
that we are ignoring it.

## Releases

Releases are cut from a `release/vX.Y` branch, published first as a release
candidate (`vX.Y.0-rc.1`), soaked, then merged into `main` and tagged:

```shell
git switch -c release/v0.5 develop
git tag -s v0.5.0-rc.1 -m "v0.5.0-rc.1"
git push origin release/v0.5 v0.5.0-rc.1
```

GoReleaser builds the binaries, publishes the container image to GHCR and
drafts the release notes from the commit log. Pre-release tags are published as
GitHub pre-releases and never become `latest`.

The full procedure, including hotfixes and the versioning rules, is in
[docs/branching.md](docs/branching.md).
