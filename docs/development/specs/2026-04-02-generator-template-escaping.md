---
title: "Generator Template Escaping for Non-Code Locations"
description: "Escape user-provided template data in non-code output locations to prevent template injection, while preserving unescaped output in generated Go code."
date: 2026-04-02
status: APPROVED
tags:
  - specification
  - generator
  - security
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Generator Template Escaping for Non-Code Locations

Authors
:   Matt Cockayne

Date
:   02 April 2026

Status
:   APPROVED

---

## Overview

A security audit identified that `internal/generator/skeleton.go` uses `text/template` to render scaffolded project files with user-provided data. Because `text/template` performs no automatic escaping, a malicious or accidental input value (e.g. a Slack channel name containing `{{ .` or backtick sequences) could cause template injection, producing corrupted output, information disclosure, or build failures in the generated project.

Template data **must NOT** be escaped where it appears in generated Go code (identifiers, types, import paths, struct field values) because escaping would cause compilation errors. The scope of this spec is strictly limited to **non-code locations**: string values in YAML/TOML configuration files, Markdown documentation, comments, and similar textual output.

### Threat Model

| Threat | Vector | Impact |
|--------|--------|--------|
| YAML injection | User input containing YAML special characters (`:`, `#`, newlines, flow-sequence characters) | Malformed YAML, structural override (injected keys), config-parse errors in scaffolded tool |
| TOML injection | User input containing `"`, `\`, or newlines in TOML string values | Malformed TOML, structural override, parse errors |
| Markdown injection | User input containing Markdown syntax (headings, links, fenced code) | Misleading documentation, social-engineering links in generated docs |
| CODEOWNERS syntax injection | User input containing newlines or characters invalid in usernames | Invalid rules, unintended owners assigned, silent drops by GitHub/GitLab |
| Unicode homoglyph/zero-width characters in `Name`, `Org` | Display-spoofed tool name (e.g. `ρaypal`, `admin` with hidden ZWJ) | Trust confusion; downstream search/audit impacted |
| Path traversal via `Name` or `ModulePath` | `..` sequences, absolute paths, backslashes | File write outside target directory (via path components in templates) |
| Go compilation breakage | Code-location templates must NOT escape — but invalid `Name`/`ModulePath` values still break `go build` | Generated project fails to compile |
| Template directive abuse (`{{`) in user input | Raw `{{ .SensitiveField }}` in `Name` | **Not exploitable** — `text/template` parses the template string (a trusted constant), not user data. User data is interpolated as literal text. Mentioned only to dispel the misconception. |

### Defence Strategy: Two Layers

Defence-in-depth requires **both** input validation and output escaping:

1. **Input validation (Phase 1)** — rejects values that are structurally dangerous (`..`, control characters, invalid Go identifiers, invalid hostnames). This is the primary line of defence: most injection vectors collapse if the input is constrained to a safe character class.
2. **Output escaping (Phase 2)** — context-aware escaping at each non-code template site, so that values passing validation (and future values whose constraints loosen) cannot produce malformed output.

Input validation alone is not sufficient (escaping constraints vary by output format; a valid `Name` in one context may still be problematic in another). Output escaping alone is not sufficient (escaping `..` in path components requires context; input validation is cleaner).

### Scope

**In scope:**

- All `text/template` rendered files in `internal/generator/assets/skeleton/`, `skeleton-github/`, and `skeleton-gitlab/`
- The two inline template strings: `SkeletonGoMod` and `SkeletonConfig`
- Identification of code vs non-code template locations
- Input validation rules for user-provided fields (`Name`, `Description`, `Repo`, `Host`, `Org`, Slack/Teams identifiers, `EnvPrefix`)
- Custom template function(s) for escaping non-code outputs
- Fuzz testing of escape functions

**Out of scope:**

- Jennifer-generated Go files (`skeleton_root.go`) — jennifer's `Lit()` function already handles Go string literal escaping. **Note**: `EnvPrefix`, `SlackChannel`, `SlackTeam`, `TeamsChannel`, `TeamsTeam`, `TelemetryEndpoint`, `TelemetryOTelEndpoint` currently flow **only** through jennifer (`jen.Lit`). If future changes route them through text templates, their escaping must be audited.
- Template rendering in non-generator contexts
- Post-generation validation of the scaffolded project (e.g. running `go build` as part of the generator)

## Design Decisions

**Custom template functions over `html/template`**: Using `html/template` is unsuitable because it escapes for HTML contexts, not YAML/Markdown/TOML contexts. Custom functions give precise control over escaping per output format.

**Opt-in escaping at call sites**: Rather than globally escaping all fields (which would break code locations), each non-code template location explicitly pipes through an escape function. This makes the escaping intent visible and auditable.

**Multiple escape functions by context**: Different output formats need different escaping rules. YAML values need quoting, Markdown needs backtick/link escaping, and TOML string values need quote escaping. A single `escape` function would be either too aggressive or too lax.

**No changes to jennifer-generated code**: The `skeleton_root.go` file uses `jen.Lit()` which already handles Go string escaping. No additional escaping is needed there.

**Validation rejects; escaping sanitises**: Validation errors abort generation with an actionable message. Escape functions are pure and infallible — they never panic, never error, and always produce valid output for any input. Rejection belongs at the entry point; sanitisation belongs at the rendering layer.

**NFC Unicode normalisation on string fields**: All user-provided string fields are normalised to Unicode NFC form before validation and template rendering. This prevents homoglyph attacks and normalises away combining characters. Fields that must be ASCII (e.g. `Name`, `Org`, `EnvPrefix`) fail validation if they contain non-ASCII code points after normalisation.

## Input Validation

Validation runs at the wizard/flag entry point in `internal/generator/skeleton.go` (via `SkeletonOptions.ValidateOrPrompt` and the equivalent manifest loading paths in `regenerate.go`). Validation errors abort generation with a clear message indicating which field failed and why.

### Field Validation Rules

| Field | Rule | Rationale |
|-------|------|-----------|
| `Name` | Must match `^[a-z][a-z0-9-]{0,63}$` | Must be a valid Go binary name, directory name, and appear as a simple identifier in shell commands. Lowercase-only avoids case-sensitivity confusion across filesystems. Length 64 is generous for a CLI name. |
| `Description` | Must be ≤ 500 bytes after NFC normalisation; no control characters except `\t`; no `{{` or `}}` sequences | Length bounds YAML/TOML value size; control character ban prevents YAML structural injection; template-brace ban is belt-and-braces (defence-in-depth; templates don't re-parse user data). |
| `Repo` | Must match Go module path rules: domain + path segments of `[a-zA-Z0-9._~-]`; no leading/trailing `/`; no `..` | `go mod tidy` will fail on invalid paths; `..` prevents traversal. |
| `Host` | Must be a valid RFC 1123 hostname (or `host:port`). Punycode is accepted; raw Unicode is not. | Prevents URL construction errors and homoglyph spoofing in documentation URLs. |
| `Org` | Must match `^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$` (GitHub rules); for GitLab, also allow `/`-separated subgroups up to 4 levels deep. | CODEOWNERS `@`-mentions require valid namespace syntax; invalid values are silently dropped by the VCS. |
| `EnvPrefix` | Must match `^[A-Z][A-Z0-9_]{0,31}$` if non-empty | Valid environment variable prefix; bounded length; no shell metacharacters. |
| `SlackChannel` | Must match `^[a-z0-9-]{1,80}$` if non-empty | Slack channel naming rules; prevents Markdown/link injection in channel references. |
| `SlackTeam` | Must match `^[a-zA-Z0-9][a-zA-Z0-9-]{0,20}$` if non-empty | Slack workspace-name rules. |
| `TeamsChannel` | Must be ≤ 100 bytes; no control characters; no `{{`/`}}` | Teams channels are less constrained than Slack; apply YAML-safety rules. |
| `TeamsTeam` | Same as `TeamsChannel`. | — |
| `TelemetryEndpoint` | Must parse as a valid URL with scheme `http` or `https`; no control characters; ≤ 2048 bytes | Config value; prevents injection into scaffolded config files. |
| `TelemetryOTelEndpoint` | Same as `TelemetryEndpoint`. | — |

All validation uses the canonical forms in `internal/generator/validate.go` (new file). Error messages are structured using `cockroachdb/errors` with `WithHint` so the wizard can display actionable guidance.

### Validation Timing

Validation runs:

1. After the interactive wizard collects each value (per-field, with immediate feedback).
2. When CLI flags are parsed (before any file is written).
3. When a `generator.toml` manifest is loaded for `regenerate`.

Validation is **never** deferred to template rendering time.

## Public API

This feature is entirely internal to `internal/generator/`. No `pkg/` API changes are required. No API stability implications.

## Internal Implementation

### User-Provided Data Fields

The following fields originate from user input (interactive wizard or CLI flags) and flow through template rendering:

| Field | Source | Used In |
|-------|--------|---------|
| `Name` | `--name` flag / wizard | Markdown, YAML, TOML, justfile, `.goreleaser.yaml`, CHANGELOG.md, go.mod, CI workflows |
| `Description` | `--description` flag / wizard | Markdown (`docs/index.md`), TOML (`zensical.toml`) |
| `Repo` | `--repo` flag / wizard | Markdown (`README.md`), go.mod, TOML (`zensical.toml`) |
| `Host` | `--host` flag / wizard | TOML (`zensical.toml`), `.goreleaser.yaml` |
| `Org` | Derived from `Repo` | CODEOWNERS, TOML (`zensical.toml`) |
| `RepoName` | Derived from `Repo` | (currently only in jennifer code) |
| `ModulePath` | Derived from `Host` + `Repo` | go.mod, `.golangci.yaml`, `.mockery.yml` |
| `SlackChannel` | `--slack-channel` flag / wizard | (currently only in jennifer code) |
| `SlackTeam` | `--slack-team` flag / wizard | (currently only in jennifer code) |
| `TeamsChannel` | `--teams-channel` flag / wizard | (currently only in jennifer code) |
| `TeamsTeam` | `--teams-team` flag / wizard | (currently only in jennifer code) |
| `TelemetryEndpoint` | Manifest telemetry config | (currently only in jennifer code) |
| `TelemetryOTelEndpoint` | Manifest telemetry config | (currently only in jennifer code) |
| `GoVersion` | Autodetected / `--go-version` | go.mod |
| `ReleaseProvider` | Derived from `Host` | `.goreleaser.yaml` |
| `GoToolBaseVersion` | Runtime version | (not user-provided; low risk) |

### Template Location Audit

#### Non-Code Locations (Require Escaping)

| File | Field(s) | Context | Risk |
|------|----------|---------|------|
| `README.md` | `Name`, `Repo` | Markdown headings, prose, code blocks | Markdown injection |
| `docs/index.md` | `Name`, `Description` | Markdown headings, prose, code blocks | Markdown injection |
| `CHANGELOG.md` | `Name` | Markdown code blocks | Markdown injection |
| `zensical.toml` | `Name`, `Description`, `Org`, `Host`, `Repo` | TOML string values | TOML injection (unquoted strings, special chars) |
| `.goreleaser.yaml` | `Name`, `ModulePath`, `Host`, `ReleaseProvider` | YAML values, path components | YAML injection |
| `.golangci.yaml` | `ModulePath` | YAML string values (quoted) | YAML injection (already quoted, low risk) |
| `.mockery.yml` | `ModulePath` | YAML string/key values | YAML injection |
| `justfile` | `Name` | Comments, command arguments, paths | Shell injection in comments |
| `.gitignore` | (none) | No user data | No risk |
| `.pre-commit-config.yaml` | (none) | No user data | No risk |
| `.releaserc` (both providers) | (none) | No user data | No risk |
| `.github/renovate.json5` | (none) | No user data | No risk |
| `.github/CODEOWNERS` | `Org` | GitHub CODEOWNERS syntax | Minor (org name in `@` mention) |
| `.gitlab/CODEOWNERS` | `Org` | GitLab CODEOWNERS syntax | Minor (org name in `@` mention) |
| CI workflow files | (none) | No user data (use `{{ "{{" }}` escapes for GitHub Actions expressions) | No risk |
| `go.mod` (inline template) | `ModulePath`, `GoVersion` | Go module path, version constraint | Low risk (validated by `go mod tidy`) |
| `config.yaml` (inline template) | (none) | No user data | No risk |

#### Code Locations (Must NOT Be Escaped)

| File | Field(s) | Context | Reason |
|------|----------|---------|--------|
| `.goreleaser.yaml` | `Name` in `builds[].main`, `ModulePath` in `ldflags` | File paths, Go linker flags | Escaping would break build |
| `justfile` | `Name` in `go build -o bin/{{ .Name }}`, `go install` | Shell commands, binary paths | Escaping would break commands |
| `.mockery.yml` | `ModulePath` in `packages:` key | Go import path (YAML key) | Escaping would break mockery |
| `go.mod` | `ModulePath` | Go module declaration | Escaping would break `go mod tidy` |
| `.golangci.yaml` | `ModulePath` in `local-prefixes`, `module-path` | Go import prefix | Escaping would break linter config |

### Escaping Strategy

#### Dual-Use Fields

Several fields (`Name`, `ModulePath`) appear in both code and non-code contexts within the same file. For these cases, the template must use the raw field in code locations and the escaped variant in non-code locations.

The recommended approach is to register custom template functions and use piped escaping only at non-code call sites:

```go
funcMap := template.FuncMap{
    "escapeYAML":     escapeYAML,
    "escapeMarkdown": escapeMarkdown,
    "escapeTOML":     escapeTOML,
}
```

#### Escape Functions

All escape functions are pure and infallible. They accept any string and return a string that is safe to interpolate into the named context. Invalid bytes (e.g. invalid UTF-8) are replaced with the Unicode replacement character (`\uFFFD`) rather than returned verbatim.

**`escapeYAML(s string) string`** — For YAML string values in block scalar context:
- Always wrap the result in double quotes. Unlike the original "quote if needed" approach, unconditional quoting is simpler, auditable, and robust across YAML 1.1 and 1.2 parsers (both resolve `yes`/`no` as booleans under certain tags, and implicit typing rules differ).
- Inside the quoted string: escape `\` as `\\`, `"` as `\"`, and control characters (`0x00`–`0x1F`, `0x7F`) as `\xHH` escapes.
- Strip NUL bytes (they cannot appear in YAML even when escaped, per the YAML 1.2 character-set production).

**`escapeMarkdown(s string) string`** — For Markdown prose contexts (NOT code blocks or headings):
- Escape CommonMark punctuation that initiates a construct: `\`, `` ` ``, `*`, `_`, `[`, `]`, `<`, `>`, `|`, `{`, `}`, `!`, `#`.
- Do NOT escape `.`, `-`, `+` universally — these only have meaning at line-start or in specific positions, and universal escaping corrupts ordinary prose (e.g. `v1.0.0` becomes `v1\.0\.0`). The template writer is responsible for placing `escapeMarkdown` only in inline-prose contexts.
- Replace `\r\n` and `\r` with `\n`; then each `\n` is preserved as-is (Markdown line breaks are benign in prose).

**`escapeMarkdownCodeBlock(s string) string`** — For Markdown inside a fenced code block:
- Code blocks render content verbatim but an input containing ``` ``` ``` would close the block. Replace runs of 3+ backticks with `\`` sequences.
- Strip NUL bytes and normalise line endings.

**`escapeTOML(s string) string`** — For TOML basic string values (`"..."`):
- Escape `\`, `"`, and control characters using TOML escape sequences (`\\`, `\"`, `\b`, `\t`, `\n`, `\f`, `\r`, `\uHHHH`).
- Per TOML spec, DEL (`0x7F`) and other control characters must be escaped.

**`escapeComment(s string) string`** — For single-line comments (justfile `#`, YAML `#`, CODEOWNERS `#`):
- Replace all newline sequences with a single space. A newline in a comment allows the next line to escape comment context and inject content.
- Replace NUL bytes with space.

**`escapeShellArg(s string) string`** — For shell-like contexts where values are not in quoted arguments (justfile recipe bodies):
- `exec.Command` does not invoke a shell, but `just` recipes execute under `sh` by default. Any user input interpolated into a recipe body could reach the shell.
- Wrap the value in single quotes; escape existing single quotes as `'\''`.
- Only used in the justfile template; currently no user field reaches a shell command body (all `Name` uses are inside binary paths which validation already constrains to `[a-z0-9-]`).

**Identity guarantee**: For any string matching `^[a-zA-Z0-9 _.,/-]*$` (the "safe" character class), every escape function returns the input unchanged. Existing generated projects with clean inputs will see no diff after this change.

#### Template Changes

Example changes to skeleton templates:

```
# README.md -- Markdown context
# {{ .Name | escapeMarkdown }}

{{ .Name | escapeMarkdown }} is a tool built with [gtb](...).
```

```
# zensical.toml -- TOML string values
site_name = "{{ .Name | escapeTOML }}"
site_description = "{{ .Description | escapeTOML }}"
site_author = "{{ .Org | escapeTOML }}"
```

```
# .goreleaser.yaml -- mixed contexts
# YAML value (non-code): project_name is a display name
project_name: {{ .Name | escapeYAML }}
# Code path (must not escape): build main path
    main: cmd/{{ .Name }}/main.go
```

```
# justfile -- mixed contexts
# Build the {{ .Name | escapeComment }} binary   <-- comment (non-code)
build: tidy generate
    go build -o bin/{{ .Name }} ./cmd/{{ .Name }}  <-- code path (no escape)
```

### Integration with Template Rendering

The `renderAndHashSkeletonTemplate` function in `skeleton.go` currently creates a bare `text/template`. It must be updated to register the function map:

```go
tmpl, err := template.New(fullPath).Funcs(templateFuncMap).Parse(tmplStr)
```

The `templateFuncMap` is a package-level variable containing all escape functions, defined in a new file `internal/generator/template_escape.go`.

The inline template strings (`SkeletonGoMod`, `SkeletonConfig`) must also have the function map available. Since `SkeletonConfig` currently contains no user data, only `SkeletonGoMod` needs consideration -- and its fields (`ModulePath`, `GoVersion`) appear in code-context locations, so no escaping is applied there.

## Project Structure

| File | Action |
|------|--------|
| `internal/generator/validate.go` | **Create** — field validators (`ValidateName`, `ValidateDescription`, `ValidateRepo`, `ValidateHost`, `ValidateOrg`, `ValidateEnvPrefix`, etc.) |
| `internal/generator/validate_test.go` | **Create** — unit tests including adversarial Unicode cases |
| `internal/generator/template_escape.go` | **Create** — escape functions and `templateFuncMap` |
| `internal/generator/template_escape_test.go` | **Create** — unit tests for each escape function |
| `internal/generator/template_escape_fuzz_test.go` | **Create** — fuzz tests for each escape function |
| `internal/generator/skeleton.go` | Modify — wire validators into `ValidateOrPrompt`; register `templateFuncMap` in `renderAndHashSkeletonTemplate`; apply `templateFuncMap` to inline template parsers (`SkeletonGoMod`, `SkeletonConfig`) |
| `internal/generator/regenerate.go` | Modify — apply validators when loading manifests for `regenerate` |
| `internal/generator/manifest.go` | Modify — wire validators into manifest load path |
| `internal/generator/assets/skeleton/README.md` | Modify — pipe `Name` through `escapeMarkdown` in prose |
| `internal/generator/assets/skeleton/docs/index.md` | Modify — pipe `Name`, `Description` through `escapeMarkdown` |
| `internal/generator/assets/skeleton/CHANGELOG.md` | Modify — `Name` in prose uses `escapeMarkdown`; `Name` in fenced code blocks uses `escapeMarkdownCodeBlock` |
| `internal/generator/assets/skeleton/zensical.toml` | Modify — pipe all string values (`Name`, `Description`, `Org`, `Host`, `Repo`) through `escapeTOML` |
| `internal/generator/assets/skeleton/.goreleaser.yaml` | Modify — pipe `Name` through `escapeYAML` for `project_name`; leave path-context fields (`cmd/{{ .Name }}/main.go`, `ldflags`) unescaped since validation guarantees safe characters |
| `internal/generator/assets/skeleton/justfile` | Modify — pipe `Name` through `escapeComment` in comments |
| `internal/generator/assets/skeleton-github/.github/CODEOWNERS` | Modify — `Org` is validated (no escape needed, but audit shows `@{{ .Org }}` is the expected form) |
| `internal/generator/assets/skeleton-gitlab/.gitlab/CODEOWNERS` | Modify — same as GitHub CODEOWNERS |

## Generator Impact

This change modifies generator template output. After implementation:

1. Existing projects regenerated with `gtb regenerate` will see minor diffs in non-code locations (added escaping). These are cosmetic for clean inputs and protective for adversarial inputs.
2. The conflict detection system (hash-based) will detect these as generator-side changes and prompt appropriately.
3. No changes to the manifest format or feature flags.

## Error Handling

- Escape functions must be pure and infallible (no error returns). Invalid input is sanitised, never rejected, at the template layer.
- Input validation (rejecting clearly invalid names, channels, etc.) is a separate concern to be addressed in a follow-up spec. This spec focuses solely on defence-in-depth at the template rendering layer.

## Testing Strategy

### Unit Tests — Validation (`internal/generator/validate_test.go`)

Table-driven tests for each field validator:

| Validator | Accepting Cases | Rejecting Cases |
|-----------|-----------------|-----------------|
| `ValidateName` | `mytool`, `my-tool`, `a`, 64-char lowercase-alnum-hyphen | Empty, `MyTool` (uppercase), `my_tool` (underscore), `123tool` (leading digit), 65+ chars, `../tool` (traversal), `tool\n` (newline), `tool\x00` (NUL), Unicode homoglyphs |
| `ValidateDescription` | Short/long prose up to 500 bytes, unicode text, punctuation | Control characters (except `\t`), `{{`/`}}` sequences, >500 bytes |
| `ValidateRepo` | `github.com/org/repo`, `gitlab.com/group/sub/repo` | Empty, `../repo`, `/repo`, `repo/`, trailing dot, scheme prefix |
| `ValidateHost` | `github.com`, `gitlab.example.com`, `localhost:8080`, punycode host | Empty, Unicode homoglyph host, `host:`, space-containing host, control chars |
| `ValidateOrg` | `myorg`, `MyOrg`, `my-org`, 39-char max | Empty, leading hyphen, trailing hyphen, 40+ chars, special chars, Unicode |
| `ValidateEnvPrefix` | `GTB`, `MY_TOOL`, empty (optional) | Lowercase, leading digit, over 32 chars, non-ASCII |

Adversarial input tests include: NUL bytes, CR/LF injection, right-to-left override (`\u202E`), zero-width joiner (`\u200D`), BOM (`\uFEFF`), combining diacritics, and CJK homoglyphs.

### Unit Tests — Escape Functions (`internal/generator/template_escape_test.go`)

Table-driven tests for each escape function:

| Function | Test Cases |
|----------|------------|
| `escapeYAML` | Plain string, string with `:`, `#`, `{{`, boolean-like values (`yes`, `true`), numeric-like values (`1.0`), YAML flow characters (`[`, `{`, `,`), control characters, NUL bytes, empty string, Unicode text |
| `escapeMarkdown` | Plain string, `*`, `_`, `` ` ``, `[`, `]`, `<`, `>`, `|`, `{{`, heading-like (`# malicious`), empty string, legitimate punctuation (`v1.0.0` unchanged) |
| `escapeMarkdownCodeBlock` | Plain code, code with `` ``` ``, code with NUL bytes, Unicode |
| `escapeTOML` | Plain string, `"`, `\`, control characters, newlines, DEL, BOM, empty string |
| `escapeComment` | Plain string, multiline strings (CR, LF, CRLF), NUL bytes, `{{`, empty string |
| `escapeShellArg` | Plain string, string with `'`, `$`, `` ` ``, `;`, `&&`, empty string |

### Fuzz Tests (`template_escape_fuzz_test.go`)

Required for Phase 1. For each escape function:

```go
func FuzzEscapeYAML(f *testing.F) {
    f.Add("plain")
    f.Add("key: value")
    f.Add("line1\nline2")
    f.Fuzz(func(t *testing.T, s string) {
        out := escapeYAML(s)
        // Property 1: output must parse as a valid YAML scalar
        var parsed string
        if err := yaml.Unmarshal([]byte(`v: `+out), &struct{ V *string }{V: &parsed}); err != nil {
            t.Errorf("escapeYAML produced unparseable output for input %q: %v", s, err)
        }
        // Property 2: never panics (implicit — Fuzz would catch a panic)
        // Property 3: the parsed value must round-trip to something lossy-equivalent
        //            to the input (e.g. NUL bytes stripped is acceptable)
    })
}
```

Equivalent fuzz tests exist for `escapeTOML` (property: output parses as TOML scalar), `escapeMarkdown` (property: does not produce unbalanced link/code constructs), and `escapeShellArg` (property: surviving shell tokenisation yields exactly one argument equal to or lossy-equivalent to input).

Fuzz corpus is seeded with known-problematic inputs from the unit test tables.

### Integration Tests

- Render each modified skeleton template with adversarial input data (where input validation would reject them, the test uses a lower-level API that bypasses validation) and verify the rendered output is syntactically valid (YAML parses, TOML parses, Markdown renders without injection).
- Verify code-location outputs remain unescaped and functional by running `go build`, `yamllint`, and `tomli` (or `github.com/pelletier/go-toml` parser) on the rendered files.
- Verify that `Name = "../../etc/passwd"` or similar is **rejected at input validation** and never reaches the template layer. This is the primary integration assertion.

### Regression Tests

- Generate a full skeleton with normal input values and verify the output is byte-identical to pre-change output. Escape functions are identity for the safe character class, so output should not change for any existing test fixture.
- Hash-verify every rendered file against stored golden hashes. If a golden hash changes, CI fails and the reviewer must confirm the change is expected.

### Regeneration Compatibility

The generator's hash-based conflict detection (see `internal/generator/manifest.go`) tracks per-file hashes. Adding escape functions changes output for adversarial inputs but not for clean inputs. Regression tests confirm no hash drift for all existing fixtures.

## Migration & Compatibility

- **Backward compatible**: Clean input values produce identical output (escape functions are identity for safe strings).
- **No API changes**: Entirely internal to `internal/generator/`.
- **No manifest changes**: File hashes will differ only if templates produce different output for the same input, which only happens for inputs containing special characters.
- **Regeneration safe**: Existing projects with clean inputs will see no hash mismatches after this change.
- **Validation may reject previously-accepted inputs**: Users whose tool `Name`, `Org`, `Host`, etc. contain now-disallowed characters will see a validation error on regeneration. The error message surfaces the exact rule and the offending input; the migration guide documents the rules and offers workarounds (e.g. punycode for Unicode hosts).

---

## Non-Functional Requirements

### Testing & Quality Gates

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 90 % for `internal/generator/validate.go` and `template_escape.go` |
| Branch coverage | ≥ 85 % across the two new files; 100 % for the escape functions themselves |
| Race detector | All new tests must pass under `go test -race` |
| Fuzz testing | **Required**. One fuzz test per escape function and per validator; each must run ≥ 60 s in CI and maintain a committed corpus |
| Property tests | Each escape function satisfies: (a) identity on the safe character class, (b) output parses as the target format, (c) idempotent — `f(f(x)) == f(x)` |
| Golangci-lint | No new findings; no `//nolint` directives |
| Regression fixtures | Every existing generator golden test must pass byte-identical; a new golden set covers adversarial inputs |
| Generator end-to-end | `just build && go run ./cmd/gtb generate <tool> -p tmp` on a CI-generated tool compiles with `go build`, lints with `golangci-lint`, and produces valid YAML/TOML parseable by downstream tools |
| BDD scenarios | **Required**. At least one Gherkin scenario under `features/generator/validation.feature` covering: (a) tool generation rejects invalid `Name`, (b) valid inputs produce valid output, (c) regeneration preserves validation |

### Documentation Deliverables

The following artefacts must be produced or updated before the implementing PR is merged:

| Artefact | Scope |
|----------|-------|
| `docs/how-to/generate-tool.md` | Update. Add a "Field rules and validation" subsection with every validation rule, the rationale, and examples of accepted/rejected values. |
| `docs/components/generator.md` | Update. Document the `templateFuncMap`, each escape function's contract (inputs, outputs, invariants), and when to use which. |
| `docs/development/template-security.md` | New. Threat model, rationale for defence-in-depth, guidance for contributors adding new user-input fields to templates. |
| `docs/migration/<version>-generator-validation.md` | New. Describe validation rules, breaking-case examples, and remediation for users whose existing inputs are no longer accepted. |
| Template data struct docstrings | Update `internal/generator/skeleton.go`: every field used in templates must have a comment noting its validation rule and which escape functions are required at which template sites. |
| BDD feature files | New (`features/generator/validation.feature`). Living documentation for the validation rules. |
| CLAUDE.md | Update. Add a one-line reference under "Architecture / Code Generation" pointing at the validation rules and the template-security doc. |

### Observability

| Event | Level | Fields |
|-------|-------|--------|
| Validation failure (interactive wizard) | Prompt re-shown to user; never logged | Field name, rule that failed; never the offending value at any level above DEBUG |
| Validation failure (non-interactive, e.g. manifest load) | ERROR (fatal) | Field name, rule, position in input; value truncated to first 32 chars and quoted to make the failure actionable without leaking arbitrary content |
| Template render error (post-validation) | ERROR | Template path, field being rendered; never the value |
| Escape function invoked on non-UTF-8 input | Not logged | The function returns `\uFFFD` silently; no noise in the log stream |

### Performance Bounds

| Metric | Bound | Notes |
|--------|-------|-------|
| Validation | O(n) per field, linear in input length | No regex backtracking; all patterns are anchored and use bounded quantifiers |
| Escape functions | O(n) per input | No regex inside escape functions; pure byte/rune scan |
| Fuzz iterations | ≥ 100 000 executions / function in CI over the committed corpus | — |
| Wall-clock: full skeleton generation | ≤ 500 ms added total for validation + escaping vs pre-change baseline | — |
| Memory | O(n) per escape (return value); no buffered input beyond the rendered output | — |

### Security Invariants

Summarised from the [Threat Model](#threat-model) and [Resolved Decisions](#resolved-decisions):

1. Every user-provided field has an explicit validation rule. Accepting any new user field requires updating `internal/generator/validate.go` in the same PR.
2. Escape functions are identity on the safe character class and idempotent on any input.
3. NFC normalisation happens **before** validation so homoglyphs fail fast.
4. Code-context template sites never receive escaped input; non-code sites always do (defence-in-depth even when validation covers the character class).
5. Jennifer-generated Go (`skeleton_root.go`) handles its own literal escaping via `jen.Lit()`. Any new user field routed through jennifer must be validated even if no escape function applies.
6. Validation errors never echo the offending value at log levels above DEBUG.

---

## Future Considerations

1. **Input validation at the wizard/flag layer**: A complementary spec could add validation rules to `SkeletonOptions.ValidateOrPrompt()` that reject or warn about inputs containing template-sensitive characters. This provides defence at the entry point rather than just the rendering layer.
2. **Fuzz testing**: The escape functions are good candidates for Go fuzz testing to discover edge cases in escaping logic.
3. **Centralised template rendering**: If the generator gains more template-rendered outputs in the future, the function map registration should be centralised (this spec already proposes a package-level `templateFuncMap`).
4. **Content Security Policy for Markdown**: If generated documentation is ever served in a web context, additional XSS protections may be needed.

## Implementation Phases

### Phase 1: Input Validation and Escape Functions

Validation is the primary defence; it ships first so no further changes can introduce adversarial inputs to the rendering layer.

1. Create `internal/generator/validate.go` with field validators (see Input Validation section).
2. Create `internal/generator/validate_test.go` with comprehensive unit tests (including adversarial Unicode cases).
3. Wire validators into `SkeletonOptions.ValidateOrPrompt` and the manifest-loading paths in `regenerate.go`.
4. Create `internal/generator/template_escape.go` with `escapeYAML`, `escapeMarkdown`, `escapeMarkdownCodeBlock`, `escapeTOML`, `escapeComment`, `escapeShellArg`, and `templateFuncMap`.
5. Create `internal/generator/template_escape_test.go` with comprehensive unit tests.
6. Create `internal/generator/template_escape_fuzz_test.go` with fuzz tests for each escape function.
7. Modify `skeleton.go` to register `templateFuncMap` in `renderAndHashSkeletonTemplate` — and in the inline template strings (`SkeletonGoMod`, `SkeletonConfig`) for consistency, even though they currently use only code-context fields.
8. Run existing test suite; no functional change to rendered output expected.

### Phase 2: Apply Escaping to Skeleton Templates

Now that validation rejects most adversarial inputs, escaping at non-code sites provides defence-in-depth.

1. Update each skeleton template file to use escape functions at non-code call sites (see Project Structure table).
2. Add integration tests rendering templates with adversarial input (bypassing validation at a lower-level API) and asserting valid YAML/TOML/Markdown.
3. Add regression tests confirming clean-input output is byte-identical to pre-change output.
4. Add golden-hash regression on every skeleton file.

### Phase 3: Continuous Improvement

Tracked as follow-ups rather than required work:

1. Add a `--no-validate` flag (or environment variable) for CI/automation paths that trust their inputs. Default remains strict.
2. Centralise validation rules if other parts of the codebase (AI wizard, config init) collect similar fields.
3. Consider CLDR-based Unicode validation for fields like `Description` that allow Unicode but must still be safe for display in terminals and web UIs.

## Resolved Decisions

1. **`Name` is restricted to `^[a-z][a-z0-9-]{0,63}$` at input time.** This eliminates path traversal, Unicode spoofing, shell/YAML/TOML/Markdown injection, and invalid-Go-identifier issues in a single check. Most other escaping concerns for `Name` become moot. The restriction is tighter than Go identifier rules (no underscores, no uppercase) to enforce consistent naming across binaries, config paths, and URLs.

2. **`Host` is validated as an RFC 1123 hostname (optionally with `:port`) at input time.** Punycode (`xn--...`) is accepted so internationalised hosts are supported. Raw Unicode hosts are rejected — the tool author must convert to punycode explicitly, preventing homoglyph attacks. In templates, `Host` is treated as trusted (no escape pipe needed) because validation has already constrained the character class.

3. **`Org` is validated against GitHub org rules** (`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`) for `github` release provider, and GitLab namespace rules (`[a-zA-Z0-9][a-zA-Z0-9/-]{0,254}` with `/`-segment depth ≤ 4) for `gitlab`. CODEOWNERS injection is prevented by validation alone; no runtime escape is applied to `Org`.

4. **Escaping is universal at non-code sites even for validated fields.** Validation eliminates known injection vectors, but escape functions are cheap (identity on safe input) and protect against future field additions or relaxed validation. Defence-in-depth is preferred over trusting a single layer.

5. **`EnvPrefix`, Slack/Teams, and telemetry-endpoint fields are validated and flow only through jennifer.** They are not currently used in text templates, but this spec adds validation so that if future changes add them to text templates, the entry point is already safe.
