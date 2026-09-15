<!--
Thanks for contributing. A short description of why this change is needed is
worth more than a long description of what it does; the diff already says what
it does.

Base branch: this should target `develop`, not `main`. Only `release/*` and
`hotfix/*` branches go to `main`. If the base is wrong, use Edit next to the
title to change it. See docs/branching.md.

Title: use a Conventional Commit, for example
  feat(diagnostics): report ProviderConfig credential resolution
It becomes the squashed commit message.
-->

## What does this change?

<!-- One or two sentences. -->

## Why?

<!--
What problem does this solve? If it fixes an issue, link it:
Fixes #123
-->

## How did you test it?

<!--
Which Crossplane version and provider did you try this against? "Unit tests
only" is a fine answer for changes that do not touch cluster behaviour.
-->

## Checklist

- [ ] This pull request targets `develop`, not `main` ([why](https://github.com/ravibagri5/crossplane-mcp-server/blob/main/docs/branching.md))
- [ ] The title is a Conventional Commit
- [ ] Commits are signed off (`git commit --signoff`)
- [ ] Tests cover the behaviour I changed
- [ ] `make check` passes locally
- [ ] The README tool table is updated, if I added or renamed a tool
- [ ] CHANGELOG.md has an entry under Unreleased, for user-visible changes

## Breaking changes

<!--
Renaming or removing a tool, or making an optional argument required, breaks
every user's prompts. If this does that, say so here and explain the migration.
Otherwise write "None".
-->

None
