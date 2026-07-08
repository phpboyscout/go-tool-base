---
name: conventional-commits
description: Write Conventional Commits cleanly — type(scope) subject, one coherent change per commit, a body that explains why. Use whenever you are committing, structuring a feature into commits, or wording a commit message.
---

# Conventional Commits

Use this whenever you are committing, or splitting a piece of work into commits.
[Conventional Commits](https://www.conventionalcommits.org/) make history
machine-readable: release tooling (semantic-release, releaser-pleaser, and the
like) reads the `type` to compute the next version and the changelog, so a wrong
type can suppress a release or inflate a version. Even without that tooling, a
disciplined log is the cheapest documentation you have.

## The format

```
type(scope): short imperative summary

Optional body: explain WHY this change was made — the rule, the spec section,
the bug, the decision — not just what the diff shows.

Optional footer: BREAKING CHANGE: …, or issue refs.
```

## Types

| Type | When | Typical release effect |
|------|------|------------------------|
| `feat` | New user-facing capability | minor |
| `fix` | Bug fix (incl. test/lint correctness) | patch |
| `refactor` | Structural change, no behaviour change | patch or none |
| `perf` | Performance, no behaviour change | patch |
| `docs` | Documentation only | none |
| `test` | Tests only | none |
| `ci` | Pipeline / tooling config | none |
| `chore` | Deps, housekeeping | none |
| `style` | Formatting only, zero logic | none |

The release column is the *common* mapping — your release tool defines the truth.
Confirm its behaviour before relying on it (see Breaking changes).

## Always scope it

Include a `(scope)` naming the **functional area** — the package, subsystem, or
feature — so the log is scannable without opening each diff:

```
fix(parser): handle empty input without panicking
feat(auth): add API-key verification
chore(deps): bump golang.org/x/crypto to v0.49.0
```

Avoid junk scopes — `(misc)`, `(various)`, or the repo's own name — they carry no
information.

## One coherent change per commit

Each commit is a single, self-contained change. Don't bundle unrelated fixes, and
don't mix mechanical cleanup (lint, formatting) with feature work — split them so
each can be read, reverted, or cherry-picked on its own. Grouping is fine only
when the changes are genuinely inseparable (a dependency bump that forces a
call-site fix in the same file).

The body explains **why**. Link the linter rule, the deprecation notice, the spec
decision, or the issue — the *what* is already in the diff.

## Breaking changes — know your tool first

A `feat!:` / `fix!:` marker or a `BREAKING CHANGE:` footer signals an incompatible
change, and most tools cut a **major** bump for it. Two cautions:

- **The footer is load-bearing even in prose.** Some tools scan the whole commit
  message (and even MR/PR descriptions) for the literal token `BREAKING CHANGE:`.
  Writing it casually — e.g. "this is not a BREAKING CHANGE" — can trip the
  release. Quote or rephrase it (`` `BREAKING CHANGE` ``) when you mean it
  literally but aren't declaring one.
- **Pre-1.0 projects often don't want a major bump.** A `0.x` project that marks a
  break may get force-promoted to `1.0.0` by a tool with no pre-1.0 guard. Check
  how *your* tool treats `0.x` before declaring a break; many ship breaking
  changes as a minor while pre-1.0.

## Get approval before committing

Don't run `git commit` until the human has asked you to. Present the changes and
the proposed message, then wait — the person who runs the commit owns it. See the
[forge-publish-workflow](../../../phpboyscout-common/skills/forge-publish-workflow/SKILL.md)
skill for the surrounding publish discipline (and: no AI attribution, no
`@`-mentions).
