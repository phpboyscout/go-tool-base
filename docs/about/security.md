---
title: Security & Trust Model
description: GTB's security posture — what the framework protects against, what it leaves to the tool author, and the deployment contexts each storage and network-handling primitive is designed for.
tags: [about, security, trust-model, credentials]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Security & Trust Model

GTB is a framework for building CLI tools. It ships with opinionated defaults for credential storage, output redaction, URL handling, and dependency hygiene — but it is not a turnkey security product. The sections below describe what the framework guarantees, what responsibilities fall to the tool author, and the deployment contexts each primitive is designed for.

## Credential storage

GTB supports three storage modes for user-supplied secrets (AI API keys, VCS tokens, Bitbucket dual-credentials). The interactive `init` wizards default to env-var references; the literal mode is retained for backward compatibility and refused under `CI=true`.

| Mode | Config records | Secret lives | Best for |
|------|----------------|--------------|----------|
| Env-var reference (default) | `<prefix>.env: <VAR_NAME>` | Shell profile, CI platform secret injection | Everything except airgapped single-user tools |
| OS keychain (opt-in) | `<prefix>.keychain: <service>/<account>` | macOS Keychain / Secret Service / Credential Manager | Desktop users who don't want to manage env vars |
| Literal | `<prefix>.key`, `<prefix>.value`, `bitbucket.username` + `bitbucket.app_password` | The config file on disk | Legacy configs; refused in CI |

### Trust model by deployment context

**Local development (workstation).** Env-var references and the OS keychain are both safe. Pick whichever is most ergonomic — keychain if you don't want to manage shell dotfiles, env-var if you already use `direnv` / `1password-cli` / similar. Keychain activation requires the tool's `main` to blank-import `pkg/credentials/keychain`; the setup wizards detect the live backend via `credentials.Probe()` and hide the option on hosts without a reachable Secret Service provider.

**CI / CD pipelines.** Env-var references only. GTB refuses literal-mode configuration under `CI=true` — a plaintext token in a build artefact is the class of mistake the hardening work aims to prevent. Use your CI platform's secret injection (`${{ secrets.GITHUB_TOKEN }}`, `$ANTHROPIC_API_KEY`, etc.) and map each secret to the config's `<prefix>.env` reference.

**Containerised / Kubernetes.** Env-var references only. Bake the config into the image with `<prefix>.env` pointing at the variable name the platform injects. Never ship a literal credential in a container image. The OS keychain is not applicable (no D-Bus, no Keychain daemon).

**Regulated / air-gapped deployments.** Env-var references. Do **not** blank-import `pkg/credentials/keychain` from the tool's `main` — the linker will omit go-keyring, godbus, and wincred from the shipped binary, giving you an SBOM-clean artefact that cannot perform any IPC to a session bus or platform keychain service. Literal mode is technically still available for the throwaway / single-admin case, but `doctor credentials.no-literal` will warn.

**Throwaway / air-gapped single-user tools.** Literal mode is acceptable if you accept the plaintext-on-disk risk. The config file is written `0600` and the doctor check will still warn — the warning is informational, not a block.

## Credential resolution at runtime

Every VCS, chat, and setup subsystem in GTB resolves credentials through the same chain:

