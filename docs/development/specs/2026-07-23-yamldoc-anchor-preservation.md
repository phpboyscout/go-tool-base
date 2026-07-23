---
title: "yamldoc: preserve anchors on Set, and make Bytes() validation resolve aliases"
description: "Replacing an anchored value via Set silently drops the anchor, emitting a document with a dangling alias — and the Bytes() safety net does not catch it, because it only re-parses and goccy accepts an alias to an undefined anchor at parse time. This violates the package's refuse-rather-than-corrupt promise. Preserve the anchor on the replacement node (recommended) or refuse with ErrUnsupported, and harden Bytes() to a full decode that resolves aliases. yamldoc underpins the whole config family, so the fix propagates via a config-core dependency bump."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - yamldoc
  - config
  - yaml
  - data-integrity
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# yamldoc: preserve anchors on Set, and make Bytes() validation resolve aliases

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH yamldoc finding),
    [config module extraction](2026-07-18-config-module-extraction.md) (the config family this module underpins)

---

## 1. Problem

yamldoc's core promise is refuse-rather-than-corrupt: mutate a document
surgically, and never emit bytes that damage it. Anchored values break both
halves of that promise at once.

- `go/yamldoc/set.go:39-56` — `setExisting` handles the scalar-in-place case
  via `assignScalar`, otherwise replaces `entry.Value` wholesale with
  `buildValueNode(...)`. An `*ast.AnchorNode` matches neither `assignScalar`'s
  type switch nor any anchor-aware path, so the anchor node — and with it the
  `&name` definition — is discarded along with the old value.
- `go/yamldoc/yamldoc.go:119-131` — `Bytes()`'s safety net only re-**parses**
  the emitted output (`parser.ParseBytes`). goccy accepts an alias to an
  undefined anchor at parse time, so the damage sails through.

Verified on:

```yaml
defaults: &d
  retries: 3
prod:
  settings: *d
```

`Set("defaults", map[string]any{"retries": 9})` succeeds, and `Bytes()`
returns without error — but the `&d` anchor is gone, `*d` now dangles (any
consumer decoding the output fails, or silently changes meaning under lenient
decoders), and the replacement is mis-indented because `blockAnchor` aligned
against the old `&d` token position. Silent corruption of a valid document is
exactly the failure class this package exists to prevent, and the
otherwise-strong adversarial suite has no anchor-mutation cases.

Related LOW (same root): `Set("defaults.retries", 5)` — editing *inside* an
anchored mapping — fails with a misleading `"defaults" is not a mapping`
(`ErrNotFound`) because `asMappingNode` (`path.go:80-93`) does not unwrap
`*ast.AnchorNode`.

## 2. Proposed change

Two alternatives for the write path:

**A. Preserve the anchor (recommended).** Teach `setExisting` (and the path
resolver) to see through `*ast.AnchorNode`: when the target value is anchored,
build the replacement node, re-attach it under the original anchor (keeping
`&name` and its token position for `blockAnchor` alignment), and mutate
scalars in place through the anchor wrapper so comments/quoting survive as
they do today. Aliases elsewhere keep resolving to the *new* value —
which is standard YAML anchor semantics and almost always what a config-editing
caller means. Also fix `asMappingNode` to unwrap `AnchorNode`, making
`Set("defaults.retries", 5)` work naturally.

**B. Refuse with `ErrUnsupported`.** Detect an anchored target (or an
anchored ancestor) in `Set`/`Remove` and return
`fmt.Errorf("%w: cannot replace anchored value %q", ErrUnsupported, path)`.
Safe and small, but it makes the library unusable on legitimately anchored
config files and pushes the problem to every caller.

**Recommendation: A**, with B's refusal retained as the fallback for the cases
A cannot yet do safely (e.g. `Remove` of an anchor definition whose aliases
remain — that genuinely must refuse, naming the dangling alias).

**Independently of A/B, harden the safety net.** `Bytes()` validation becomes
a full decode, not just a parse: after `parser.ParseBytes` succeeds, run
`yaml.Unmarshal` (alias-resolving) over the output and wrap any failure in
`ErrEmitInvalid`. This catches dangling aliases from *any* future mutation bug,
not just this one — the safety net finally covers semantic damage. Bounded
alias expansion must be configured (goccy's alias-depth/count limits) so the
validator itself is not a billion-laughs vector.

## 3. Scope & release plan

- **Module:** `gitlab.com/phpboyscout/go/yamldoc` (currently v0.1.3).
  `fix(set):` + `fix(bytes):` (+ `fix(path):` for the anchor-unwrap) →
  patch/minor release via releaser-pleaser.
- **Downstream propagation:** yamldoc sits under the whole config family —
  `go/config` pins `yamldoc v0.1.3` in its go.mod and the YAML write path
  (`Apply`, comment-preserving in-place writes) flows through it. Pickup is a
  **config-core dependency bump** (its own releaser-pleaser release), which
  the config-format satellites and GTB then pick up via routine Renovate
  bumps. No API change is expected for either alternative A or the hardened
  `Bytes()`; alternative B would surface new `ErrUnsupported` cases to config
  `Apply` callers and must be called out in the config release notes.
- **Docs:** yamldoc's design docs gain an "anchors & aliases" section stating
  the supported semantics (replace-under-anchor, refusal cases); config docs
  note the behaviour for anchored user config files.

## 4. Acceptance criteria

- **Anchor-mutation adversarial cases** added to the adversarial suite:
  - Replace an anchored scalar and an anchored mapping → output keeps `&name`
    on the new value, aliases resolve to the new value, indentation correct.
  - In-place scalar edit through an anchor (`Set("defaults.retries", 5)`)
    preserves the anchor, the sibling comments, and quoting style.
  - `Remove` of an anchor definition with a live alias elsewhere → refuses
    with `ErrUnsupported`, message naming the alias path; document unchanged.
  - Alias target edits (`Set` on a path whose value is an `*ast.AliasNode`) →
    defined behaviour: refuse (an alias edit is ambiguous) with a clear error.
- **Safety-net regression:** a crafted mutation that produces a dangling alias
  (e.g. via the pre-fix bug reproduced in a white-box test) is rejected by
  `Bytes()` with `ErrEmitInvalid` — proving the decode-level net catches what
  the parse-level net missed.
- Billion-laughs guard: a document with deeply nested alias expansion is
  either handled within bounds or refused — `Bytes()` never hangs or
  balloons memory.
- Full existing suite (round-trip, comment-ownership, hardening) green;
  `go/config`'s own write-path tests green against the bumped yamldoc before
  the config release is cut.

## 5. Open questions

1. Anchor semantics under alternative A: replacing an anchored value changes
   what every alias resolves to. Is that ever surprising enough to warrant an
   opt-in (`SetOption`) instead of the default? Recommendation: default-on —
   it is standard YAML semantics — but confirm before release.
2. Should the `Bytes()` full-decode validator be applied to `Parse` input as
   well, so a document that *arrives* with a dangling alias is reported
   immediately rather than at emit time? Leaning yes (report via
   `Unsupported()`), but it widens the change.
