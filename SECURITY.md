# Security Policy

## Supported versions

Security fixes land on the latest minor release. Older releases are not
patched; please upgrade.

| Version | Supported |
| --- | --- |
| Latest minor | Yes |
| Anything older | No |

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Report privately using
[GitHub's private vulnerability reporting](https://github.com/ravibagri5/crossplane-mcp-server/security/advisories/new),
which notifies the maintainers listed in [MAINTAINERS.md](MAINTAINERS.md)
without disclosing anything publicly.

Please include:

- A description of the issue and its impact.
- Steps to reproduce, ideally with a minimal configuration.
- The version of `crossplane-mcp-server` and of Crossplane you tested against.
- Whether you believe the issue is being actively exploited.

### What to expect

| Stage | Target |
| --- | --- |
| Acknowledgement | 3 business days |
| Initial assessment | 10 business days |
| Fix or mitigation plan | 30 days for high and critical severity |

We will credit you in the advisory unless you ask us not to. Please give us a
reasonable window to ship a fix before disclosing publicly.

## Threat model

Understanding what this server does and does not do will help you judge whether
something is a vulnerability.

### The server is read-only

Every tool performs `get` or `list` requests only. There is no code path that
creates, updates, patches or deletes anything on the control plane. A report
that the server can be made to mutate cluster state is a high severity bug and
we want to hear about it immediately.

### The server acts as you

Over the stdio transport the server uses your kubeconfig and therefore has
exactly your permissions. It does not escalate privilege, and it cannot read
anything you could not read with `kubectl`.

Run it against a context with least privilege if you want a stronger guarantee.
The RBAC section of the [README](README.md#required-rbac) shows a read-only
cluster role that covers every tool.

### Secrets

Crossplane writes connection details into Kubernetes `Secret` objects. This
server never reads `Secret` resources, and `crossplane_resource_get` never
returns a resource's connection secret. It does return the full manifest of a
resource when you ask for it, which for a managed resource may include
references to a secret's name and namespace, but not its contents.

If you find a path through which secret data reaches a tool result, that is a
vulnerability. Please report it.

### The HTTP transport is unauthenticated

`--http-address` serves the MCP streamable HTTP transport with no
authentication or authorisation of its own. Anyone who can reach the endpoint
can call every tool with the server's credentials.

This is a deliberate design choice, not a bug: authentication belongs in a
proxy that your organisation already operates. Do not expose the HTTP endpoint
to an untrusted network. Reports that the HTTP endpoint is unauthenticated will
be closed with a pointer to this section.

### Prompt injection

Tool results contain data from your control plane: resource names, condition
messages, and event text. Any of these can contain text written by a third
party, for example an error message from a cloud provider API. A sufficiently
motivated attacker who can influence that text may be able to influence the
model reading it.

The server truncates condition and event messages, but it does not and cannot
sanitise them meaningfully. Treat tool output as untrusted data, and do not
give an assistant using this server the ability to act on your infrastructure
without review.

## Dependencies

Dependencies are updated automatically by Dependabot and every pull request is
scanned with `govulncheck` in CI. If you spot a vulnerable dependency we have
missed, please open a regular issue; it is not sensitive.
