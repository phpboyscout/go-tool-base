---
title: "Generator: batched MEDIUM/LOW follow-ups from the architectural review"
description: "Grouped remediation of the remaining internal/generator findings from the 2026-07-23 architectural review: protected-command removal, broken documentation cleanup, non-atomic real-FS generation, provenance KV corruption on spaced values, swallowed verification errors, the scaffolded telemetry Basic-auth encoding gap, and structural cleanups (duplicate tree-walkers, duplicate encode helpers, scratch comments, misleading version-gate message, unbounded port digits)."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - generator
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Generator: batched MEDIUM/LOW follow-ups from the architectural review

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (generator §MEDIUM/LOW findings + gtb-core §LOW telemetry-auth finding),
    [generator manifest validation hardening](2026-07-23-generator-manifest-validation-hardening.md) (sibling spec; carries the HIGH manifest-validation scope and the docs-escaping MEDIUM — both excluded here)

---

## 1. Context

The 2026-07-23 architectural review of `internal/generator/` found the core mechanisms
(manifest-driven scaffolding, hash-based overwrite protocol, two-layer template
security) sound, with the significant validation gaps covered by the sibling
manifest-validation-hardening spec. This spec batches the remaining MEDIUM and LOW
findings — destructive-operation safety holes, atomicity/integrity gaps, one
generator-template correctness bug from the gtb-core review, and structural cleanup —
so they can be implemented and reviewed together without each needing its own spec.

## 2. Items

### 2.1 Destructive-operation safety

#### 2.1.1 `gtb remove command` deletes protected commands — MEDIUM

`performRemoval` (`internal/generator/removal.go:52-71`) walks straight to
`g.props.FS.RemoveAll(cmdDir)` with no `Protected` check, while generation
(`checkProtection`, `internal/generator/commands.go:242-274`) and the MCP toggle
(`SetMCPEnabled`, `internal/generator/generator.go:209-212`) both enforce protection.
An operator marks a command protected precisely because it carries hand-written
logic, and removal deletes the whole directory — preserved `main.go` included.

**Direction:** in `performRemoval`, look up the manifest entry *before* mutating
anything and refuse with `ErrCommandProtected` when `Protected` is true, mirroring
`SetMCPEnabled`; allow override only via an explicit `--force` flag.

**Acceptance:** removing a protected command exits with the protection error and
leaves the tree untouched; `--force` removes it.

#### 2.1.2 `cleanupDocumentation` is doubly broken — MEDIUM

`cleanupDocumentation` (`internal/generator/removal.go:64-80`) calls
`g.FindCommandParentPath(g.config.Name)` after `performRemoval` step 2 has already
deleted the command from the manifest, so the lookup always fails and a nested
`parent/child` removal targets `docs/commands/child` instead of
`docs/commands/parent/child`. Worse, the path is hardcoded to the legacy flat
`docs/commands/…` layout, while the default Diátaxis layout
(`internal/generator/skeleton.go:809`) stores command docs under
`docs/reference/cli/<path>.md` (`internal/generator/docs.go:707-721`) — so on every
current-default project removal cleans up nothing and the regenerated index/nav still
reference the dead page.

**Direction:** capture the parent path before mutating the manifest (it is already in
`g.config.Parent`) and resolve the doc path through the same
`prepareDocsContext`/`commandDocRelPath` machinery generation uses.

**Acceptance:** removing a nested command deletes its doc page in both layouts, and
the regenerated index/nav no longer reference it.

### 2.2 Atomicity & integrity

#### 2.2.1 Generation/regeneration is non-atomic on the real FS — MEDIUM

`internal/generator/skeleton.go:458-483` and `internal/generator/regenerate.go:94-140`
write files to the target FS one-by-one, with project hashes persisted only at the end
(`regenerate.go:570`) and the pipeline's manifest write treated as advisory
(`internal/generator/pipeline.go:87-90`). A mid-run failure leaves written files whose
manifest hashes are stale — the next regenerate flags every one as "manually modified"
(or, under `--overwrite allow`/`--force`, overwrites genuinely user-modified files
indistinguishably). The dry-run path proves a copy-on-write overlay already exists
(`internal/generator/dryrun.go:99-131`); the real path never uses it.