1. Direct value passed by the caller (e.g. `Config.Token`).
2. `<prefix>.env` — name of an environment variable to read.
3. `<prefix>.keychain` — `<service>/<account>` reference passed to the registered `credentials.Backend`.
4. `<prefix>.value` / `<prefix>.key` / `bitbucket.<field>` — literal value stored in config.
5. Well-known ecosystem env var (`ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, etc.).

Steps 2 and 3 return empty silently when the referenced variable is unset or the backend is absent — the chain cascades to the next step. A configured `<prefix>.keychain` that points at a corrupt Bitbucket JSON blob aborts resolution (R3) rather than falling through; that's the one exception, intentional so broken state is surfaced, not masked.

## Logging and output

Three redaction layers protect credential values from leaking into logs and telemetry:

- **`pkg/config` sensitive masker** matches any dot-segment containing `token`, `password`, `secret`, `key`, `apikey`, `auth`, `username`, or `app_password`. Values are rendered as `****<last 4>` in `config get` / `config list` / every surface that goes through `Masker.MaskIfSensitive`. Tool authors extend with `WithKeyPattern` and `WithValuePattern`.
- **`pkg/redact` credential stripper** removes URL userinfo, Authorization headers, common token query parameters, and well-known provider prefixes (`sk-`, `ghp_`, `AIza`, `AKIA`, Slack). Used automatically by `telemetry.TrackCommandExtended` for `args` and `errMsg`. Applies to free-form strings headed to telemetry or distributed-logging backends.
- **Resolver-level contract** — no resolver in `pkg/chat`, `pkg/vcs`, or `pkg/setup` logs a credential value at any level. Errors from the credentials backend (keychain missing, Vault unreachable, permission denied) are wrapped without embedding the secret, per R2 of the hardening spec.

Tool-author responsibility: never interpolate a credential value into a `CheckResult.Message`, a health-check output, or any format string that goes through `p.Logger` at INFO or higher. If a debug log genuinely needs to show something about the credential, include the **name** of the env var or keychain reference — not the value.

## Network handling

**Chat provider endpoints** are validated through `chat.ValidateBaseURL` — HTTPS-only, no URL userinfo, rejected placeholder hosts. `chat.New` logs the endpoint hostname at INFO, never the path or query. The `AllowInsecureBaseURL: true` escape hatch is `json:"-"` so it cannot be enabled via config file — only in-process by a caller with a signed reason.

**Regex compilation** for caller-supplied patterns (Bitbucket `filename_pattern`, docs search) routes through `pkg/regexutil.CompileBoundedTimeout` — 1 KiB length cap, 100 ms compile timeout. Mitigates ReDoS for patterns that originate outside the binary. Literal patterns in source still use `regexp.MustCompile`.

**URL opening** for docs / OAuth / help links routes through `pkg/browser.OpenURL`, which enforces an HTTPS/HTTP/mailto allow-list, a length bound, and control-character rejection before handing off to `cli/browser`. Never call `exec.Command("xdg-open"|"open"|"rundll32")` directly.

## Supply-chain posture

**Core library has zero optional dependencies linked by default.** `pkg/credentials` and its `Backend` interface are in the root module with no external deps beyond the existing ones (`cockroachdb/errors`). The keychain implementation is a separate subpackage — importing it is an explicit decision.

**Release artefacts are FIPS-enabled, CGO-disabled, cross-compiled.** Shipping `gtb` with `CGO_ENABLED=0 GOLANG_FIPS=1` is load-bearing for federal deployments. The go-keyring dependency is pure-Go on all three platforms (macOS via exported C symbols, Linux via godbus, Windows via `danieljoos/wincred`) — no `libsecret-dev` build dependency at compile time.

**Security scanning is part of every PR.** `just security` runs `govulncheck`, `trivy`, `gitleaks`, and `osv-scanner`. Each gates the pipeline independently. Residual accepted risks are documented in `docs/development/security-decisions.md` with explicit rationale.

## Responsibilities by role

### Tool authors (building on GTB)

- **Use the existing resolver chain** (`vcs.ResolveTokenContext`, `chat.New`'s internal `resolveAPIKey`, Bitbucket's `resolveCredentials`) — don't roll your own credential plumbing.
- **Never log credential values.** Pass them through env-var or keychain refs; let the resolver pull the value at the point of use.
- **Decide your keychain posture once** per tool by choosing whether to blank-import `pkg/credentials/keychain` from `cmd/<tool>/main`. Regulated downstreams omit the import. For hybrid deployments, ship two binaries.
- **Don't add `//nolint` for sensitive checks.** The hardening work assumes every lint violation on `gosec`, `errcheck`, etc. is a real signal; silencing them via nolint erodes the guarantee. Refactor instead.
- **Document your trust model** for your tool's users, following the contexts above as a template.

### End users (running a GTB-built tool)

- **Prefer env-var references for shared workstations and CI.** Your shell profile or CI platform secret store becomes the single source of truth.
- **Run `doctor` after `init`.** The `credentials.no-literal` check catches any lingering plaintext credentials from historical configs or copy-paste mishaps.
- **Use `config migrate-credentials` to upgrade.** The command is idempotent — running it twice is safe — and has a `--dry-run` preview if you want to see the plan first. CI/CD pipelines use `--yes` to migrate silently.

### GTB contributors

- **The `Backend` interface is public and stable-ish.** Every method takes `context.Context`. Custom backends (Vault, AWS SSM, 1Password Connect, …) implement it and register via `credentials.RegisterBackend`. See `docs/how-to/custom-credential-backend.md`.
- **Credential keys must appear in the scanner.** Adding a new credential config key (e.g. a new VCS provider) requires an entry in `pkg/cmd/config/migrate_scan.go`'s `knownCredentials` AND in `pkg/cmd/doctor/checks.go`'s `literalCredentialKeys` so migrate and doctor stay aligned.
- **Test against the memory backend.** `pkg/credentials/credtest.MemoryBackend` + `credtest.Install(t)` exercises resolver and setup paths end-to-end without a real OS keychain.

## Related

- [Credentials component reference](../components/credentials.md) — architecture and API
- [Configure credentials how-to](../how-to/configure-credentials.md) — end-user guide to picking a mode
- [Migrate literal credentials how-to](../how-to/migrate-literal-credentials.md) — move off plaintext storage
- [Custom credential backend how-to](../how-to/custom-credential-backend.md) — implement Vault / SSM / etc.
- [Credential storage hardening spec](../development/specs/2026-04-02-credential-storage-hardening.md) — threat model and requirements
- [Security decisions / accepted risks](../development/security-decisions.md) — audit findings and their resolutions
