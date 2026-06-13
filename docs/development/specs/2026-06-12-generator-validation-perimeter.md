---
title: "Generator validation perimeter — close the manifest/signing/AI-tool input gaps"
description: "Extend the generator's input-validation gate so every user-influenced field that flows into a file path or a non-code render site is validated and escaped. Closes the two high-severity generator findings from the 2026-06-12 audit (command-name path traversal via a tampered manifest, and unescaped/unvalidated ManifestSigning fields injected into the CI-executed .goreleaser.yaml), plus the AI doc-tool path-traversal and a sentinel-with-format-verb defect."
status: APPROVED
date: 2026-06-12
tags:
  - specification
  - generator
  - security
  - template-security
  - validation
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-fable-5)
    role: AI drafting assistant
---

# Generator validation perimeter

Authors
:   Matt Cockayne, Claude (claude-fable-5) *(AI drafting assistant)*

Date
:   2026-06-12

Status
:   APPROVED (open questions resolved in review 2026-06-12)

## Summary

The code generator (`internal/generator/`) renders user-influenced fields into scaffolded files. It has a well-built escaping layer (`template_escape.go`) and a validator (`validate.go`), but the audit found the **perimeter** is incomplete: some inputs reach a `filepath.Join`/`RemoveAll` or a non-code render site without passing through validation or escaping. This spec closes those gaps so the documented template-security model (`docs/development/template-security.md`) holds for *every* user-influenced field, not just the project-name/description/host/org fields it currently covers.

Findings addressed (from `docs/development/reports/codebase-audit-2026-06-12.md`):

- `command-name-no-validation-path-traversal` — **High, security**
- `signing-fields-unvalidated-unescaped-yaml-injection` — **High, security**
- `ai-doc-tools-path-traversal` — **Medium, security**
- `generator-sentinel-with-format-verb` — **Medium, bug** (folded in: it lives in the same files and is a correctness pre-req for clean error reporting)
- `remove-generate-command-name-path-traversal` — **Low** (resolved as a side effect of `ValidateCommandName`)

## Motivation

`ValidateManifest` is documented as the gate that forecloses path-traversal and injection from a tampered `.gtb/manifest.yaml`. In practice it validates only `Properties` and `ReleaseSource` and **never walks `m.Commands` or `m.Signing`**. Two concrete exploit primitives result:

1. **Path traversal (write + recursive delete).** Command names get only `not "options"` / `not "root"` / `no-spaces` checks, then flow into `filepath.Join(g.config.Path, "pkg", "cmd", name)` and `FS.RemoveAll(cmdDir)`. `filepath.Join` cleans `..`, so a crafted name escapes the project tree. The most serious vector is `regenerate`: a tampered manifest drives writes/deletes outside the tree despite the gate whose documented purpose is to prevent exactly this.

2. **YAML injection into a CI-executed file.** `Backend`, `KMSRegion`, `KeyID`, `PublicKey` are interpolated into double-quoted YAML scalars in `skeleton/.goreleaser.yaml` with **no `escapeYAML` pipe** and **no validation**. A value containing a quote + newline breaks out of the scalar and injects keys into the GoReleaser `signs` block, which runs in CI at release time.

A third, lower-severity gap: the AI doc-generation tools (`handleReadFileTool`/`handleListDirTool`) join LLM-supplied paths to the project root with no containment check, while the sibling `handleGoDocTool` validates its argument.

## Design decisions

### D1 — A single `ValidateCommandName` applied at every entry point

Add `ValidateCommandName(name string) error` to `validate.go` enforcing an NFC-normalised character class of **kebab plus underscore** — `^[a-z][a-z0-9_-]{0,63}$` (resolved O1: underscores are permitted for downstream snake_case command names) — and explicitly rejecting `/`, `.`, and `..`. Call it from:

- `generate`/`remove` CLI option validation (`internal/cmd/generate`, `internal/cmd/remove`) — here an invalid name is a **hard error** to the user (they typed it), and
- **recursively over `m.Commands`** inside `ValidateManifest`, so the `regenerate` path (which trusts the manifest) is covered.

This is the load-bearing fix: validating only at the CLI flag is insufficient because `regenerate` reads names from the manifest, not flags.

For the manifest path, an invalid command **does not abort** the whole regeneration (resolved O3): the offending command is **skipped** and the generator emits an **ERROR-level log** naming the command and the validation failure, so the user clearly sees there is a problem while the valid commands still regenerate. (The traversal-into-`filepath.Join`/`RemoveAll` is still foreclosed because a skipped command is never acted on.) The signing-field path is treated the same way — an invalid `ManifestSigning` field skips signing-block rendering with an ERROR log rather than aborting.

