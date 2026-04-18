---
title: "Template Security — Generator Input Validation and Escape Helpers"
description: "Threat model and contributor guidance for the two-layer defence that protects scaffolded project outputs from injection via user-supplied inputs."
date: 2026-04-17
tags: [development, security, generator, template, validation, escaping]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Template Security

The generator renders scaffolded project files from `text/template` using inputs collected via an interactive wizard, CLI flags, or a regenerate manifest. Because `text/template` performs no automatic escaping, an adversarial or accidentally-malformed input value can produce corrupted output (broken YAML, Markdown injection, path traversal) or disrupt downstream builds.

Two complementary layers of defence apply to every user-influenced field:

1. **Input validation (`internal/generator/validate.go`)** — a constrained character class per field rejects structurally dangerous values at the entry point. Most injection vectors collapse if the input never matches a template-active character.
2. **Output escaping (`internal/generator/template_escape.go`)** — context-aware escape functions pipe values through at non-code template sites, so even if validation ever widens or a new input field is added, the rendering layer remains safe.

## Scope

This page covers the defence layers inside `internal/generator/`. Other generator inputs (command flags, manifest commands) follow their own conventions:

- Jennifer-generated Go (`skeleton_root.go` and friends) handles escaping via `jen.Lit()` — which produces correctly-escaped Go literals automatically. No additional template-escape helpers apply there.
- Shell and OS commands run by the generator use `exec.Command` (no shell) so argument quoting is not a concern.

## Input Validation

Every user-influenced field has a dedicated validator in `internal/generator/validate.go`. Each validator:

- Normalises the input to Unicode NFC form before checking. Homoglyph attacks (`ρaypal`, hidden ZWJ) fail fast this way.
- Applies a strict, anchored regex or structural check (e.g. `url.Parse` + scheme allowlist for endpoints).
- Returns a `cockroachdb/errors` value wrapping the `ErrInvalidInput` sentinel so callers can distinguish validation failures via `errors.Is`.
- Produces a hint that names the field and the rule, and includes the offending input (truncated) only when doing so aids debugging. No hint reveals more than the first 32 runes of the input.

### Field Rules

| Field | Rule | Rationale |
|-------|------|-----------|
| `Name` | `^[a-z][a-z0-9-]{0,63}$` | Lowercase-only, letter first; forecloses path traversal, Unicode spoofing, and injection in a single rule. |
| `Description` | ≤ 500 bytes after NFC; no control chars except `\t`; no `{{` or `}}` | Length-bounds YAML/TOML values; ASCII-control ban prevents YAML structural injection; template-brace ban is belt-and-braces. |
| `Repo` | Go module path — domain + segments `[a-zA-Z0-9._~-]+`; no leading/trailing `/`; no `..` or `.` segments | Matches `go mod tidy` acceptability; rejects traversal early. |
| `Host` | RFC 1123 hostname with optional `:port`; punycode accepted, raw Unicode rejected | Prevents homoglyph-spoofed URLs in documentation; rejects URL construction errors. |
| `Org` (github) | `^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$` | GitHub's own org-name rules; invalid values silently drop in CODEOWNERS. |
| `Org` (gitlab) | Same first-char rule; allows `/`-separated subgroups ≤ 4 deep; ≤ 255 chars total | GitLab namespace rules. |
| `EnvPrefix` | `^[A-Z][A-Z0-9_]{0,31}$` (or empty) | Valid environment-variable prefix; excludes shell metacharacters. |
| `SlackChannel` | `^[a-z0-9-]{1,80}$` (or empty, leading `#` stripped) | Slack's own channel naming rules. |
| `SlackTeam` | `^[a-zA-Z0-9][a-zA-Z0-9-]{0,20}$` (or empty) | Slack workspace rules. |
| `TeamsChannel` / `TeamsTeam` | ≤ 100 bytes; no control chars; no `{{`/`}}` | Teams is less constrained than Slack; apply YAML-safety. |
| `TelemetryEndpoint` / `TelemetryOTelEndpoint` | Parses as URL; scheme `http` or `https`; no control chars; ≤ 2048 bytes | Prevents endpoint-config injection into scaffolded YAML. |

