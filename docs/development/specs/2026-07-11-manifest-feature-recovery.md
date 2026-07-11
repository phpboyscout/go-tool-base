---
title: "Complete manifest reconstruction from source"
description: "Make `gtb regenerate manifest` reconstruct the entire manifest from a project's generated source when the manifest is absent — the full feature set (incl. man and keychain), the bootstrap policy, docs layout, hashes, and every other rendered property — by making the manifest scanner symmetric with the SetFeatures/props-literal renderer over a single per-field source of truth, so a from-scratch rebuild reproduces the manifest and survives a subsequent regenerate project unchanged."
date: 2026-07-11
status: IMPLEMENTED
tags:
  - specification
  - generator
  - manifest
  - features
  - bootstrap
  - regenerate
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude
    role: AI drafting assistant
---

# Complete manifest reconstruction from source

Authors
:   Matt Cockayne, Claude *(AI drafting assistant)*

Date
:   11 July 2026

Status
:   IMPLEMENTED

Related
:   [`2026-07-07-config-section-adapters-for-extraction.md`](2026-07-07-config-section-adapters-for-extraction.md)
    (v0.30.0, where this was surfaced),
    [`2026-07-06-bootstrap-auto-initialise-skip-config-check.md`](2026-07-06-bootstrap-auto-initialise-skip-config-check.md),
    [`2026-06-15-generator-custom-partial-templates.md`](2026-06-15-generator-custom-partial-templates.md)

---

## 1. Context

`gtb regenerate manifest` rebuilds `.gtb/manifest.yaml` by scanning a project's
source. **With the manifest present** it is a clean fixed point — it updates
commands/flags from source and preserves project metadata. **From scratch** (the
manifest deleted first) it recovers only `Name`, `Description`, and a broken
subset of `Features` (`init/update/mcp/docs`), and silently drops everything
else. A subsequent `regenerate project` then strips the `SetFeatures(...)` wiring
and any dropped property, mutilating the generated tool.

Surfaced (and deferred) during the v0.30.0 config-section-adapters work via the
`gtb-generator-manual-test` skill. Pre-existing — v0.29.0 behaves identically.

## 2. Root cause

Two layers, same shape — **the renderer and the scanner are asymmetric**:

1. **Feature model frozen at four.** `extractFeaturesFromSetFeatures`
   (`internal/generator/ast_extract.go`) hardcodes `init/update/mcp/docs` in its
   output list, its default-enabled seed, and its `constToFeature` map. When the
   feature set was expanded (`doctor`, `changelog`, `ai`, `config`, `telemetry`
   toggleable; `keychain` via blank import; `man` as a `FeatureCmd`), this scanner
   was not. So it drops every feature beyond the original four. The correct model
   already exists twice over — `props.AllFeatures`/`props.DefaultFeatures`
   (`pkg/props/tool.go`) and generator `ToggleableFeatures`/`featureDefaultEnabled`
   (`internal/generator/features.go`) — and the scanner reads neither.

2. **Most manifest properties are never parsed back.** The props-literal parser
   (`findPropsLiteralInFunc`) handles only `Name`, `Description`, `Features`, and
   `ReleaseSource`. The renderer (`skeleton_root.go`) emits far more into the
   generated `cmd.go` — `Bootstrap` (`props.BootstrapPolicy{...}`), `Help`,
   `Telemetry`, `Signing`, `EnvPrefix`, update policy — none of which the scanner
   reads. And `manifest_scan.go` copies only `Name`/`Description`/`Features` out of
   the scan result even for what it does parse.

`keychain` is a third wrinkle: it is enabled by a **blank-import artefact**
(`skeleton_keychain.go`), not `SetFeatures`, so it can only be recovered from
file presence.

## 3. Approved decisions (2026-07-11 review)

Resolved with Matt across two rounds:

- **Q1 → robust, expandable, bootstrap-aware.** Not a pick between offered
  options — the requirement is a design with **no fragility**, trivially
  extendable as features are added, that also accounts for the bootstrap policy.
  Resolved as **D1/D2/D6**: one per-field source of truth plus a render↔recover
  symmetry test that fails when they drift, so a new feature or property added to
  the renderer without a recovery path is caught at CI, not in the field.
- **Q2 → include `man` fully** as a toggleable feature (**D5**).
- **Q3 → exact manifest reproduction** (**D7**) — bounded by the one field that
  is not on disk (Templates, below).
