---
title: Security Decisions & Accepted Risks
description: Living record of security decisions, accepted risks, and mitigations from audits of the GTB project.
date: 2026-04-02
tags: [development, security, audit, decisions, risk]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Security Decisions & Accepted Risks

This document records security-related decisions and accepted risks identified during audits of the GTB project. It serves as a reference for contributors and tool authors who need to understand why certain behaviours exist and what responsibilities fall to the consuming developer.

Each entry includes the finding identifier, a description of the behaviour, the rationale for accepting it, and any guidance for tool authors.

---

## Audit: 2026-04-02

### Accepted Risks

#### M-1: Health Endpoint Error Messages

**Severity:** Medium | **Status:** Accepted

Health endpoint responses (`/healthz`, `/livez`, `/readyz`) include error detail messages returned by `StatusFunc` and `ProbeFunc` callbacks. This is intentional — health responses must convey enough information for operators to diagnose issues without requiring log access.

**Tool author responsibility:** Sanitize error messages before returning them from health check callbacks. Do not pass raw database connection strings, internal hostnames, credentials, or stack traces through `CheckResult.Message`. Return a descriptive but non-sensitive summary instead.

```go
// Bad — leaks connection string
return controls.CheckResult{
    Status:  controls.CheckUnhealthy,
    Message: fmt.Sprintf("connection failed: %v", err), // err may contain "postgres://user:pass@internal-host:5432/db"
}

// Good — descriptive without sensitive detail
return controls.CheckResult{
    Status:  controls.CheckUnhealthy,
    Message: "database connection failed: timeout after 2s",
}
```

