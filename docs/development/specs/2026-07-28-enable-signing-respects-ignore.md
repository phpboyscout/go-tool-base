---
title: "enable/disable signing must respect .gtb/ignore and never clobber a customised .goreleaser.yaml"
description: "gtb enable signing re-renders .goreleaser.yaml from the embedded skeleton, silently destroying a hand-customised release config — including files explicitly protected via .gtb/ignore. The hash-conflict guard cannot protect ignored files (their recorded hash IS the customised content's hash) and enable signing never loads the ignore rules. Honour .gtb/ignore in the targeted enable/disable commands, prefer safe YAML injection of the signs: block, and degrade to a fail-loud advisory paste-block when the file cannot be edited safely."
date: 2026-07-28
status: DRAFT
tags:
  - specification
  - generator
  - signing
  - goreleaser
  - ignore
  - data-loss
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# enable/disable signing must respect `.gtb/ignore` and never clobber a customised `.goreleaser.yaml`

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-28

Status
:   DRAFT

Tracking
:   GitLab issue #4 — "enable signing clobbers a customised `.goreleaser.yaml`, bypassing `.gtb/ignore`"

---

## 1. Reported problem

`gtb enable signing` re-renders `.goreleaser.yaml` from the embedded skeleton,
**silently destroying a customised release config** — including files the
project has explicitly protected via `.gtb/ignore`.

On a downstream project (`krites`) this wiped out an entire shipped feature (the
signed macOS `.app`/`.dmg` packaging) and produced a release config that
**cannot compile**, discovered only when a release job failed after ~36 minutes
of building.

Running `gtb enable signing --key-id … --key-source both --email …` added the
intended `signs:` block, but replaced everything else with the stock skeleton,
dropping:

- the `before` hook staging the ONNX Runtime dylib for app bundling;
- the primary build's `id` / `main` / `binary`;
- a **deliberate Windows exclusion** — replaced with `windows` in `goos` (the
  damaging one: that target genuinely cannot build — POSIX-only
  `purego.Dlopen`/`RTLD_*` and `syscall.SysProcAttr.Setsid` — and the comment
  explaining exactly that was deleted with the exclusion);
- the entire second build (the macOS `.app` executable);
- the entire `app_bundles:` block (icon, `Info.plist`, bundled dylib, LICENSE
  placement);
- the `dmg:` packaging;
- `archives.ids`.

The failure is **silent**: the command reports success and lists the
trustkeys/root-command changes; nothing indicates the release config was
rewritten. A reviewer looking at the added `signs:` block sees exactly what they
asked for — the deletions are elsewhere in a large diff. Any downstream project
with a customised release config is exposed, and the damage surfaces only at
release time.

Reported against `gtb v0.33.0`.

### Reporter's root-cause theory

1. `internal/generator/signing.go:regenerateGoreleaserAsset` renders
   `assets/skeleton/.goreleaser.yaml` over the project's file.
