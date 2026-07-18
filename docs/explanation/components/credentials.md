---
title: "Credentials"
description: "go-tool-base's integration of the standalone go/credentials storage-mode abstraction — the GTB config-key schema, per-subsystem resolution cascades, the doctor check, and the config masker."
date: 2026-07-18
tags: [component, security, credentials, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Credentials

The credential storage-mode abstraction has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/credentials`](https://gitlab.com/phpboyscout/go/credentials)
module**. Its full documentation — the three storage modes, the pluggable
`Backend` and its stub/keychain/custom implementations, `Probe`, the `Prompter`
seam, the `credtest` helper, the trust model, and the auditable keychain opt-out —
now lives at:

> **[credentials.go.phpboyscout.uk](https://credentials.go.phpboyscout.uk)**

API reference: **[pkg.go.dev/gitlab.com/phpboyscout/go/credentials](https://pkg.go.dev/gitlab.com/phpboyscout/go/credentials)**.
See the [migration note](../../reference/migration/v0.x-credentials-extracted.md)
for the module map and how to consume it directly.

go-tool-base imports the module directly (no adapter package); this page documents
only the **GTB-specific integration** layered on top — the config-key schema, the
per-subsystem resolution cascades, the doctor check, and the config masker. Those
are GTB concerns the config-agnostic module deliberately knows nothing about.

## Storage modes in GTB config

The module defines `ModeEnvVar` / `ModeKeychain` / `ModeLiteral`; GTB maps each to
a config-key shape:

| Mode | Value | What GTB writes to config | Where the secret lives |
|------|-------|---------------------------|-----------------------|
| `ModeEnvVar` | `"env"` | the **name** of an env var (`GITHUB_TOKEN`, `ANTHROPIC_API_KEY`, …) | process environment / shell profile / CI secret injection |
| `ModeKeychain` | `"keychain"` | a `<service>/<account>` reference | OS keychain — **only when the tool blank-imports `go/credentials/keychain`** |
| `ModeLiteral` | `"literal"` | the secret itself | the config file |

`ModeEnvVar` is the default and the only mode permitted under `CI=true`. The setup
wizards build their storage-mode selectors from `credentials.ModeChoices` (the
UI-agnostic replacement for the module's old huh helper) and render them with GTB's
own `huh` forms — the module carries no TUI dependency.

## Per-subsystem resolution cascades (GTB-owned)

The module resolves a `Backend` entry; GTB owns the **config-key precedence** each
subsystem walks:

| Subsystem | Resolution order |
|-----------|------------------|
| `pkg/chat` | direct → `<provider>.api.env` (ref) → `<provider>.api.keychain` (lookup) → `<provider>.api.key` (literal) → well-known env fallback |
| `pkg/vcs` | `auth.env` → `auth.keychain` → `auth.value` → fallback env (GitHub/GitLab/Gitea/direct) |
| `pkg/vcs/bitbucket` | `bitbucket.<field>.env` → shared `bitbucket.keychain` JSON blob (`{username, app_password}`) → literals → well-known env. A corrupt blob aborts resolution rather than falling through to stale literals. |

## Consumers

| Subsystem | Relationship to `go/credentials` |
|-----------|----------------------------------|
| `pkg/setup/ai` | Storage-mode selector via `ModeChoices` gated on `Probe`; the chosen mode decides whether `<provider>.api.env`, `.keychain`, or `.key` is written. Keychain mode stores the secret via `credentials.Store` — never in config. |
| `pkg/setup/github` | CI refusal for `ModeLiteral`; OAuth device flow with a manual-PAT fallback on headless hosts. |
| `pkg/setup/bitbucket` | Dual-credential model; keychain mode serialises `{username, app_password}` into one JSON-blob entry. |
| `pkg/cmd/config` | `migrate-credentials` moves literals to env/keychain; the config masker renders literal secrets as `****<tail>`. |
| `pkg/cmd/doctor` | `credentials.no-literal` check warns when any `ModeLiteral`-style value is present. |

## Activating the keychain backend

GTB itself blank-imports the module's keychain subpackage in `cmd/gtb/keychain.go`:

```go
import _ "gitlab.com/phpboyscout/go/credentials/keychain"
```

Deleting that one file and rebuilding produces a keychain-free `gtb` binary (the
linker drops go-keyring). Scaffolded tools get the same file via `gtb generate`
(the `keychain` feature). The full opt-out mechanics and SBOM verification are on
the microsite: [the keychain opt-out](https://credentials.go.phpboyscout.uk/explanation/keychain-opt-out/).

## Related

- **Configuration:** [Configure credentials](../../how-to/configure-credentials.md),
  [Migrate literal credentials](../../how-to/migrate-literal-credentials.md)
- **Module docs:** [credentials.go.phpboyscout.uk](https://credentials.go.phpboyscout.uk)
- **Trust model / spec:** [`2026-04-02-credential-storage-hardening.md`](../../development/specs/2026-04-02-credential-storage-hardening.md)