See also: [Register Custom Health Checks](../how-to/register-health-checks.md#security-considerations).

---

#### M-4: Credentials Not Cleared from Memory

**Severity:** Medium | **Status:** Accepted

Go's garbage-collected runtime makes reliable memory zeroing of `string` values impractical. Strings are immutable and may be copied by the runtime, interned, or retained in dead stack frames until the garbage collector reclaims them. There is no portable way to guarantee that sensitive values (SSH passphrases, VCS tokens) are scrubbed from process memory.

**Mitigating factors:**

- **CLI tools** (the primary GTB use case) have short process lifetimes. Credentials exist in memory only for the duration of the command.
- **Long-lived services** should be aware that credentials remain in process memory for the lifetime of the struct that holds them. If this is a concern, isolate credential-holding components and consider process-level isolation (e.g., separate sidecar for secret retrieval).

**Guidance for tool authors:** For services handling multiple users or operating in high-security environments, avoid storing credentials in long-lived structs. Prefer short-lived credential retrieval (e.g., fetch a token, use it, let the variable go out of scope) over caching credentials for the process lifetime.

---

#### M-6: `update --from-file` Accepts Arbitrary Paths

**Severity:** Medium | **Status:** Accepted (by design)

The `update --from-file <path>` command accepts any filesystem path the user provides. This is intentional for a CLI tool where the invoking user explicitly supplies the path and the operating system's file permission model applies.

**Rationale:** Adding path restrictions (e.g., limiting to a specific directory) would reduce the utility of the feature without improving security. The user already has shell access and could copy the file to an allowed directory. OS-level permissions are the correct enforcement mechanism.

---

#### L-1: Debug-Mode Stack Traces

**Severity:** Low | **Status:** Accepted (by design)

When the log level is set to `DEBUG`, error responses include full stack traces via `cockroachdb/errors`. This is intentional for development troubleshooting — stack traces are essential for diagnosing error origins.

**Guidance for tool authors:** Ensure production deployments do not run with the log level set to `DEBUG`. Use `INFO` or `WARN` as the default, and document that `DEBUG` is for development use only.

---

#### L-2: Provider Environment Variable Logged at Debug Level

**Severity:** Low | **Status:** Accepted

The resolved AI provider name (e.g., `"anthropic"`, `"openai"`) is logged at `DEBUG` level during chat client initialisation. This aids troubleshooting when users have multiple providers configured.

**Clarification:** The API key is never logged. Only the provider name (a non-sensitive configuration value) appears in log output.

---

#### L-4: SSH Key Discovery Scans ~/.ssh

**Severity:** Low | **Status:** Accepted (by design)

The TUI key selection during `init` reads filenames from `~/.ssh` to present an interactive list of available keys. This is a directory listing only — file contents are never read unless the user explicitly selects a key.

**Rationale:** SSH key discovery is a standard requirement for the initialisation workflow. Keys from unrelated systems (personal keys, keys for other services) may appear in the list but are never accessed unless explicitly selected. The alternative — requiring users to type the full path manually — would degrade the setup experience without improving security.

---

#### L-5: Machine ID in Telemetry

**Severity:** Low | **Status:** Accepted (by design)

The machine ID transmitted in telemetry events is a SHA-256 hash of multiple system signals (OS machine ID, MAC address, hostname, username), truncated to 16 hex characters. Raw identifiable values are never transmitted.

**Privacy properties:**

- The hash is one-way — the original values cannot be recovered.
- Truncation to 16 hex characters further reduces collision resistance but also limits re-identification potential.
- The machine ID is used solely for deduplication and aggregate counting, not user identification.

See also: [Telemetry design](specs/2026-03-21-opt-in-telemetry.md) for the full privacy model.

---

## Audit: 2026-04-17

### Remediated Findings

#### H-2 & H-3: User-Supplied Regex Patterns Compiled Without Bounds

**Severity:** High | **Status:** Remediated

Two call sites compiled caller-supplied regex patterns via `regexp.Compile` without length or timeout bounds: `pkg/vcs/bitbucket/release.go` (the `filename_pattern` config key) and `pkg/docs/tui.go` (the docs-browser search query). Go's RE2 engine mitigates classical catastrophic backtracking at match time, but compile time is not guaranteed linear — a sufficiently large or pathological pattern can still stall the compile step long enough to be user-visible.

**Mitigation.** Introduced [`pkg/regexutil`](../components/regexutil.md) with `CompileBounded` and `CompileBoundedTimeout` helpers enforcing a 1 KiB length cap and a 100 ms wall-clock compile timeout. Both affected call sites route through the helper. Tool authors accepting patterns in their own config should use the same helper — see [the component doc](../components/regexutil.md#call-site-discipline) and `CLAUDE.md` § Regex Compilation.

**Tool author responsibility.** Never call `regexp.Compile` directly on a pattern that originates outside the binary. The helper is the designated entry point; bypassing it reintroduces the ReDoS class this remediation closes.

Spec: [2026-04-17-regex-hardening.md](specs/2026-04-17-regex-hardening.md).

---

#### M-3: Chat Provider BaseURL Accepted Without Validation

**Severity:** Medium | **Status:** Remediated

`chat.Config.BaseURL` was accepted by the OpenAI-compatible and Gemini paths without validation. An operator who could influence config — tampered file, hostile environment variable, compromised setup wizard — could redirect API traffic (and its `Authorization` header) to an attacker-controlled HTTPS host. URLs of the form `https://user:pass@host/` were particularly risky: some HTTP libraries propagate the userinfo as Basic auth, others log the full URL.

**Mitigation.** Every `chat.New` call now routes through `chat.ValidateBaseURL`, which rejects non-HTTPS schemes, URLs with userinfo, oversize or control-character-bearing inputs, and placeholder hosts (`example.com` and subdomains). The test-only `Config.AllowInsecureBaseURL` opt-out is tagged `json:"-"` so configuration files cannot downgrade HTTPS enforcement. Every successful provider init logs the endpoint hostname at INFO for operational audit trail.

**Tool author responsibility.** Validate `BaseURL` values at the boundary (your setup wizard, CLI flag parser, or config loader) via `chat.ValidateBaseURL` — not only at `chat.New` time — so misconfiguration is reported with context.

Spec: [2026-04-17-chat-baseurl-validation.md](specs/2026-04-17-chat-baseurl-validation.md).

---

#### M-5 & M-6: Telemetry and OTel Headers Could Leak Credentials

**Severity:** Medium | **Status:** Remediated

`TrackCommandExtended` shipped `errMsg` and `args` verbatim to the configured telemetry backend when `ExtendedCollection` was enabled. A typical error message such as `failed GET https://api.example.co/?apikey=sk-abc123: 401` embedded an API key in the outgoing event. Separately, `WithOTelHeaders` accepted arbitrary headers — tool authors routinely place bearer tokens in `Authorization` or `X-API-Key` — and the surrounding HTTP middleware could log those values at DEBUG.

**Mitigation.** Introduced [`pkg/redact`](../components/redact.md) with `String`, `Error`, `SensitiveHeaderKeys`, and `IsSensitiveHeaderKey`. `TrackCommandExtended` now applies `redact.String` unconditionally to both `errMsg` and every entry of `args` before the event is appended to the buffer. `WithOTelHeaders` records an advisory `WARN` per caller-supplied header key that matches the sensitive pattern, emitted at backend construction time via the configured logger. The HTTP middleware header-redaction map in `pkg/http/logging.go` is now sourced from `redact.SensitiveHeaderKeys` so all three surfaces share one catalogue.

**Tool author responsibility.** Any tool-owned log line, custom telemetry event, or third-party observability payload containing free-form strings should be routed through `redact.String` / `redact.Error`. The package is the single entry point for untrusted-string redaction across GTB.

Spec: [2026-04-17-telemetry-redaction.md](specs/2026-04-17-telemetry-redaction.md).

---

#### Generator: Template Injection via User-Supplied Inputs

**Severity:** Medium | **Status:** Remediated

`internal/generator/skeleton.go` rendered scaffolded project files from `text/template` with user-supplied data (`Name`, `Description`, `Repo`, `Host`, `Org`, Slack/Teams identifiers, telemetry endpoints) and no automatic escaping. An adversarial or accidentally-malformed value could produce corrupted YAML, Markdown injection, path traversal via `..`, Unicode homoglyph spoofing, or broken Go compilation. The rendered output lands on the contributor's disk and typically gets committed verbatim.

**Mitigation.** Two-layer defence in `internal/generator/`:

1. **Input validation** (`validate.go`) — every user-influenced field is NFC-normalised and checked against a tight character-class rule: `Name ^[a-z][a-z0-9-]{0,63}$`, `Description ≤ 500 bytes + no control chars + no `{{`/`}}``, `Repo` Go-module-path shape, RFC 1123 `Host` (punycode-only), GitHub- or GitLab-specific `Org` rules, `EnvPrefix ^[A-Z][A-Z0-9_]{0,31}$`, Slack/Teams naming rules, and HTTP/HTTPS `URL.Parse` for telemetry endpoints. Rejections wrap `ErrInvalidInput`. Runs at wizard, flag, and manifest-load entry points.
2. **Output escaping** (`template_escape.go`) — context-aware helpers (`escapeYAML`, `escapeMarkdown`, `escapeMarkdownCodeBlock`, `escapeTOML`, `escapeComment`, `escapeShellArg`) registered via `templateFuncMap` on every `text/template`. Non-code template sites in skeleton templates pipe values through the appropriate helper. Every helper is pure, infallible, idempotent-where-applicable, and identity on the safe character class `[a-zA-Z0-9 _.,/-]`.

**Tool author responsibility.** When adding a new user-facing field to the generator: add a validator in `validate.go`, update `ValidateManifest`, and pipe the field through the appropriate escape helper at every non-code template call site. See `docs/development/template-security.md` for the full contributor guide.

Spec: [2026-04-02-generator-template-escaping.md](specs/2026-04-02-generator-template-escaping.md).

---

#### H-1 (2026-04-02 audit): Plaintext Credentials in Config Files

**Severity:** High | **Status:** Remediated — Phase 1 of 3

The interactive setup wizard for both AI providers and the VCS integrations wrote API keys and tokens to `~/.<tool>/config.yaml` as plaintext. Config file permissions are restricted to `0600`, but plaintext secrets on disk remain exposed to backups, dotfile sync, shared workstations, compromised local accounts, and accidental commits to public repositories.

**Mitigation (Phase 1 of 3).** Introduced [`pkg/credentials`](../../pkg/credentials/) with a `Mode` taxonomy (`ModeEnvVar`, `ModeKeychain`, `ModeLiteral`), sentinel errors, and a keychain-capability probe. The AI setup wizard presents a storage-mode selector defaulting to env-var mode; the config now records `{provider}.api.env: <VAR_NAME>` instead of the literal key when env-var mode is chosen. The chat client's credential resolution checks `{provider}.api.env` before the literal key, so env-var mode is honoured at runtime. The GitHub wizard refuses to write a literal token when `CI=true` and short-circuits when a `GITHUB_TOKEN`-style env-var is already configured. The Bitbucket dual-credential resolver (`pkg/vcs/bitbucket`) gained `bitbucket.{username,app_password}.env` env-var reference precedence. A new `doctor` check `credentials.no-literal` warns when literal credentials remain in the loaded config.

**Deferred to Phase 2 & 3.**
- **Phase 2**: OS keychain integration behind `-tags keychain` (dep on `github.com/zalando/go-keyring`), `auth.keychain` resolution step in `pkg/vcs/auth.go`, full keychain storage for dual-credential Bitbucket entries.
- **Phase 3**: `config migrate-credentials` command, GitHub OAuth+display-once flow, BDD coverage, migration guide.

**Tool author responsibility.** New user-supplied credentials should route through the same three-mode pattern: prefer env-var references, fall back to literal only outside CI, and register a new `doctor` pattern when introducing a new config key that may hold a secret.

Spec: [2026-04-02-credential-storage-hardening.md](specs/2026-04-02-credential-storage-hardening.md).

---

## Adding New Entries

When a new security audit or review produces findings, add them to this document under a dated audit heading. Each entry should include:

1. **Finding identifier** (e.g., M-1, L-3) matching the audit report.
2. **Severity** (Critical, High, Medium, Low, Informational).
3. **Status** (Accepted, Mitigated, Remediated, Deferred).
4. **Description** of the behaviour.
5. **Rationale** for the decision.
6. **Guidance** for tool authors where applicable.
