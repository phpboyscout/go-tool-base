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
| Command `Name` (`ValidateCommandName`) | `^[a-z][a-z0-9_-]{0,63}$`; `/`, `\`, and `.` rejected explicitly; `root` and `options` reserved | The name flows into `filepath.Join(path, "pkg", "cmd", name)` and `FS.RemoveAll`; this rule forecloses path traversal from CLI flags and tampered manifests alike. Underscores are permitted for snake_case command names. |
| Parent path (`ValidateParentPath`) | `/`-separated command names; literal `root` (or empty) accepted as-is | `--parent` segments join into the same command-directory path. |
| `Signing.Backend` (`ValidateSigningBackend`) | `^[a-z][a-z0-9-]{0,31}$` (or empty) | Registered `gtb sign` backend names (`aws-kms`, `local`); rendered into the CI-executed `.goreleaser.yaml` signs block. |
| `Signing.KMSRegion` (`ValidateSigningKMSRegion`) | `^[a-z][a-z0-9-]{0,31}$` (or empty) | AWS region identifiers; same render site. |
| `Signing.KeyID` (`ValidateSigningKeyID`) | `^[a-zA-Z0-9:/_.=+,@-]{1,256}$` (or empty), and must not contain a literal `..` substring | KMS ids/ARNs/aliases plus the local backend's PEM paths; quotes, whitespace, and control characters are outside the class so the value cannot break out of its quoted YAML scalar. The `..` rejection is defence-in-depth — legitimate KMS ids/ARNs/aliases (`alias/my-key`) and local relative PEM paths (`./release.pem`) never contain it. |
| `Signing.PublicKey` (`ValidateSigningPublicKey`) | Clean `/`-separated path relative to the project root; a single leading `./` is normalised away (`./key.asc` is accepted as `key.asc`); no absolute paths, `..` segments, or backslashes; segments `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$` | The field is a path to the armored public-key file (default `internal/trustkeys/keys/signing-key-v1.asc`), not inline armor. The leading-`./` normalisation is a friendliness affordance; the path must still resolve cleanly inside the project root after it. |

### Where the Validators Fire

`ValidateManifest` is the gate for the regenerate path: it walks `Properties`, `ReleaseSource`, **every command in the `Commands` tree** (via `ValidateCommandName`), and **every rendered `Signing` field**, so a tampered `.gtb/manifest.yaml` cannot drive writes or deletes outside the project tree, nor inject YAML into the release pipeline.

The same rules apply at every entry point, with severity matched to the input's origin:

- **CLI flags / interactive wizard** (`gtb generate command`, `gtb remove command`, `gtb generate project --signing-*`, `gtb enable signing`) — an invalid value is a **hard error**: the user typed it and can correct it.
- **Manifest entries** (the `regenerate` path) — an invalid command or signing block is **skipped, not fatal**: the generator logs at ERROR level naming the entry and the rule it failed, and the remaining valid entries still regenerate. A skipped entry is never acted on, so the `filepath.Join`/`RemoveAll` sink is foreclosed. The on-disk manifest is left untouched for the user to fix.
- **The join sink itself** (`getCommandPath`) — independently of entry-point validation, every resolved command directory is checked (via `filepath.Rel`) to sit strictly under `<project>/pkg/cmd`.

### AI Doc-Tool Containment

The AI documentation generator exposes `read_file`, `list_dir`, and `go_doc` tools to the model. All three are contained to the project root: model-supplied relative paths are joined to the absolute project root and rejected (`filepath.Abs` + `filepath.Rel`) if the result escapes it, so a model-supplied `../../etc/passwd` cannot read or list outside the tree.

## Output Escaping

The `templateFuncMap` in `template_escape.go` is registered on every `text/template` used by the generator. Call sites in non-code locations pipe their values through the appropriate helper:

```text
# README.md — Markdown prose vs fenced code block
{{ .Name | escapeMarkdown }} is a tool built with [gtb](...).   # prose
    go install {{ .Repo | escapeMarkdownCodeBlock }}@latest     # inside ``` fence

# zensical.toml — TOML string values
site_name = "{{ .Name | escapeTOML }}"
site_description = "{{ .Description | escapeTOML }}"

# .goreleaser.yaml — mixed contexts
project_name: {{ .Name | escapeYAML }}   # YAML value (non-code)
    main: cmd/{{ .Name }}/main.go        # code path (no escape)

# justfile — mixed contexts
# Build the {{ .Name | escapeComment }} binary               # comment (non-code)
build: go build -o bin/{{ .Name | escapeShellArg }} ...      # sh recipe body (shell arg)
```

Both `escapeShellArg` (justfile recipe bodies) and `escapeMarkdownCodeBlock`
(fenced README/docs blocks) are wired at the render sites above as
defence-in-depth: `ValidateName`/`ValidateRepo` already restrict the inputs to
a shell- and fence-safe character class, so clean projects see no diff, but the
pipe keeps the output safe if a validator is ever widened. The
`TestEscape_HelpersWiredAtDocumentedSites` test asserts the pipes stay in place.

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