2. The hash guard in `internal/generator/skeleton.go:checkSkeletonConflict`
   cannot protect an ignored, customised file: `regenerate project` records an
   ignored file's **on-disk (customised) hash** ("skip generation but hash
   on-disk content, so drift stays tracked"), so `storedHash ==
   calculateHash(existingContent)` is *true*, the guard returns `nil`, and the
   skeleton overwrites the file.
3. `enable signing` never calls `LoadIgnoreRules` (unlike `regenerate project`).

### Reporter's expected behaviour

1. **Honour `.gtb/ignore`** in `enable`/`disable` commands that write skeleton
   assets, as `regenerate project` does — never overwrite an ignored path.
2. **Prefer a safe injection** of a top-level `signs:` key, leaving everything
   else untouched.
3. **Fail loudly and usefully** when the file cannot be modified safely
   (ignored, customised, unparseable, or a `signs:` block already present):
   don't write; print the exact YAML block plus the path for the user to paste.
   Everything else the command does (the `internal/trustkeys` scaffold, root
   wiring, manifest signing block) can still proceed — only the release-config
   edit degrades to advisory.

---

## 2. Independent root-cause analysis

The reporter's theory was verified independently against **current `main`**
(commit `e7578e20`, well past the reported `v0.33.0`; the `generator-manifest-validation`
/ `generator-followups` work did not touch this path). Each sub-claim is
confirmed with file:line, and one **additional** aggravating factor the report
did not mention was found.

### 2a. Re-render vs. inject — **AGREE**

`internal/cmd/enable/signing.go` → `generator.EnableSigning`
(`internal/generator/signing.go:168`) → `applySigningPosture`
(`signing.go:200`) → `regenerateGoreleaserAsset` (`signing.go:250`).

`regenerateGoreleaserAsset` reads the **entire** embedded skeleton:

```go
content, err := fs.ReadFile(skeletonAssets, goreleaserAssetEmbedPath) // "assets/skeleton/.goreleaser.yaml"
…
hash, err := g.renderAndHashSkeletonTemplate(
    filepath.Join(g.config.Path, goreleaserAssetRelPath), // ".goreleaser.yaml"
    goreleaserAssetRelPath, string(content),
    g.buildSkeletonTemplateData(*m), storedHashes,
)
```

It renders the whole skeleton template and writes it over the project file — it
does **not** inject a `signs:` block. The skeleton
(`internal/generator/assets/skeleton/.goreleaser.yaml:47`) gates the whole
`signs:` block on `{{ if and .Signing.Enabled .Signing.KeyID }}`, confirming the
signs block is produced by a full re-render, not a surgical edit.

> Note: `signing_merge_test.go` / `mergeSigning` concern **manifest-block**
> field merging on re-run (preserving email/key across invocations); they do
> **not** merge or inject into `.goreleaser.yaml`.

### 2b. `enable signing` never calls `LoadIgnoreRules` — **AGREE**

`LoadIgnoreRules` is invoked only from the full skeleton path
(`skeleton.go:318`), and the ignore check itself lives only in
`walkSkeletonAssets` (`skeleton.go:615`):

```go
if rules.IsIgnored(relPath) {
    g.props.Logger.Debug("ignored by .gtb/ignore", "path", relPath)
    g.hashIgnoredFile(destPath, relPath, collectedHashes)
    return nil
}
```

`regenerateGoreleaserAsset` bypasses `walkSkeletonAssets` entirely — it calls
`renderAndHashSkeletonTemplate` → `writeRenderedSkeletonFile` →
`checkSkeletonConflict` directly, none of which is ignore-aware. Grep confirms
no `LoadIgnoreRules` / `IgnoreRules` reference anywhere in `signing.go` or the
`enable`/`disable` command packages. So `.gtb/ignore` has **no effect** on
`enable`/`disable signing`.

Note also that `.goreleaser.yaml` is deliberately **excluded** from
`isUserOwnedSeedFile` (`skeleton.go:745`, comment: "Framework-structural files
(CI pipelines, .goreleaser.yaml, …) are deliberately NOT in this set"), so the
seed-file preservation path does not save it either.

### 2c. The hash guard structurally cannot protect an ignored file — **AGREE**

The guard (`skeleton.go:760`):

```go
storedHash := storedHashes[relPath]
if storedHash == "" || storedHash == calculateHash(existingContent) || g.config.Force {
    return nil // proceed to overwrite
}
```

The stored hash for an ignored file is written by `hashIgnoredFile`
(`skeleton.go:642`), which reads the **on-disk** content and stores
`calculateHash(content)`. That flows into the manifest via
`writeSkeletonManifest` / `mergeHashes`. So after any `regenerate project`,
`m.Hashes[".goreleaser.yaml"]` **is the customised content's hash**.

In `regenerateGoreleaserAsset`, `storedHashes := m.Hashes`. At the guard,
`existingContent` is the customised on-disk file, so
`calculateHash(existingContent)` equals the recorded `storedHash` — the guard
takes the "unchanged, proceed to overwrite" branch and the skeleton is written
over the customisation. The guard's question is "has the file drifted from what
gtb last recorded?"; for an ignored file gtb records the *customised* content,
so there is by construction **no detectable drift** — the guard can never fire
for it. **Structurally incapable, exactly as reported.**

### 2d. Additional factor not in the report — `Overwrite: "allow"` neuters the guard regardless

`enable signing` constructs the generator with `Overwrite: "allow"`
(`internal/cmd/enable/signing.go:191`). Even in the sub-case where the guard
*does* detect a conflict — e.g. the file was customised but a `regenerate` has
**not** re-recorded its hash, so `storedHash` still holds the stock skeleton's
hash and `storedHash != calculateHash(existingContent)` — `checkSkeletonConflict`
then calls `promptOverwrite` (`skeleton.go:773`), which for `Overwrite: "allow"`
returns `true` unconditionally (`hash.go:78`). So the customised file is
clobbered **whether or not** its hash was recorded. This means:

- the bug reproduces even for a customised-but-not-yet-regenerated
  `.goreleaser.yaml`, not only the reporter's recorded-hash scenario; and
- simply "fixing the hash bookkeeping" would **not** be sufficient — under
  `Overwrite: "allow"` the guard offers no protection at all. The real fix must
  be an explicit ignore check (and/or not re-rendering the whole file), not a
  tweak to hashing.

`disable signing` drives the identical `applySigningPosture` →
`regenerateGoreleaserAsset` path, so it has the same defect.

**Verdict:** the reporter's root cause is correct on all three sub-claims. The
only refinement is that the hash-bookkeeping issue (2c) is not the *sole* reason
the overwrite happens — `Overwrite: "allow"` (2d) independently guarantees it —
which strengthens the case for the reporter's proposed solution direction over
any hash-only fix.

---

## 3. Validation on current `main` + red evidence

Two red tests were added (branch `fix/enable-signing-respects-ignore`,
`internal/generator/signing_goreleaser_ignore_test.go`). Both scaffold a
signing-less project on a real `afero.OsFs`, overwrite `.goreleaser.yaml` with a
customised release config carrying five sentinel markers (an onnx staging hook,
a deliberate `linux, darwin`-only build with a Windows-exclusion comment, a
second `krites-app` build, an `app_bundles:` block, and a `dmg:` block), list
`.goreleaser.yaml` in `.gtb/ignore`, and record the customised on-disk hash into
the manifest — exactly the state `regenerate project` leaves an ignored file in.
They then run `EnableSigning` (the work `gtb enable signing --key-id …` does).

- `TestEnableSigning_PreservesCustomisedIgnoredGoreleaser` — asserts each
  sentinel survives.
- `TestEnableSigning_HonoursGtbIgnore` — asserts the ignored file is byte-for-byte
  unchanged.

Both **FAIL on current `main`**: after `EnableSigning` the file is the stock
`sign-tool` skeleton plus the `signs:` block — every sentinel gone.

```
--- FAIL: TestEnableSigning_PreservesCustomisedIgnoredGoreleaser (0.01s)
    … does not contain "stage-onnxruntime.sh"
    Messages: customised .goreleaser.yaml lost "stage-onnxruntime.sh" — enable signing clobbered an ignored file
    … does not contain "Windows deliberately excluded"
    … does not contain "krites-app"
    … does not contain "app_bundles:"
    … does not contain "dmg:"