- **Q4 → include `docs_layout` and hashes** (**D8**).
- **Q-A (Templates) → reuse the AST-scan pattern; record provenance in a
  dedicated annotated generated file (no boundary).** The original design
  AST-scanned the props definition and looked up defaults; the fix **completes
  that same pattern**. `extractFeaturesFromSetFeatures` /
  `findPropsLiteralInFunc` already are that scan — every props-literal field
  (`Features`, `Bootstrap`, `Help`, `Telemetry`, `Signing`, `EnvPrefix`,
  `UpdatePolicy`, `UpdateCheckInterval`, `ModulePublished`) is recovered by
  extending it, and the non-literal fields by source inspection (below).
  `Templates` provenance is written today only to the manifest
  (`saveManifestTemplates`), so it had no on-disk source. **Resolved (D9): emit a
  dedicated generated file (`// Code generated by gtb. DO NOT EDIT.`) carrying
  structured `// gtb:template …` provenance annotations**, so a from-scratch scan
  reconstructs `Templates[]` too and reproduction becomes exact with **no
  exceptions**. Rejected alternatives, with reasons:
    - *Extend `BootstrapPolicy`* — orthogonal concerns (runtime config lifecycle
      vs generation-time provenance); overloads a well-scoped type.
    - *A `props.Tool.Templates` field* — drags generation-only inventory into the
      **runtime** `Tool` struct (the compiled tool carries lineage it never uses)
      and bloats the public `props` API for a non-runtime concern.
    - *A `.gtb/`-resident marker* — fragile to `.gtb/` deletion (the very failure
      mode we are hardening against); a tracked in-tree file survives it.
    - The chosen annotated file is comment-parseable by the generator's existing
      comment-aware AST (`dave/dst`), keeps provenance out of runtime config, and
      is guarded by the D6 symmetry test.
- **Q-B (CI) → infer from committed CI files** (no marker): `ManifestCI` is just
  `ComponentSource`, read from the `.gitlab-ci.yml` include base; git backend from
  which CI file exists (**D8**).