**Direction:** route real generation/regeneration through the existing overlay FS and
materialise to the target FS as a final commit step that includes the manifest/hash
write (promoting the pipeline manifest write from advisory to part of the commit);
failing that, persist hashes incrementally per written file.

**Acceptance:** a regenerate aborted mid-run (fault-injection test) leaves tree and
manifest mutually consistent — the next regenerate reports no spurious conflicts.

#### 2.2.2 Provenance KV encoding corrupts values containing spaces — MEDIUM

`encodeKV`/`decodeKV` (`internal/generator/provenance.go:183-209`) join `key=value`
pairs space-separated and split with `strings.Fields`, assuming recorded values are
token-like. But a **local** template-source `Location` is a filesystem path and
`validateTemplateLocation` (`internal/generator/templatesource_validate.go:80-108`)
permits spaces: an overlay at `/home/me/My Templates/gtb` round-trips through a
from-scratch `gtb regenerate manifest` as `location=/home/me/My`, silently dropping
the rest — defeating the exact reconstruction the provenance file exists for.
`ExternalKeyEmail` shares the fragility.

**Direction:** percent-encode values in `encodeKV` (decode symmetrically, with a
fallback for existing un-encoded provenance lines), *or* reject spaces in
`validateTemplateLocation` for local sources. Prefer encoding: it fixes the class.

**Acceptance:** a template-source location containing spaces round-trips byte-exact
through provenance encode → decode → manifest reconstruction.

#### 2.2.3 Swallowed errors in verifyProject and regenerate post-processing — LOW

`verifyProject` (`internal/generator/generator.go:259-262`) does
`m, _ := g.loadManifest(); if m == nil { return nil }` — a present-but-corrupt
manifest passes project verification and silently skips the version-compatibility
gate. Similarly `internal/generator/regenerate.go:82-86` skips all post-regeneration
processing (linter, hash refresh) when `collectSkeletonHashes` errors, with no log.

**Direction:** return the decode error from `verifyProject` when the manifest file
exists but fails to load; log `collectSkeletonHashes` failures at WARN.

**Acceptance:** a corrupt `.gtb/manifest.yaml` fails `verifyProject` with the decode
error; a `collectSkeletonHashes` failure is visible in the log output.

### 2.3 Template correctness

#### 2.3.1 Scaffolded telemetry auth env fallback is not base64-encoded — LOW (from gtb-core review)

The generated root's `init()` assigns the raw env value — `otelAuth = v` from
`OTEL_API_KEY` (`internal/generator/templates/skeleton_root.go:64-79`) — yet the
header renders as `"Authorization": "Basic " + otelAuth`
(`skeleton_root.go:280-283`). RFC 7617 Basic credentials must be `base64(user:pass)`,
and gtb's own root encodes `base64(otelInstanceID + ":" + raw)`
(`internal/cmd/root/root.go:45-47`) — so every scaffolded tool's local-dev telemetry
auth sends a malformed header the collector rejects.

**Direction:** update the jennifer template so the generated `init()` base64-encodes
`instanceID + ":" + raw`, matching gtb's own root; the ldflags-injected value
(already encoded at build time) stays untouched.

**Acceptance:** a freshly scaffolded project with `OTEL_API_KEY` set emits an
`Authorization: Basic <base64(id:key)>` header matching gtb's own root in shape.

### 2.4 Structural cleanup

#### 2.4.1 Seven hand-rolled manifest tree-walkers + duplicated dedupe — LOW

Seven functions — `findCommandAt`/`findCommandRecursive`/`findCommandPathRecursive`/
`updateProtectionRecursive`/`removeFromCommandRecursive`
(`internal/generator/manifest_query.go:32,136,53,194,252`) plus
`updateCommandRecursive`/`updateCommandHashRecursive`
(`internal/generator/manifest_update.go:140,262`) — reimplement the same
walk-parent-path-then-act recursion, each with its own `//nolint:gosec // G602`
annotations; the duplicate-name rename block appears verbatim twice
(`internal/generator/manifest_scan.go:132-146` and `289-299`).

**Direction:** one generic `walkCommandPath` + visitor callback, and a shared rename
dedupe helper — expected to drop roughly 150 LOC and five nolint suppressions with no
behaviour change.