## Output Escaping

The `templateFuncMap` in `template_escape.go` is registered on every `text/template` used by the generator. Call sites in non-code locations pipe their values through the appropriate helper:

```text
# README.md — Markdown prose context
{{ .Name | escapeMarkdown }} is a tool built with [gtb](...).

# zensical.toml — TOML string values
site_name = "{{ .Name | escapeTOML }}"
site_description = "{{ .Description | escapeTOML }}"

# .goreleaser.yaml — mixed contexts
project_name: {{ .Name | escapeYAML }}   # YAML value (non-code)
    main: cmd/{{ .Name }}/main.go        # code path (no escape)

# justfile — mixed contexts
# Build the {{ .Name | escapeComment }} binary   # comment (non-code)
build: go build -o bin/{{ .Name }}                 # code path (no escape)
```

### Helper Contract

Every escape function is:

- **Pure.** Same input → same output; no side effects; safe for concurrent use.
- **Infallible.** Invalid UTF-8 is replaced with U+FFFD; every other input produces well-formed output.
- **Identity on the safe class.** For inputs matching `^[a-zA-Z0-9 _.,/-]*$`, the output equals the input. Clean projects see no diff after piping values through the helpers.
- **Syntactically valid in the target format.** `escapeYAML` output parses as a YAML scalar; `escapeTOML` output parses as a TOML basic string; `escapeMarkdownCodeBlock` output contains no `` ``` `` fence sequence.

### Helpers Available

| Function | Purpose |
|----------|---------|
| `escapeYAML` | Double-quoted YAML scalar with `\`/`"`/control bytes escaped. Unconditional quoting avoids YAML 1.1/1.2 implicit-typing edge cases (`yes`, `null`, `1.0`). |
| `escapeMarkdown` | CommonMark prose context. Escapes `\`, backtick, `*`, `[`, `]`, `<`, `>`, `|`, `{`, `}`, `!`, `#`. Leaves `_`, `.`, `-`, `+` alone so ordinary prose (`v1.0.0`, `foo_bar`) survives unchanged. |
| `escapeMarkdownCodeBlock` | Fenced code block content. Runs of 3+ backticks are broken with a zero-width space between the 2nd and 3rd so the enclosing fence cannot close early. Idempotent by construction. |
| `escapeTOML` | TOML basic-string interior (without enclosing quotes). |
| `escapeComment` | Single-line comment contexts (`#` in YAML / justfile / CODEOWNERS). Newlines and NUL bytes become spaces so comment scope cannot escape. |
| `escapeShellArg` | POSIX single-quoted shell argument; interior single quotes become `'\''`. Used in justfile recipe bodies when user input reaches a shell. |

## Adding a New User-Input Field

When you add a new field that flows from the wizard/flags/manifest into skeleton templates:

1. **Add a validator** in `validate.go` with a rule as tight as reasonable. Prefer a strict character class over a permissive one; start narrow and relax only with a clear use case.
2. **Test the validator** with representative accepting and rejecting inputs, including Unicode adversarial cases (NUL, RTL override, zero-width joiner, CJK homoglyphs).
3. **Audit the template call sites** for the new field. For each non-code site, pipe the field through the appropriate escape helper. Code sites (paths, identifiers, Go source strings) must not be piped.
4. **Decide whether the field is required or optional** and wire that into `ValidateManifest` accordingly: required fields hard-fail on empty; optional fields short-circuit to nil.
5. **Run the existing fuzz and regression tests.** No golden-hash drift should occur for existing clean fixtures — the escape functions are identity on the safe class.

See `docs/development/specs/2026-04-02-generator-template-escaping.md` for the full rationale and the complete audit of template locations.
