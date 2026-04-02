---
title: "Generator Template Escaping for Non-Code Locations"
description: "Escape user-provided template data in non-code output locations to prevent template injection, while preserving unescaped output in generated Go code."
date: 2026-04-02
status: DRAFT
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
:   DRAFT

---

## Overview

A security audit identified that `internal/generator/skeleton.go` uses `text/template` to render scaffolded project files with user-provided data. Because `text/template` performs no automatic escaping, a malicious or accidental input value (e.g. a Slack channel name containing `{{ .` or backtick sequences) could cause template injection, producing corrupted output, information disclosure, or build failures in the generated project.

Template data **must NOT** be escaped where it appears in generated Go code (identifiers, types, import paths, struct field values) because escaping would cause compilation errors. The scope of this spec is strictly limited to **non-code locations**: string values in YAML/TOML configuration files, Markdown documentation, comments, and similar textual output.

### Threat Model

| Threat | Vector | Impact |
|--------|--------|--------|
| Template injection via `text/template` directives | User input containing `{{` sequences | Arbitrary template execution, corrupted output |
| YAML injection | User input containing YAML special characters (`:`, `#`, newlines) | Malformed configuration files, potential config override |
| Markdown injection | User input containing Markdown syntax | Corrupted documentation, misleading content |
| Go string literal injection | User input containing `"` or backticks in jennifer `Lit()` calls | Compilation errors (mitigated by jennifer's own escaping) |

### Scope

**In scope:**

- All `text/template` rendered files in `internal/generator/assets/skeleton/`, `skeleton-github/`, and `skeleton-gitlab/`
- The two inline template strings: `SkeletonGoMod` and `SkeletonConfig`
- Identification of code vs non-code template locations
- Custom template function(s) for escaping non-code outputs

**Out of scope:**

- Jennifer-generated Go files (`skeleton_root.go`) -- jennifer's `Lit()` function already handles Go string literal escaping
- Template rendering in non-generator contexts
- Input validation (separate concern; may be addressed in a follow-up spec)

## Design Decisions

**Custom template functions over `html/template`**: Using `html/template` is unsuitable because it escapes for HTML contexts, not YAML/Markdown/TOML contexts. Custom functions give precise control over escaping per output format.

**Opt-in escaping at call sites**: Rather than globally escaping all fields (which would break code locations), each non-code template location explicitly pipes through an escape function. This makes the escaping intent visible and auditable.

**Multiple escape functions by context**: Different output formats need different escaping rules. YAML values need quoting, Markdown needs backtick/link escaping, and TOML string values need quote escaping. A single `escape` function would be either too aggressive or too lax.

**No changes to jennifer-generated code**: The `skeleton_root.go` file uses `jen.Lit()` which already handles Go string escaping. No additional escaping is needed there.

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

**`escapeYAML(s string) string`** -- For YAML string values:
- If the string contains any of `: # { } [ ] , & * ? | - < > = ! % @ \` ` or leading/trailing whitespace, wrap in single quotes
- Within single-quoted YAML strings, escape `'` as `''`
- Strings that look like YAML booleans (`true`, `false`, `yes`, `no`) or numbers must also be quoted

**`escapeMarkdown(s string) string`** -- For Markdown prose contexts:
- Escape characters with special Markdown meaning: `\`, `` ` ``, `*`, `_`, `{`, `}`, `[`, `]`, `(`, `)`, `#`, `+`, `-`, `.`, `!`, `|`
- Prefix each with `\`
- This is NOT used inside fenced code blocks (where the content is already literal)

**`escapeTOML(s string) string`** -- For TOML string values:
- The values are already inside double-quoted strings in the template
- Escape `\`, `"`, and control characters using TOML escape sequences (`\\`, `\"`, `\n`, etc.)

**`escapeComment(s string) string`** -- For comments (justfile, CODEOWNERS):
- Strip or escape newlines to prevent multi-line injection
- For CODEOWNERS: strip characters not valid in GitHub/GitLab usernames

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
| `internal/generator/template_escape.go` | Create -- escape functions and `templateFuncMap` |
| `internal/generator/template_escape_test.go` | Create -- unit tests for each escape function |
| `internal/generator/skeleton.go` | Modify -- register `templateFuncMap` in `renderAndHashSkeletonTemplate` |
| `internal/generator/assets/skeleton/README.md` | Modify -- pipe `Name`, `Repo` through `escapeMarkdown` in prose |
| `internal/generator/assets/skeleton/docs/index.md` | Modify -- pipe `Name`, `Description` through `escapeMarkdown` |
| `internal/generator/assets/skeleton/CHANGELOG.md` | Modify -- pipe `Name` through `escapeMarkdown` in prose (not code blocks) |
| `internal/generator/assets/skeleton/zensical.toml` | Modify -- pipe string values through `escapeTOML` |
| `internal/generator/assets/skeleton/.goreleaser.yaml` | Modify -- pipe `Name` through `escapeYAML` for `project_name`; `Host` through `escapeYAML` for URL values |
| `internal/generator/assets/skeleton/justfile` | Modify -- pipe `Name` through `escapeComment` in comments only |
| `internal/generator/assets/skeleton-github/.github/CODEOWNERS` | Modify -- pipe `Org` through `escapeComment` |
| `internal/generator/assets/skeleton-gitlab/.gitlab/CODEOWNERS` | Modify -- pipe `Org` through `escapeComment` |

## Generator Impact

This change modifies generator template output. After implementation:

1. Existing projects regenerated with `gtb regenerate` will see minor diffs in non-code locations (added escaping). These are cosmetic for clean inputs and protective for adversarial inputs.
2. The conflict detection system (hash-based) will detect these as generator-side changes and prompt appropriately.
3. No changes to the manifest format or feature flags.

## Error Handling

- Escape functions must be pure and infallible (no error returns). Invalid input is sanitised, never rejected, at the template layer.
- Input validation (rejecting clearly invalid names, channels, etc.) is a separate concern to be addressed in a follow-up spec. This spec focuses solely on defence-in-depth at the template rendering layer.

## Testing Strategy

### Unit Tests (`template_escape_test.go`)

Table-driven tests for each escape function:

| Function | Test Cases |
|----------|------------|
| `escapeYAML` | Plain string (no change), string with `:`, string with `#`, string with `{{`, boolean-like values, empty string, string with single quotes |
| `escapeMarkdown` | Plain string, string with `*`, `_`, `` ` ``, `[`, `{{`, heading injection (`# malicious`), empty string |
| `escapeTOML` | Plain string, string with `"`, `\`, newlines, control characters, empty string |
| `escapeComment` | Plain string, string with newlines, string with `{{`, empty string |

### Integration Tests

- Render each modified skeleton template with adversarial input data containing `{{ .Name }}`, YAML special characters, Markdown syntax, and newlines
- Verify the rendered output is syntactically valid (YAML parses, TOML parses, Markdown renders without injection)
- Verify code-location outputs remain unescaped and functional

### Regression Tests

- Generate a full skeleton with normal input values and verify the output is identical to pre-change output (escape functions are no-ops for clean input)
- Generate a skeleton with each adversarial field individually and verify no template execution errors

## Migration & Compatibility

- **Backward compatible**: Clean input values produce identical output (escape functions are identity for safe strings).
- **No API changes**: Entirely internal to `internal/generator/`.
- **No manifest changes**: File hashes will differ only if templates produce different output for the same input, which only happens for inputs containing special characters.
- **Regeneration safe**: Existing projects with clean inputs will see no hash mismatches after this change.

## Future Considerations

1. **Input validation at the wizard/flag layer**: A complementary spec could add validation rules to `SkeletonOptions.ValidateOrPrompt()` that reject or warn about inputs containing template-sensitive characters. This provides defence at the entry point rather than just the rendering layer.
2. **Fuzz testing**: The escape functions are good candidates for Go fuzz testing to discover edge cases in escaping logic.
3. **Centralised template rendering**: If the generator gains more template-rendered outputs in the future, the function map registration should be centralised (this spec already proposes a package-level `templateFuncMap`).
4. **Content Security Policy for Markdown**: If generated documentation is ever served in a web context, additional XSS protections may be needed.

## Implementation Phases

### Phase 1: Escape Functions and Infrastructure

- Create `internal/generator/template_escape.go` with `escapeYAML`, `escapeMarkdown`, `escapeTOML`, `escapeComment`, and `templateFuncMap`
- Create `internal/generator/template_escape_test.go` with comprehensive unit tests
- Modify `skeleton.go` to register `templateFuncMap` in `renderAndHashSkeletonTemplate`
- Verify all existing tests pass (no functional change yet -- functions are registered but not called)

### Phase 2: Apply Escaping to Skeleton Templates

- Update each skeleton template file to use escape functions at non-code call sites (see Project Structure table)
- Add integration tests rendering templates with adversarial input
- Add regression tests confirming clean-input output is unchanged

### Phase 3: Validation (Optional Follow-Up)

- Add input validation to the wizard and CLI flags to reject or warn about dangerous characters
- This phase is out of scope for this spec but should be tracked as a follow-up

## Open Questions

1. **Should `Name` be restricted to `[a-z0-9-]` at input time?** If so, many template escaping concerns for `Name` become moot. This would be an input validation change (Phase 3), but the answer affects how aggressively we need to escape `Name` in Phase 2.

2. **Should `.goreleaser.yaml` URL values (`api`, `upload`, `download`) escape `Host`?** These are URL components where `Host` is expected to be a valid hostname. Escaping might break valid hostnames with unusual but legal characters. Alternatively, `Host` could be validated as a hostname at input time.

3. **Should the `Org` field in CODEOWNERS be validated against GitHub/GitLab username rules?** The CODEOWNERS file has strict syntax requirements. Escaping alone may not be sufficient if `Org` contains characters invalid in GitHub/GitLab org names.