--- FAIL: TestEnableSigning_HonoursGtbIgnore (0.01s)
    Not equal:
    expected: "# CUSTOMISED BY THE PROJECT … app_bundles: … dmg: …"
    actual  : "version: 2\nproject_name: \"sign-tool\" … signs:\n  - id: checksums …"
    Messages: path listed in .gtb/ignore was modified by enable signing
FAIL	gitlab.com/phpboyscout/go-tool-base/internal/generator
```

The bug is **present and reproduced on current `main`**.

---

## 4. Proposed solution direction

Three mechanisms, assessed. The recommendation combines all three: an ignore
gate (safety), safe injection (the happy path), and a fail-loud advisory
fallback (when injection is unsafe or impossible).

### 4.1 Honour `.gtb/ignore` in `enable`/`disable` (and any targeted asset write)

Load the ignore rules in `applySigningPosture` (covering both enable and
disable) and short-circuit `regenerateGoreleaserAsset` when `.goreleaser.yaml`
is ignored — mirroring `walkSkeletonAssets`, including re-recording the on-disk
hash so drift stays tracked.

- **Pro:** directly closes the reported hole; consistent with `regenerate
  project`; cheap; also neutralises the 2d `Overwrite: "allow"` factor for
  ignored paths.
- **Con:** on its own it only protects *ignored* files. A customised-but-not-
  ignored `.goreleaser.yaml` is still fully re-rendered and clobbered under
  `Overwrite: "allow"` (2d). Necessary but **not sufficient**.
- **Verdict:** adopt as the safety floor, but pair with 4.2/4.3 so
  non-ignored customisation is also protected.

### 4.2 Safe YAML injection of the `signs:` block

Instead of re-rendering the whole skeleton, parse the existing
`.goreleaser.yaml`, add/replace **only** the top-level `signs:` key (and remove
it on disable), and re-serialise — leaving every other block untouched.

- **Pro:** preserves arbitrary customisation by construction; the command does
  what its output claims (adds a signs block) and nothing else; works whether or
  not the file is ignored.
- **Con:** YAML round-trips are lossy for comments/anchors/formatting unless a
  comment-preserving approach is used. Options to weigh:
  - `gopkg.in/yaml.v3` `yaml.Node` (already a dependency) preserves comments and
    ordering if edited node-wise rather than decoded into structs.
  - A targeted **textual** insert/removal of the `signs:` block (locate the
    top-level key, splice) avoids a full re-serialise but needs care with
    document markers and existing `signs:`.
  - Injection into a genuinely unparseable file must be refused (→ 4.3).
- **Verdict:** the preferred happy path. Prototype the `yaml.Node` route first;
  fall back to 4.3 whenever the shape isn't safely editable.

### 4.3 Fail-loud advisory paste-block fallback

When the release-config edit cannot be done safely — the path is ignored, the
file is customised beyond safe injection, it is unparseable, or a `signs:` block
already exists — **do not write**. Emit a clear message with the path and the
exact YAML block to paste, e.g.:

```
.goreleaser.yaml is customised (listed in .gtb/ignore) — not modified.
Add the following top-level block to enable release signing:

signs:
  - id: checksums
    cmd: gtb
    args: ["--ci","sign","--backend","aws-kms", …]
    artifacts: checksum
    signature: "${artifact}.sig"
    output: true
```

Crucially, **the rest of `enable signing` still proceeds** — the
`internal/trustkeys` scaffold, root-command wiring, and manifest signing block
are unaffected; only the release-config edit degrades to advisory. The command's
summary must state clearly that the release config was **not** modified and
that manual action is required (i.e. remove the false "success, nothing else
changed" impression that makes the current failure silent).

- **Pro:** no data loss is ever possible; the user gets an actionable next step;
  makes the previously-silent case loud.
- **Con:** manual step for the ignored/customised case — acceptable, and the
  correct trade-off versus clobbering.
- **Verdict:** adopt as the universal fallback and the disable-side story
  (advise which block to remove).

### Recommended shape

1. `applySigningPosture` loads ignore rules once and passes an
   "editable?" decision to the goreleaser step.
2. Not ignored **and** safely injectable → inject the `signs:` block (4.2).
3. Ignored, unsafe, unparseable, or `signs:` already present → advisory
   paste/removal block (4.3), continue with everything else, and report clearly.
4. Stop constructing the goreleaser write with an unconditional `Overwrite:
   "allow"` bypass for this asset; the ignore/injection logic — not a blanket
   force — decides whether to write.

---

## 5. Acceptance criteria

1. A customised `.goreleaser.yaml` **listed in `.gtb/ignore`** is left
   byte-for-byte unchanged by `gtb enable signing` and `gtb disable signing`
   (the two red tests in §3 pass).
2. A customised `.goreleaser.yaml` **not** listed in `.gtb/ignore` retains all
   of its customisation: `enable signing` adds only the top-level `signs:` block
   (and `disable signing` removes only that block); no other block, comment, or
   the deliberate platform exclusion is lost.
3. When the release-config edit cannot be performed safely, the command:
   - does **not** modify the file;
   - prints the exact `signs:` block (enable) or the block to remove (disable),
     with the file path;
   - **still** performs the trustkeys scaffold, root-command wiring and manifest
     update; and
   - reports clearly that the release config was not modified and manual action
     is required.
4. The manifest hash for `.goreleaser.yaml` continues to track the on-disk
   content after enable/disable (no spurious future-drift warnings), including
   for ignored files.
5. No regression to the fresh-`generate` path: a newly scaffolded project with
   `signing.enabled && key_id` still renders the complete `signs:` block
   (existing `signing_goreleaser_test.go` assertions continue to pass).
6. E2E/BDD coverage for the targeted `enable signing` workflow asserting the
   ignore-honouring and advisory-fallback behaviour (per the repo's "new CLI
   command / workflow changes must include Gherkin" rule).
7. `docs/` updated: the `enable signing` / signing component docs describe the
   `.gtb/ignore` interaction and the advisory fallback.

---

## 6. Open questions

1. **Injection engine.** `yaml.v3` `yaml.Node` (comment-preserving, already a
   dependency) vs. a targeted textual splice? Confirm `yaml.Node` round-trips
   the skeleton's block scalars (`name_template: >-`) and comments without loss
   before committing to it.
2. **Scope of the ignore/injection fix.** Only `.goreleaser.yaml` via the
   signing commands, or should every targeted `enable`/`disable` command that
   writes a skeleton asset (e.g. `enable mcp`) route through the same
   ignore-aware, inject-or-advise helper? Recommend generalising to avoid the
   same class of bug elsewhere.
3. **`signs:` already present.** On `enable`, if a `signs:` block already exists
   (author-written or a prior run), replace it, leave it, or advise? Leaning
   "advise and don't touch" to avoid clobbering an author-tuned block.
4. **Disable semantics for an injected block.** Should `disable signing` only
   remove a `signs:` block gtb itself injected (e.g. tagged by the `# gtb enable
   signing wired this` marker or `id: checksums`), and otherwise advise, to avoid
   deleting an author-written signing block?
5. **Detecting "customised".** For the non-ignored case, is the trigger for
   advisory-vs-inject purely "does safe injection succeed", or should a
   hash-drift signal also force advisory even when injection *would* succeed?
6. **`Overwrite: "allow"` audit.** Are there other targeted commands relying on
   `Overwrite: "allow"` to write framework-structural files that could clobber
   customisation the same way? Worth a sweep as part of question 2.
7. **Backfill for already-damaged projects.** Out of scope for the fix, but
   should `doctor` gain a check that flags a `.goreleaser.yaml` matching the
   stock skeleton in a project whose manifest implies customisation, to catch
   silent prior damage?
