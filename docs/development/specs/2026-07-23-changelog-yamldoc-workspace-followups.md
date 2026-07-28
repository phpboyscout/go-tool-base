---
title: "changelog, yamldoc, workspace: medium/low follow-ups from the architectural review"
description: "Batches the remaining MEDIUM and LOW findings for three leaf modules from the 2026-07-23 architectural review: yamldoc's unsupported-construct scanner misreading block-scalar content as flow structure (refusing valid documents) and its misleading refusal when editing through an anchored mapping; changelog's same-commit tag collision, silently-disabled WithSinceTag filter, and empty-repo error; and workspace's WithMaxDepth(0) skipping even the start directory. The changelog HIGHs and the yamldoc anchor HIGH are owned by their own specs."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - changelog
  - yamldoc
  - workspace
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# changelog, yamldoc, workspace: medium/low follow-ups from the architectural review

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (§ controls/workspace/changelog/yamldoc),
    [changelog real-world format compatibility](2026-07-23-changelog-real-world-format-compat.md) (the two changelog HIGHs, specced separately),
    [yamldoc anchor preservation](2026-07-23-yamldoc-anchor-preservation.md) (the yamldoc anchor-drop HIGH, specced separately)

---

## 1. Context

The 2026-07-23 architectural review judged these leaf extractions well-crafted
— yamldoc's refuse-rather-than-corrupt philosophy and changelog's
parser/generator split both drew praise — and placed their defects at the
semantic edges rather than in structure. The HIGH findings (changelog's
releaser-pleaser-format blindness and merge-commit tag loss; yamldoc's
dangling-alias corruption) are owned by the sibling specs above; this spec
batches the one remaining MEDIUM and four LOWs across three modules into a
single small hardening pass so each module ships one follow-up release rather
than several.

## 2. Items

### 2.1 yamldoc

**F1 — Track block-scalar regions in the unsupported-construct scanner.**
*(MEDIUM)*
`detectUnsupported`/`flowScanner` (`unsupported.go:41-99`) is line-based and
knows nothing about block scalars. A block scalar containing JSON with a `#`
(e.g. `template: |` followed by `"a": 1, # not a yaml comment` and a closing
`}`) — a very common shape for embedded templates — is flagged as a
`multi-line flow collection with interior comments`, and `Bytes()` then
refuses the whole document with `ErrUnsupported` (verified by the review).
The failure direction is safe (refusal, not corruption), but it makes the
library unusable on legitimate config files.
Direction: track block-scalar regions in the scanner — a `|`/`>` header opens
a region owning every subsequent line indented deeper than the header's key —
and skip those content lines entirely; add block-scalar-with-braces and
block-scalar-with-comments cases to the adversarial suite.
*Acceptance:* a document whose only brace/`#` content sits inside a block
scalar round-trips through `Set`+`Bytes()` without `ErrUnsupported`, while
true multi-line flow collections with interior comments are still refused.

**F2 — Honest refusal when editing through an anchored mapping.** *(LOW)*
`Set("defaults.retries", 5)` on `defaults: &d\n  retries: 3` fails with
`"defaults" is not a mapping` (`ErrNotFound`) because `asMappingNode`
(`path.go:116`, callers at `path.go:56` and `path.go:80`) does not unwrap
`*ast.AnchorNode`. The refusal is safe, but the message is wrong — the target
*is* a mapping — and there is no supported way to edit through an anchor.
Direction: either unwrap `AnchorNode` in `asMappingNode` so in-place edits of
anchored-mapping children work (the anchor token itself is untouched, so this
should be safe under the re-parse net), or refuse with `ErrUnsupported` and an
anchor-specific message. Decide jointly with the
[anchor-preservation spec](2026-07-23-yamldoc-anchor-preservation.md), which
owns the adjacent replace-the-anchored-value path (Q1).
*Acceptance:* the operation either succeeds preserving `&d` byte-for-byte, or
fails with `ErrUnsupported` naming the anchor — never `ErrNotFound` with a
"not a mapping" message.

### 2.2 changelog

**F3 — Tag-map collisions, silent SinceTag miss, empty-repo error.** *(LOW)*
Three edge defects in the generator: (a) `tagByCommit` is `map[hash]name`
(`generate.go:115-118`, verified) — when two tags (v1.0.0 and v1.0.1) point at
the same commit, one overwrites the other, iteration-order dependent, and a
release silently disappears; (b) an invalid `WithSinceTag` value silently
disables the filter, so a typo yields the full history instead of an error;
(c) an empty repository returns the raw `repo.Head()` error rather than an
empty changelog.
Direction: make the map value a slice and emit one (possibly empty) release
section per tag, deterministically ordered by semver; return a wrapped
not-found error for an unknown `WithSinceTag`; detect the empty-repo
`plumbing.ErrReferenceNotFound` and return an empty `Changelog`. Coordinate
with the [format-compat spec](2026-07-23-changelog-real-world-format-compat.md)
touching the same bucketing code so the two land in one release.
*Acceptance:* two tags on one commit both appear in the output; an unknown
since-tag returns an error; `Generate` on an empty repo returns an empty
changelog and no error — all under test.