**Acceptance:** all manifest query/update/removal tests pass unchanged against the
single walker; the gosec suppressions collapse to the walker's one audited site.

#### 2.4.2 Duplicate encode helpers with a stale comment — LOW

`MarshalManifestFile` (`internal/generator/manifest.go:64-81`) and
`EncodeManifestFile` (`manifest.go:51`) both delegate to `marshalManifestBytes`, yet
`MarshalManifestFile`'s doc comment still claims an indentation difference that no
longer exists.

**Direction:** collapse to one helper (keep `EncodeManifestFile`, repoint callers).

**Acceptance:** one manifest-write helper remains; byte-stability tests pass.

#### 2.4.3 Scratch comments and dead reflection fallback — LOW

`SetProtection` (`internal/generator/generator.go:155-180`) ships
stream-of-consciousness drafting comments ("Wait, we can modify the Generator
receiver… But sticking to the plan…"); similar residue at
`internal/generator/manifest.go:277-280`. `extractReleaseProvider`'s reflection
fallback (`internal/generator/skeleton.go:119-137`) is dead per its own companion
comment (`internal/generator/regenerate.go:488-491`).

**Direction:** replace scratch comments with a one-line statement of intent; delete
the dead reflection branch.

**Acceptance:** no drafting-monologue comments remain; deadcode/lint clean.

#### 2.4.4 Misleading version-gate error and unbounded port digits — LOW

A manifest with `gtb: dev` yields "current gtb version (X) is lower than the version
specified in the manifest (dev)" (`internal/generator/generator.go:280-283`) — the
real condition is "generated by a dev build". Separately,
`internal/generator/validate.go:471-482` checks port digits only for numeric
characters, so `host:99999` passes.

**Direction:** give the dev-manifest case its own message; parse the port and bound
it to 1–65535.

**Acceptance:** a `gtb: dev` manifest produces a message naming the dev-build
condition; ports `99999` and `0` are rejected with a range error.

## 3. Scope & release plan

- GTB-repo only, entirely `internal/` — no public `pkg/` API changes, no migration
  notes needed. Ships as `fix(generator)` commits (patch), one coherent commit per
  item group; the walker consolidation may go as `refactor(generator)`.
- After implementation, run the scaffold-verification step:
  `just build && go run ./cmd/gtb generate <command> -p tmp`, verify `tmp/`
  (generated root `init()` base64 encoding, doc layout paths), then delete it.
- Regenerate-behaviour changes (2.1.2, 2.2.1, 2.2.3) must be exercised through the
  generator E2E suite with `-count=1` — Godog results are go-test-cached and the
  cache does not invalidate on `internal/` changes.
- `/gtb-verify` (tests, race detector, lint, mocks) before raising the MR. No new CLI
  surface except the possible `remove --force` flag, which needs a Gherkin scenario
  if added.

## 4. Open questions — RESOLVED

1. **Atomicity approach (2.2.1):** RESOLVED — overlay for **regenerate only**,
   where the misclassification damage actually occurs. `regenerateProject` now runs
   the docs migration + file writes + manifest/hash persistence through a staged
   copy-on-write overlay (`stagedFS`, `internal/generator/materialise.go`) and
   materialises to the real filesystem as a single commit step only once every step
   succeeds; a mid-run failure leaves the tree and manifest untouched. The staged FS
   records base-file removals (rather than EPERM-refusing) so deletion semantics
   (e.g. dropping `signing.go`) are preserved through the buffer. `generate skeleton`
   was deliberately left on its existing path.
2. **Provenance encoding (2.2.2):** RESOLVED — percent-encode values in `encodeKV`
   (`url.QueryEscape`), decode with `url.QueryUnescape`, which is an identity for the
   legacy token-like (space-free) values, so it doubles as the backward-compat
   fallback for provenance files written before encoding.
3. **Protected removal override (2.1.1):** RESOLVED — `gtb remove command --force`
   is the escape hatch. The Protected check runs first (`checkRemovalProtection`,
   before any mutation); `--force` overrides it.
4. **Dev-manifest gate (2.4.4):** RESOLVED — a release CLI **hard-refuses** a
   `gtb: dev` (and version-less) manifest, but with a dedicated, clear message naming
   the dev-build condition instead of the misleading "your gtb is lower than the
   manifest" wording.
