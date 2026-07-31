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
| Command `Name` (`ValidateCommandName`) | `^[a-z][a-z0-9_-]{0,63}$`; `/`, `\`, and `.` rejected explicitly; `root` and `options` reserved; **Go reserved words rejected** (`token.IsKeyword`) | The name flows into `filepath.Join(path, "pkg", "cmd", name)` and `FS.RemoveAll`; this rule forecloses path traversal from CLI flags and tampered manifests alike. Underscores are permitted for snake_case command names. The name also becomes a Go package name (`package <name>`), so a keyword (`func`, `type`, …) would produce uncompilable output — rejected up front with a clear message rather than emitted broken. |
| `PackagePath` (`ValidatePackagePath`) | Clean `/`-separated path relative to the project root; single leading `./` normalised away; no absolute paths, `..`, or backslashes; segments `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$` | The `--package` argument to `generate docs` flows verbatim into `filepath.Join` for both the source dir and the output path; the clean-relative rule **forecloses writing the generated doc outside `docs/`**. |
| `CIComponentSource` (`ValidateCIComponentSource`) | ≤ 255 bytes; no control chars; no `{{`/`}}`; runes limited to letters, digits, `.`, `-`, `_`, `/` (or empty); URL schemes rejected | The value is interpolated **verbatim into an unquoted YAML `include:` component path** in the scaffolded `.gitlab-ci.yml`; the bare-host/path class stops a scheme, whitespace, or template delimiter from breaking out of that include and injecting pipeline configuration. |
| `FlagName` (`ValidateFlagName`) | `^[a-z][a-z0-9-]{0,63}$` | The flag's long name becomes a **Go identifier** (pascalCased) in the generated options struct and a cobra `Flags().XxxVar` registration, so it is constrained the same way command names are. |
| `FlagShorthand` (`ValidateFlagShorthand`) | Exactly one ASCII letter (or empty) | Becomes cobra's single-rune shorthand (`-x`) in the generated `StringVarP`; anything longer or non-letter is rejected before it reaches generated code. |
| `FlagType` (`ValidateFlagType`) | Membership in the generator's known flag-type set (empty and `string` accepted) | An unknown type silently degrades to a `string` flag in the generated code, so the non-interactive `add-flag` path rejects it rather than emitting a mistyped flag. This is a **reject-not-fallback** guard, not a character-class rule. |
| `UpdatePolicy` (`ValidateUpdatePolicy`) | One of `disabled`, `prompt`, `enabled` (or empty) | Selects a typed `props.UpdatePolicy` constant rendered into the tool; an unknown value is **rejected rather than silently treated as disabled** — an enum guard, not a fallback. |
| `UpdateCheckInterval` (`ValidateUpdateCheckInterval`) | ≤ 32 bytes; a valid, non-negative Go duration (`time.ParseDuration`, e.g. `24h`, `168h`) — or empty | Rendered into the tool's `props.Tool.UpdateCheckInterval` as a `time.Duration` expression; a malformed or negative value is **rejected rather than silently falling back**. The length cap bounds the parse input. |
| `FeatureName` (`ValidateFeatureName`) | Membership in `ToggleableFeatures` | The `gtb enable`/`gtb disable` name must be a real toggleable feature; an unknown name **fails fast with the valid set listed** rather than writing a junk manifest entry. |
| Parent path (`ValidateParentPath`) | `/`-separated command names; literal `root` (or empty) accepted as-is | `--parent` segments join into the same command-directory path. |
| `Signing.Backend` (`ValidateSigningBackend`) | `^[a-z][a-z0-9-]{0,31}$` (or empty) | Registered `gtb sign` backend names (`aws-kms`, `local`); rendered into the CI-executed `.goreleaser.yaml` signs block. |
| `Signing.KMSRegion` (`ValidateSigningKMSRegion`) | `^[a-z][a-z0-9-]{0,31}$` (or empty) | AWS region identifiers; same render site. |
| `Signing.KeyID` (`ValidateSigningKeyID`) | `^[a-zA-Z0-9:/_.=+,@-]{1,256}$` (or empty), and must not contain a literal `..` substring | KMS ids/ARNs/aliases plus the local backend's PEM paths; quotes, whitespace, and control characters are outside the class so the value cannot break out of its quoted YAML scalar. The `..` rejection is defence-in-depth — legitimate KMS ids/ARNs/aliases (`alias/my-key`) and local relative PEM paths (`./release.pem`) never contain it. |
| `Signing.PublicKey` (`ValidateSigningPublicKey`) | Clean `/`-separated path relative to the project root; a single leading `./` is normalised away (`./key.asc` is accepted as `key.asc`); no absolute paths, `..` segments, or backslashes; segments `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$` | The field is a path to the armored public-key file (default `internal/trustkeys/keys/signing-key-v1.asc`), not inline armor. The leading-`./` normalisation is a friendliness affordance; the path must still resolve cleanly inside the project root after it. |
| `Signing.ExternalKeyEmail` (`ValidateSigningExternalKeyEmail`) | Email-shaped: `local@domain` with a single `@`, no whitespace or control characters; ≤ 254 bytes (or empty) | Written raw into the `// gtb:signing` annotation of the generated `pkg/cmd/root/provenance.go`; the class excludes newlines (comment breakout) and spaces (the annotation's space-separated KV encoding). |
| `Signing.KeySource` (`ValidateSigningKeySource`) | One of `embedded`, `external`, `both` (or empty) | Enum for the trust-anchor source recorded in the manifest and the provenance annotation; an unknown value is rejected rather than silently recorded. |
| `ReleaseSource.Type` (`ValidateReleaseSourceType`) | One of `github`, `gitlab` (or empty for the host-derived default) | Selects the skeleton asset set; `gitea`/`bitbucket` are reserved for the forge adapters but rejected until skeleton assets exist for them. |
| `ReleaseSource.Repo` (`ValidateRepoName`) | Single path segment `^[a-zA-Z0-9][a-zA-Z0-9._~-]*$`; ≤ 255 bytes | The manifest's bare repository name is joined into `{{ .Repo }}`/`{{ .ModulePath }}` and rendered raw into CI-executed files (`.gitlab-ci.yml` component inputs, `.goreleaser.yaml` ldflags, `.golangci.yaml`, `.mockery.yml`); quotes, whitespace, brackets, and separators are all outside the class. (The CLI `--repo` module path is covered by `ValidateRepo` above.) |
| `LongDescription` (`ValidateLongDescription`) | ≤ 4000 bytes after NFC; no control chars except `\t` and `\n` | Multi-line long descriptions are legitimate, so newlines are allowed; `\|` need not be banned because the Markdown table sink escapes it (`escapeMarkdownTableCell`). |
| Flag `Default` with `default_is_code: true` (`ValidateFlagDefaultCode`) | Bare Go identifier or dot-joined selector: `^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`; ≤ 128 bytes (or empty) | The value is emitted **verbatim as Go source** via `jen.Id` in the generated command; the strict identifier/selector grammar (e.g. `defaultTimeout`, `time.Second`) forecloses compiling arbitrary Go into a regenerated tool. Expression defaults (`5 * time.Second`) are not admitted — no existing manifest or fixture uses them. |