### 2.3 workspace

**F4 — WithMaxDepth(0) must still check the start directory.** *(LOW)*
The walk loop is `for depth := range cfg.maxDepth` (`workspace.go:67-84`,
verified), so `WithMaxDepth(0)` performs zero iterations and returns
`ErrNotFound` without ever statting `startDir` — surprising for the natural
reading "don't ascend at all".
Direction: define `maxDepth` as the number of *parent* levels to ascend, with
the start directory always checked (depth 0 = start dir only); either shift
the loop to `maxDepth+1` iterations or reject `0` at option-validation time if
the current semantics are deemed intentional (Q2). Document the chosen meaning
on `WithMaxDepth`.
*Acceptance:* `WithMaxDepth(0)` with a marker in the start directory finds it
(or the option constructor rejects 0, per Q2's answer), under test.

## 3. Scope & release plan

Three independent module releases via releaser-pleaser:

1. **`gitlab.com/phpboyscout/go/yamldoc`** — F1 (`fix(unsupported):`) and F2
   (`fix(path):`), one patch release, ideally the same wave as the
   anchor-preservation spec's release. GTB has no direct yamldoc dependency:
   the fix reaches GTB via `go/config` (whose editing codec consumes yamldoc)
   re-pinning the new yamldoc and GTB then bumping config-core — slot the
   propagation into the config family re-pin sweep rather than a bespoke bump.
2. **`gitlab.com/phpboyscout/go/changelog`** — F3, patch release, landing with
   (or immediately after) the format-compat HIGH fix so GTB's changelog/update
   surface re-pins once. GTB consumes changelog directly (`pkg/` changelog
   command and the self-update release-notes path); a single `go.mod` bump.
3. **`gitlab.com/phpboyscout/go/workspace`** — F4 alone, patch release
   (`fix(workspace):`); GTB consumes workspace directly, single dep bump.

No GTB code changes are expected beyond the version bumps; all fixes are
behavioural corrections beneath stable call sites.

## 4. Open questions

- **Q1 — anchored-mapping edit vs refusal.** For F2, is unwrapping
  `AnchorNode` (allowing edits inside anchored mappings) actually safe for all
  alias consumers — every `*d` alias silently sees the edited value, which is
  correct YAML semantics but may surprise a user editing "just defaults"? If
  that surprise is deemed unacceptable, refusal-with-honest-error is the
  fallback. Should follow whatever stance the anchor-preservation spec adopts.
  **RESOLVED — follow the shipped anchor-preservation stance.** yamldoc already
  unwraps `*ast.AnchorNode` in `asMappingNode`, so an in-place scalar edit
  through an anchored mapping (`Set("defaults.retries", 5)` on `defaults: &d`)
  succeeds and preserves `&d`/`*d` byte-for-byte (covered by
  `TestSet_InPlaceEditThroughAnchor`), while an edit that would orphan an alias
  is refused with an anchor-specific `ErrUnsupported` message. F2's acceptance
  is therefore already met; no further code change was needed and no
  `ErrNotFound` "not a mapping" message can arise.
- **Q2 — WithMaxDepth(0) semantics.** Treat 0 as "start directory only"
  (behavioural change, more intuitive) or as invalid input rejected at
  construction (preserves the current "n directories checked" reading)? The
  spec leans "start directory only" — matching how most walk APIs define
  depth — but it is a user-visible semantic choice.
  **RESOLVED — start directory only.** `maxDepth` now counts parent levels to
  ascend, with the start directory always checked; the loop runs `maxDepth+1`
  times so `WithMaxDepth(0)` checks the start directory alone without ascending.
  Documented on `WithMaxDepth`.
- **Q3 — same-commit tag ordering.** When two tags share a commit (F3), the
  later tag's release section will contain no commits of its own. Emit it as
  an explicitly empty section (faithful), or annotate it (e.g. "no changes —
  re-tag of vX.Y.Z")? Faithful-empty is the default; annotation is a
  formatting-layer nicety.
  **RESOLVED — faithful empty section.** The lower-semver release claims the
  commits; the higher tag is emitted as an explicitly empty section (no
  annotation), so both releases appear newest-first and neither disappears.