- **Q-C (feature table home) → explicit exported table in `pkg/props`** (the
  constants' home) as the single origin; the generator derives from it (**D2**).
- **Q-D (hashes) → recompute over current files = exact**; unchanged files
  reproduce the block, which is the clean-rebuild case (**D8**).

## 4. Goals

- A from-scratch `regenerate manifest` reconstructs the **whole** manifest from
  generated source, so delete-then-scan followed by `regenerate project` is a
  fixed point for every project configuration.
- **One source of truth per manifest property**, shared by the renderer and the
  scanner; adding a feature or property means editing one place.
- A **symmetry test** proves `recover(render(x)) == x` across the feature set and
  every reconstructable property — drift fails CI.
- The feature model is complete (`man`, `keychain`) and bootstrap-policy-aware.

## 5. Non-goals

- Changing the runtime feature/bootstrap model, `enable`/`disable`, or how
  `generate` writes the manifest.
- Reworking the with-manifest-present path (already correct).

## 6. Field-by-field recoverability

Every `ManifestProperties` field, where it is rendered, and how it is recovered:

| Field | Rendered into | Recovery |
|---|---|---|
| `Name`, `Description` | `cmd.go` props literal | already parsed |
| `Features` (init…telemetry, **man**) | `SetFeatures(...)` in `cmd.go` | **D1/D2**: parse against the shared feature table |
| `keychain` (feature) | blank-import file | **D3**: presence of the keychain artefact |
| `ReleaseSource` | `cmd.go` props literal | already parsed |
| `Bootstrap` (AutoInitialise/SkipConfigCheck) | `props.BootstrapPolicy{...}` in `cmd.go` | **D4**: extend the parser |
| `Help`, `Telemetry`, `Signing`, `EnvPrefix`, `UpdatePolicy`, `UpdateCheckInterval`, `ModulePublished` | `cmd.go` props literal | **D4**: extend the parser (symmetry test enforces) |
| `DocsLayout` | docs **tree structure** (diataxis vs flat) | **D8**: infer from the directory layout |
| `CI` (`ComponentSource`) | `.gitlab-ci.yml` include base | **D8**: read from the CI file |
| `hashes` | (n/a — integrity block) | **D8**: recompute from the files on disk |
| `Templates` (provenance + pins) | dedicated `// gtb:template …` annotated generated file (new, D9) | **D9**: parse the provenance annotations |

## 7. Proposed decisions

- **D1 — One feature table, shared by renderer and scanner.** Replace the three
  hardcoded structures in `extractFeaturesFromSetFeatures` with lookups over a
  single feature descriptor set: `{name, props const token, default-enabled,
  enablement mechanism (SetFeatures | artefact)}`. The `SetFeatures` renderer in
  `skeleton_root.go` reads the same table. Seed enabled-state from
  `featureDefaultEnabled` and apply parsed `Enable`/`Disable` mutations.
- **D2 — Anchor to props, no stringly convention.** Enumerate the feature
  constants and their tokens from a props-owned table (Q-C), so the mapping has
  one origin and does not rely on fragile `title(value)+"Cmd"` guessing.
- **D3 — Recover `keychain` from its artefact.** Enabled iff the keychain
  blank-import file exists; kept out of the `SetFeatures` parse path.
- **D4 — Make the props-literal parser symmetric with the renderer.** Parse every
  field the renderer emits into the props literal — `Bootstrap`, `Help`,
  `Telemetry`, `Signing`, `EnvPrefix`, update policy, `ModulePublished`. The
  symmetry test (D6) is what keeps them aligned.
- **D5 — `man` is a full toggleable feature.** Add it to `ToggleableFeatures`,
  the feature table, and `enable`/`disable` plumbing.
- **D6 — Render↔recover symmetry test (the anti-fragility mechanism).** A test
  renders a project property/feature configuration, scans it back, and asserts
  equality over the whole reconstructable surface. Adding a feature or rendered
  property without a matching recovery path fails this test. This is how Q1's
  "robust, expandable" requirement is enforced, not just intended.
- **D7 — From-scratch reproduction is exact** over every field in §6, with **no
  exceptions** (Templates now covered by D9).
- **D8 — Recover `docs_layout` (tree), `CI` `ComponentSource` (the `.gitlab-ci.yml`
  include base), and `hashes` (recompute over current files).**
- **D9 — Record `Templates` provenance in a dedicated annotated generated file.**
  `generate` and `regenerate project` emit a `// Code generated by gtb. DO NOT
  EDIT.` file whose header carries one structured `// gtb:template …` line per
  overlay (name, type, location, ref, resolved SHA / fingerprint), derived from
  `manifest.Templates`. `regenerate manifest` parses those annotations (via the
  comment-aware `dave/dst` AST) to reconstruct `Templates[]` from scratch. The
  file is tracked in-tree, so it survives manifest and full `.gtb/` deletion.
  Per-overlay rendered-file `Hashes` are already recomputable from the files
  (D8), so the annotation records provenance + pins only. Not extending
  `BootstrapPolicy` and not adding a `props.Tool.Templates` field — see §3 Q-A.

## 8. Open questions

None outstanding — all resolved in §3. If implementation reveals a props-literal
field the renderer emits that resists AST recovery, the D6 symmetry test surfaces
it and it returns here.

## 9. Acceptance criteria

- No hardcoded feature subset in the scanner; features (incl. `man`) recovered
  via the shared table; `keychain` from its artefact.
- Props-literal parser recovers `Bootstrap`, `Help`, `Telemetry`, `Signing`,
  `EnvPrefix`, update policy, `ModulePublished`; `docs_layout` from the tree;
  `CI ComponentSource` from the CI file; `hashes` recomputed.
- The D6 symmetry test passes and fails on introduced drift.
- Generate-all (incl. a template overlay) → delete manifest → `regenerate
  manifest` → `regenerate project` reproduces the manifest and preserves every
  feature and property, `Templates[]` included (unit + E2E) — no exceptions.
- The `gtb-generator-manual-test` skill's "known boundary" note is updated.

## 10. Implementation status (2026-07-11)

Landed on `feat/manifest-reconstruction`:

- **D1/D2/D5** — one generator-internal `templates.FeatureCatalogue` backs the
  renderer, scanner, `ToggleableFeatures`/`featureDefaultEnabled`, and
  `calculateEnabled/DisabledFeatures`; `man` included; guarded by a
  coverage test against `props.AllFeatures`.
- **Delta form** — generate delta-normalises the manifest (name-sorted); the
  scanner recovers the same delta.
- **D3** — keychain recovered from its blank-import artefact.
- **D4** — the cmd.go Tool-literal parser recovers EnvPrefix, UpdatePolicy,
  UpdateCheckInterval, Help, Telemetry, Bootstrap.
- **D8** — docs_layout from the docs tree, CI ComponentSource from
  `.gitlab-ci.yml`; hashes are repopulated by the subsequent `regenerate
  project` (verified byte-exact end to end).
- **D6** — a from-scratch round-trip guard test.

**Result:** for a project *without template overlays or signing*, a from-scratch
`regenerate manifest` → `regenerate project` reproduces the manifest byte-for-byte.

- **D9** — signing, template-overlay provenance and `module_published` are
  recorded as `// gtb:...` annotations in a generated `pkg/cmd/root/provenance.go`
  (tracked in-tree, skipped by the command scanner, kept in sync at every
  manifest write) and parsed back on a from-scratch rebuild.

**Result:** a from-scratch `regenerate manifest` -> `regenerate project` round
trip now reproduces the manifest **byte-for-byte** for every project
configuration, signing and template overlays included. Spec **IMPLEMENTED**.
