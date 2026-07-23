---
title: "Generator: close the manifest-validation gaps behind CI-executed and code-generating sinks"
description: "ValidateManifest promises that a tampered manifest fails fast before driving file writes, but several fields it skips render — unescaped — into CI-executed files (.gitlab-ci.yml, .goreleaser.yaml), into generated Go source (default_is_code flag defaults via jen.Id), and into the provenance annotation. The boilerplate docs builder additionally renders command/flag descriptions into Markdown with neither validation nor escaping. This spec extends the existing two-layer gate (character-class validators + per-sink escapes) to cover every field the renderers actually consume."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - generator
  - security
  - validation
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Generator: close the manifest-validation gaps behind CI-executed and code-generating sinks

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (generator §HIGH manifest-validation + §MEDIUM docs-escaping findings),
    [template security](../template-security.md) (the two-layer validate + escape model this spec extends)

---

## 1. Problem

`ValidateManifest` states its own contract (`internal/generator/validate.go:868-872`): every
user-influenced field of a loaded manifest is validated "so a tampered manifest fails fast
before driving file writes". The regenerate and manifest-update paths rely on that gate —
manifest fields do **not** pass through the CLI-flag validators, and several of them reach
render sinks the gate never inspects. This is defensive hardening of the project's own
template-injection model: `.gtb/manifest.yaml` is an on-disk, hand-editable file, and the
threat model (a tampered or hostile manifest, e.g. in a cloned repository the operator
regenerates) is exactly the one `ValidateManifest` documents.

Concrete gaps, each verified against the current source:

1. **`ReleaseSource.Repo` is never validated on the manifest path.**
   `validateManifestReleaseSource` (`validate.go:995-1009`) checks only `Host` and `Owner`;
   `ValidateRepo` (`validate.go:428`) runs only on the CLI `--repo` flag. Yet
   `buildSkeletonTemplateData` (`internal/generator/regenerate.go:497-510`) builds
   `Repo = org + "/" + repoName` and `ModulePath = host + "/" + org + "/" + repoName`
   from the manifest, and these render **raw** (no escape pipe) into:
   - `assets/skeleton-gitlab/.gitlab-ci.yml:57` — `repositories: '["{{ .Repo }}"]'`, a
     CI-executed file;
   - `assets/skeleton/.goreleaser.yaml:22-24` (ldflags), `.golangci.yaml:81-83`,
     `.mockery.yml:16` via `{{ .ModulePath }}`.
   A manifest `repo: x", "hostile/repo` (or any quote/newline-bearing value) passes
   `ValidateManifest` and is written into the regenerated project's CI configuration.

2. **Manifest flags are entirely unvalidated, and `default_is_code` emits verbatim Go.**
   `validateManifestCommands` (`validate.go:905-917`) checks only each command's `Name`.
   `ValidateFlagName`/`ValidateFlagType` (`validate.go:341,383`) run only on the
   `generate add-flag` CLI path. `getFlagDefaultValue`
   (`internal/generator/templates/command.go:658-665`) emits `jen.Id(flag.Default)` —
   the manifest string **verbatim as Go source** — whenever `default_is_code: true`.
   A tampered flag default compiles arbitrary Go into the regenerated tool.

3. **Signing provenance fields are skipped.** `validateManifestSigning`
   (`validate.go:921-935`) validates `Backend`, `KMSRegion`, `KeyID`, `PublicKey` but not
   `ExternalKeyEmail` or `KeySource`, which are written raw into the `// gtb:signing`
   annotation of the generated `pkg/cmd/root/provenance.go`
   (`internal/generator/provenance.go:35`, `signingFields` at `provenance.go:115-127`).
   A value containing a newline can break out of the comment line into the generated file.