### Where the Validators Fire

`ValidateManifest` is the gate for the regenerate path: it walks `Properties`, `ReleaseSource` (type, host, owner, **and repo name**), **every command in the `Commands` tree** — name, description, long description, and **every flag** (name, type, shorthand, description, and the code-default grammar when `default_is_code: true`) — and **every rendered `Signing` field** including the provenance-annotation fields (`external_key_email`, `key_source`). A tampered `.gtb/manifest.yaml` therefore cannot drive writes or deletes outside the project tree, inject YAML into CI-executed files, compile arbitrary Go through a flag default, or break out of the provenance comment.

The `generate command` CLI applies the same rules at its boundary: `--short`/`--long` run through `ValidateDescription`/`ValidateLongDescription`, and each colon-delimited `--flag` definition is validated field-by-field (including `ValidateFlagDefaultCode` when the definition marks its default as code).

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

Not every render site is a `text/template`: the boilerplate docs builder
(`internal/generator/docs.go`) assembles Markdown with `fmt.Fprintf` on a
`strings.Builder`, so the FuncMap pipes never apply there. Its sites call the
helpers directly — `escapeMarkdown` for the description/long-description prose
and `escapeMarkdownTableCell` (composed with `escapeMarkdown`) for every flags-
and subcommands-table cell. When adding a Go-side render site, call the helper
explicitly; the two-layer rule (validate at the gate, escape at the sink) applies
regardless of the rendering mechanism.

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
| `escapeMarkdownTableCell` | GFM table cell content. Newlines collapse to spaces (a newline terminates the row), any `\|` not already backslash-escaped becomes `\\\|` (honoured by GFM even inside code spans), remaining control bytes are stripped. Composes with `escapeMarkdown` and is idempotent. Used by the boilerplate docs builder (`internal/generator/docs.go`) for the flags and subcommands tables. |
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

