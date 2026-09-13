# Contributing

Thanks for wanting to help. This project aims to be the most useful way for an
AI assistant to understand a Crossplane control plane, and that only happens
with contributions from people running real control planes.

## Table of contents

- [Code of Conduct](#code-of-conduct)
- [Ways to contribute](#ways-to-contribute)
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
- **Every tool is read only.** There is no code path in this server that
  creates, updates or deletes, and `api.Tool.Destructive` exists to keep the
  annotations honest if that ever changes. A pull request that adds a write
  needs to argue for it first.

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

Write commit messages in the imperative mood, with a short summary line and a
body explaining why the change is needed:

```text
Report Established on XRDs that do not publish Ready

Crossplane reports Established and Offered on an XRD rather than Ready, so
the summary line showed "-" for every XRD. Fall back to Established when
Ready is absent.
```

All commits must be signed off under the
[Developer Certificate of Origin](https://developercertificate.org/):

```shell
git commit --signoff
```

## Pull requests

- One logical change per pull request.
- Add or update tests for behaviour you change.
- Update the README tool table if you add or rename a tool.
- CI must be green. It runs build, vet, lint and tests on Linux, macOS and
  Windows.

A maintainer will review within a few days. If nobody has looked after a week,
please ping the pull request; it is much more likely that we missed it than
that we are ignoring it.

## Releases

Maintainers cut releases by pushing a semver tag:

```shell
git tag -s v0.4.0 -m "v0.4.0"
git push origin v0.4.0
```

GoReleaser builds the binaries, publishes the container image to GHCR and
drafts the release notes from the commit log.