4. **Boilerplate docs render descriptions with no escaping and no validation** (the
   review's closely-coupled MEDIUM — same threat model, same gate). The docs builder
   uses `fmt.Fprintf` on a `strings.Builder`, so the `templateFuncMap` escape pipes never
   apply: `internal/generator/docs.go:401` (description), `docs.go:412` (long
   description, written raw), `docs.go:436` (flags table row), `docs.go:452`
   (subcommands table row). `ManifestCommand.Description`, `LongDescription` and
   `ManifestFlag.Description` are never validated on the manifest path, and the CLI
   `--short`/`--long` inputs are not validated either. A `|` or newline breaks every
   generated flags table; a hostile description injects arbitrary Markdown/HTML into the
   docs site the downstream project publishes (potential stored XSS in a published
   artifact). This contradicts the CLAUDE.md/template-security claim that every
   user-influenced field is piped through an escape helper at non-code render sites.

## 2. Proposed change

Extend the existing two-layer defence (validators at the gate, escapes at the sink) —
per field, per sink:

1. **`validateManifestReleaseSource`** (`validate.go:995-1009`):
   - run `ValidateRepo` on `ReleaseSource.Repo` when populated;
   - restrict `ReleaseSource.Type` to the enum the skeleton assets actually support
     (`github` | `gitlab`, plus empty for default), rejecting anything else.
2. **`validateManifestCommands`** (`validate.go:905-917`) — walk each `ManifestCommand`'s
   flags and descriptions:
   - `ValidateFlagName(f.Name)` and `ValidateFlagType(f.Type)` for every `ManifestFlag`;
   - a new `ValidateFlagDefaultCode(f.Default)` applied when `DefaultIsCode` is true:
     NFC-normalised, restricted to a Go identifier/selector character class
     (`[A-Za-z_][A-Za-z0-9_]*` segments joined by `.`), bounded length — matching what
     `jen.Id` is legitimately used for (a named constant or package-qualified identifier);
   - `ValidateDescription` on `Description` and a length-bounded, control-character-free
     check on `LongDescription` (multi-line is legitimate there; `|` need not be banned
     because the sink escapes it — see 4);
   - apply the same at the CLI boundary: validate `--short`/`--long` on the generate path.
3. **`validateManifestSigning`** (`validate.go:921-935`):
   - `ValidateSigningExternalKeyEmail` — email-shaped character class (no whitespace, no
     control characters, single `@`), consistent with the provenance KV encoding;
   - `ValidateSigningKeySource` — enum check against the known key-source values.
4. **Docs builder escaping** (`docs.go:401,412,436,452`): route all four sites through
   `escapeMarkdown` plus a new `escapeMarkdownTableCell` helper in
   `internal/generator/template_escape.go` (escape `|`, collapse newlines to spaces) for
   the table rows. Sinks escape even when the gate validates — layer two is not optional.
5. **Fuzz coverage**: extend the existing validator/escape fuzz tests to the new
   validators and `escapeMarkdownTableCell`, consistent with the current suite.
6. **Docs**: update `docs/development/template-security.md` — add the manifest path to
   the field→validator→sink table and document the new helpers.

No public `pkg/` API changes; everything lives in `internal/generator`.

## 3. Scope & release plan

- GTB-repo only (`internal/generator`); ships as a `fix(generator)` (patch) — it hardens
  the existing gate to meet its documented contract, no behaviour change for valid input.
- After implementation, run the scaffold-verification step:
  `just build && go run ./cmd/gtb generate project ... -p tmp`, inspect `tmp/`
  (CI files, generated command Go, docs tables, provenance annotation), delete it. Also
  exercise `gtb regenerate project` against a manifest containing the newly validated
  fields.
- No new CLI surface, so no new Gherkin scenarios are required; the existing generator
  E2E suite must stay green (run with `-count=1` — Godog results are go-test-cached).
- `/gtb-verify` (tests, race, lint, mocks) before the MR.

## 4. Acceptance criteria

- A manifest whose `release_source.repo` contains `"`, `[`, `]`, whitespace, or path
  separators beyond the allowed segment shape is rejected by `ValidateManifest` with a
  field-named error, before any file write.
- A manifest flag with `default_is_code: true` and a default containing anything outside
  the identifier/selector class (spaces, `(`, `;`, newlines) is rejected; a legitimate
  qualified identifier (`time.Second`, `defaultTimeout`) still passes and renders as before.
- `ExternalKeyEmail`/`KeySource` values containing whitespace, newlines or out-of-enum
  values are rejected; valid values round-trip through the provenance file unchanged.
- A command/flag description containing `|` or a newline renders as a well-formed
  Markdown table (escaped pipe, collapsed newline) in generated docs; raw HTML in a
  description arrives entity-escaped per `escapeMarkdown` semantics.
- New validators and `escapeMarkdownTableCell` have unit + fuzz tests; generator package
  coverage does not regress.
- A byte-comparison of scaffold output for a fully-valid manifest is unchanged versus
  the current generator (the hardening is rejection/escaping only, not re-rendering).

## 5. Open questions

1. `ValidateFlagDefaultCode`: is the identifier/selector grammar sufficient for existing
   downstream manifests, or do any legitimately use expressions
   (e.g. `5 * time.Second`)? If expressions must be admitted, the alternative is parsing
   with `go/parser.ParseExpr` and rejecting on parse error plus a denylist of call/composite
   nodes — decide before implementation.
2. `ReleaseSource.Type` enum: lock to `github|gitlab` now, or reserve `gitea|bitbucket`
   for the forge adapters even though no skeleton asset set exists for them yet?
3. Should `{{ .Repo }}`/`{{ .ModulePath }}` additionally be piped through `escapeYAML` at
   the YAML sinks as belt-and-braces, accepting the (quoted-scalar) formatting change to
   generated files, or is gate-side validation sufficient given the strict segment rules?