See [`0055-generator-template-escaping`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0055-generator-template-escaping) for the full rationale and the complete audit of template locations.

## Custom Template Overlays — A Different Threat Model

The escape-at-known-sites model above protects GTB's **own** templates: GTB authors every template and escapes the **user-supplied field values** it interpolates. Custom template overlays ([`0080-generator-custom-partial-templates`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0080-generator-custom-partial-templates)) deliberately step **outside** that perimeter — the generator renders `text/template` content **GTB did not author**, fetched (for git sources) over the network from a repository the framework does not control. The **template author controls the output directly**, so the escape helpers cannot guarantee a custom template's output is well-formed; that correctness is the **template author's responsibility**.

GTB's guarantees for custom overlays are therefore confined to **where** output may land and **what data** a template may see — not the bytes emitted. The posture is **trusted-source with bounded blast radius**, *not* a sandbox (a true sandbox is out of scope). Adding a source **is** the trust decision; the SHA pin records exactly what was trusted.

### Hard controls (always on)

| Control | What it stops | Where |
|---------|---------------|-------|
| **Write-path containment** | A source file rendering to `../escape` or an absolute path leaving the project tree | `containedOutputPath` — `filepath.Abs` + `filepath.Rel` confine every output strictly under the project root |
| **Protected-path denylist** | An overlay shadowing `.gtb/**` (manifest/ignore), `internal/trustkeys/**` (signing anchors), or `go.mod`/`go.sum` — supply-chain injection | `isProtectedOverlayPath` — denied unconditionally, even for a `replaces:` source |
| **Restricted FuncMap** | A template reaching a file/exec/env/network helper | `overlayFuncMap` — only the pure escape helpers + pure string/format funcs; nothing that reads files, runs commands, opens sockets, or reads env |
| **Metadata-only data contract** | Exfiltration of secrets via the data context | `TemplateContractData` — a versioned, secret-free projection of `skeletonTemplateData`; no resolved token, env var, absolute path, or forge credential is reachable |
| **Inert fetch** | Clone-time code execution (hooks, filters, submodules) | go-git `PlainClone` runs no hooks/filters; submodules are not recursed; a per-file 1 MiB bound caps a pathological source |
| **Manifest-gate validation** | A tampered `templates` entry driving a fetch/write outside the rules | `ValidateTemplateSource` (type, location char-class, ref/SHA shape) via `ValidateManifest`; the source `gtb-template.yaml` is validated for a known `contract:` version and a maintained `replaces:` alias |
| **SHA pin** | A branch/tag `ref` silently changing under the operator between runs | `resolved` commit SHA; `regenerate` targets the SHA, never the moving ref |
| **First-use confirmation** | A remote source being fetched without an explicit trust decision | Interactive confirm naming host/owner/repo + ref; suppressible only under `--ci`/non-interactive, where supplying `--template` is itself the acceptance |

### Output escaping reality

GTB **cannot** guarantee a custom template emits valid YAML/Markdown/etc., and a malicious `.gitlab-ci.yml` or `justfile` it renders could run attacker code when CI/`just` later runs. That downstream-injection risk is inherent to running an operator-chosen template and is **out of scope** for GTB to neutralise — it is the same trust the operator extends to a `Makefile` or a pre-commit hook they chose to run. The controls above bound the **blast radius at generate time**; they do not vet the semantics of the emitted bytes.
