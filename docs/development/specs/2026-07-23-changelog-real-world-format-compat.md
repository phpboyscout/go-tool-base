---
title: "changelog: parse and generate against real-world release history, not just our own output"
description: "Two HIGH defects share one root cause — fixture blindness. Parse() cannot read the releaser-pleaser CHANGELOG format the surrounding toolchain actually emits (so self-update breaking-change warnings silently never fire), and the generator never detects a release tag placed on a merge commit (the normal shape of a merged Release MR), making released commits reappear as Unreleased. One fixture-driven rework fixes both: real releaser-pleaser fixtures for the parser, merge-commit repo fixtures plus ancestry-based bucketing for the generator."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - changelog
  - self-update
  - parsing
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# changelog: parse and generate against real-world release history, not just our own output

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Shipped in go/changelog v0.1.2 (changelog MR !11), red-first TDD with the defect reproduced against the module's pre-fix main before the change; GTB picks it up via the config-family / module dependency bumps. Open questions were resolved by the maintainer during triage.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (both HIGH changelog findings),
    [leaf module extraction: workspace + changelog](2026-07-21-leaf-module-extraction-workspace-changelog.md) (the extraction that published this module)

---

## 1. Problem

Both halves of `go/changelog` fail against the release shapes the surrounding
toolchain actually produces. The test suites only round-trip the module's own
`formatGroups` output and linear history — a self-consistent blind spot.

### 1a. Parse() returns zero releases for releaser-pleaser changelogs

`go/changelog/parse.go:16-19`:

```go
releaseHeaderRe = regexp.MustCompile(`^#\s+((?:v?\d+\.\d+\.\d+|Unreleased).*)`)
sectionHeaderRe = regexp.MustCompile(`^###?\s+(.+)`)
entryScopedRe   = regexp.MustCompile(`^\*\s+\*\*([^*:]+):\*\*\s*(.+)`)
entryUnscopedRe = regexp.MustCompile(`^\*\s+(.+)`)
```

Release headings must be a single `#` with a bare version, and entries must be
`*` bullets. Every changelog releaser-pleaser produces — including this
module's *own* `CHANGELOG.md` — uses `## [v0.1.0](url)` level-2, link-wrapped
headings and `-` bullets. Verified: `Parse` over `go/controls/CHANGELOG.md`
returns `releases=0, from="", to=""` — silently, with no error.

**Consequence for self-update:** GTB's update flow
(`pkg/setup/update.go:1182,1201`) calls `ParseFromArchive`/`Parse` on real
release notes, receives an empty `Changelog`, and `HasBreakingChanges()` is
therefore always `false`. The breaking-change warning — the parser's entire
reason to exist in the update path — silently never fires for the ecosystem's
own releases.

### 1b. A release tag on a merge commit is never detected as a boundary

`go/changelog/generate.go:217-230` — in `bucketCommits`, merge commits are
skipped *before* the tag-boundary lookup:

```go
// Skip merge commits (commits with more than one parent).
if c.NumParents() > 1 {
    return nil
}
// If this commit is tagged, close the current group and start a new one.
if tagName, ok := tagByCommit[c.Hash]; ok { ... }
```

A tag pointing at a merge commit — the normal shape on any repo using non-FF
merges, e.g. GitLab merging a Release MR — never closes a group. Verified: a
repo with `feat` + `fix` commits, a tagged merge commit, and one post-tag
commit generates a changelog with **no release heading at all**; every
released commit is reported under `# Unreleased`. Relatedly, bucketing by
`LogOrderCommitterTime` linearises parallel branches by committer date, so a
commit merged after a tag but *committed* before it lands in the wrong release
even without merge-commit tags. `generate_test.go:27-76` exercises only linear
history.

## 2. Proposed change

One fixture-driven rework across both halves.

**Parser — accept the real-world grammar** alongside the current one:

- Release headings: `#` or `##` level; version optionally link-wrapped
  (`[v1.2.3](url)`); trailing date/annotation text still tolerated. Extract
  the version from the link text when present.
- Entries: accept `-` as well as `*` bullets in both the scoped and unscoped
  entry patterns.
- Section mapping unchanged (`Features` / `Bug Fixes` / `Breaking Changes` are
  already what releaser-pleaser emits).
- Add real fixtures: verbatim copies of releaser-pleaser changelogs (e.g. this
  module's and `go/controls`'s `CHANGELOG.md`) checked in under `testdata/`,
  plus the module's own emitted format to keep the round-trip property.

**Generator — tag detection before merge-skip, then ancestry bucketing:**

- Immediate correctness fix: perform the `tagByCommit` lookup *before* the
  merge-commit skip so a tagged merge commit closes its group (its own message
  is still excluded from entries).
- Structural fix: replace committer-time linearisation with ancestry-based
  bucketing — order tags by semver, then walk `tagN-1..tagN` commit ranges
  (reachability difference), attributing each commit to the earliest release
  containing it. `Unreleased` is `HEAD..latest-tag`'s complement. This makes
  bucketing correct for parallel branches regardless of committer timestamps
  and removes the order-sensitivity entirely.

The parser change is backward compatible. The generator's output format is
unchanged; only bucketing correctness improves.

## 3. Scope & release plan

- **Module:** `gitlab.com/phpboyscout/go/changelog` (currently v0.1.0). Both
  fixes ship together as `fix(parse):` + `fix(generate):` commits;
  releaser-pleaser cuts one patch/minor release.
- **Downstream pickup:** GTB consumes the module directly
  (`go.mod: go/changelog v0.1.0`) in `pkg/setup/update.go` (self-update
  breaking-change warning) and `pkg/cmd/changelog` (the `changelog` command).
  A dep bump in GTB delivers both fixes; no GTB code change expected. The
  self-update warning path should get a GTB-side regression test asserting a
  releaser-pleaser-format changelog with a `Breaking Changes` section yields
  `HasBreakingChanges() == true`.
- **Docs:** module docs gain a "supported changelog formats" note; GTB
  `docs/components/` changelog page cross-checked.

## 4. Acceptance criteria

- **Real releaser-pleaser changelog fixture:** `Parse` over a verbatim
  releaser-pleaser `CHANGELOG.md` returns the correct release count, versions
  (from/to), sections, and entries; a fixture containing a
  `### Breaking Changes` section makes `HasBreakingChanges()` true.
- Legacy-format fixtures (single-`#`, `*` bullets) still parse identically —
  no regression in the round-trip tests.
- **Merge-commit repo fixture:** a generated test repo where the release tag
  points at a merge commit (branch → non-FF merge → tag → further commits)
  produces a changelog with the tagged release heading present, pre-tag
  commits under it, and only post-tag commits under `Unreleased`.
- Branch-interleave fixture: a commit authored/committed before a tag but
  merged after it is bucketed into the later release (ancestry, not committer
  time).
- Two-tags-same-commit and tagged-root-commit edge cases produce deterministic
  output (documenting the chosen behaviour for the former).
- Existing linear-history generator tests remain green unchanged.

## 5. Open questions

1. Ancestry bucketing vs. the minimal lookup-reorder: ship both in one release
   (recommended — the fixtures needed to prove the reorder already exercise
   ancestry), or land the one-line reorder as an immediate patch first?
2. When a tagged merge commit's own message *is* a conventional commit (squash
   workflows), should it contribute an entry? Recommendation: no — keep
   merge-commit messages excluded from entries for consistency.
3. Should `Parse` return a sentinel error (rather than an empty changelog)
   when the input is non-empty but yields zero releases, so callers like
   self-update can distinguish "no changelog" from "unparseable format"?