### D2 — Escape and validate every `ManifestSigning` field

Pipe every `Signing.*` value rendered into `skeleton/.goreleaser.yaml` through `escapeYAML` (matching `project_name: {{ .Name | escapeYAML }}`), and add field-specific validators invoked from `ValidateManifest`:

- `Backend` — registered-backend-name character class.
- `KMSRegion` — AWS region character class (`[a-z0-9-]`).
- `KeyID` — KMS key-id / alias character class.
- `PublicKey` — **path validation** (resolved O2: `manifest.go` documents this field as "the path to the armored public-key file", defaulting to `internal/trustkeys/keys/signing-key-v1.asc`). Validate it as a clean relative path under the project (no `..`/absolute escape), not as inline armor.

Both halves are required: escaping prevents scalar breakout; validation rejects nonsense before it reaches CI.

### D3 — Contain AI doc-tool paths

**Confirmed during review:** `handleReadFileTool` (`docs.go:621`) and `handleListDirTool` (`docs.go:639`) are **not** bound to the project root — they `filepath.Join(g.config.Path, params.Path)` with no post-join containment, so a model-supplied `../../etc/passwd` escapes. The go-doc path (`docs.go:443-465`) *does* contain via `filepath.Abs` + `filepath.Rel`.

Resolution (O4): extend the **same** `filepath.Abs` + `filepath.Rel` containment guard the go-doc path already uses to `handleReadFileTool`/`handleListDirTool`, rather than introducing a new `BasePathFs` — keeps the two code paths consistent. After joining, compute the path relative to the absolute project root and reject any result beginning with `..`.

### D4 — Make the project sentinel placeholder-free

`ErrNotGoToolBaseProject` embeds a literal `%s`. One call site concatenates it into a `Newf` without `%w` (breaking `errors.Is`); another wraps with `%w` but supplies no path (rendering literal `at '%s'`). Make the sentinel placeholder-free and attach the path via `errors.Wrapf` at call sites. This is in scope because the validator additions need clean, matchable errors.

## Open questions

*All resolved during review (2026-06-12).*

1. **O1 — Character-class for command names.** *Resolved:* kebab **plus underscore** — `^[a-z][a-z0-9_-]{0,63}$` — so downstream snake_case command names are permitted. (D1)
2. **O2 — `PublicKey` field.** *Resolved:* it is a **path** to an armored public-key file (`manifest.go:215`), validated as a clean project-relative path. (D2)
3. **O3 — Invalid command/signing field in a manifest.** *Resolved:* **skip** the offending entry and emit an **ERROR-level log** (not abort), so the valid entries still regenerate while the problem is clearly surfaced. CLI-flag inputs remain a hard error. (D1)
4. **O4 — AI doc-tool containment.** *Resolved:* `handleReadFileTool`/`handleListDirTool` are currently unguarded (confirmed in code); extend the existing `filepath.Abs`+`filepath.Rel` containment guard the go-doc path already uses to them. (D3)

## Verification plan

1. **Unit** — `validate_test.go`: `ValidateCommandName` accepts the kebab class and rejects `..`, `/`, `.`, leading digit, over-length, and the reserved names. New cases proving `ValidateManifest` walks `Commands` and `Signing`.
2. **Traversal regression** — a tampered manifest with `Commands: [{name: "../../evil"}]` fed to the `regenerate` path must error (not write/delete outside the tree). A `ManifestSigning.KeyID` containing `"\n  - artifact: pwned"` must be rejected and, if it somehow rendered, escaped.
3. **AI doc-tool** — `handleReadFileTool` with `../../../../etc/passwd` returns a containment error.
4. **Scaffold check** — `just build && go run ./cmd/gtb generate project -p tmp` then `regenerate`, verify `tmp/.goreleaser.yaml` signing block is well-formed; delete `tmp`.
5. **Docs** — update `docs/development/template-security.md` to list the newly-validated fields.

## Out of scope

- Re-architecting the escaping helpers (they are sound; this is a perimeter fix).
- Signing-feature behaviour beyond input validation (covered by `2026-06-10-signing-generator-feature.md`).

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.1
- [Plan 1 — security & bugs](../plans/2026-06-12-audit-plan-1-security-and-bugs.md) Phase 1
- `docs/development/template-security.md`
- [signing generator feature spec](2026-06-10-signing-generator-feature.md)
