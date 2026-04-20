---
title: "Credentials — Storage Mode Taxonomy"
description: "pkg/credentials describes how GTB-backed tools persist user-supplied secrets (AI API keys, VCS tokens, Bitbucket app passwords). The setup wizard, config masking, doctor checks, and runtime resolvers reason about Mode values from this package."
date: 2026-04-20
tags: [component, security, credentials, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Credentials — Storage Mode Taxonomy

`pkg/credentials` is the shared taxonomy for how GTB — and tools built on GTB — persist user-supplied secrets. It defines three storage modes, a keychain-capability probe, and two sentinel errors. Consumers include the interactive setup wizards (`pkg/setup/ai`, `pkg/setup/github`), the runtime credential resolvers (`pkg/chat`, `pkg/vcs`), the doctor subsystem, and the config masker.

The package deliberately contains no external dependencies in its default build. A Phase-2 follow-up will add `github.com/zalando/go-keyring` behind the `keychain` build tag.

## Storage Modes

Three `Mode` values are supported:

| Mode | Value | What gets written to config | Where the secret lives |
|------|-------|-----------------------------|-----------------------|
| `ModeEnvVar` | `"env"` | The **name** of an env var (`GITHUB_TOKEN`, `ANTHROPIC_API_KEY`, …) | Process environment, shell profile, or CI secret injection |
| `ModeKeychain` | `"keychain"` | A `<service>/<account>` reference | OS keychain (macOS Keychain, Linux libsecret, Windows Credential Manager) — **only available with `-tags keychain`** |
| `ModeLiteral` | `"literal"` | The secret itself | The config file |

`ModeEnvVar` is the recommended default and the only mode permitted under `CI=true`. `ModeLiteral` is supported for backward compatibility and throwaway environments; the setup wizard refuses it under CI and the doctor `credentials.no-literal` check warns on its presence.

## API

```go
import "github.com/phpboyscout/go-tool-base/pkg/credentials"

// Which modes can this build offer to the user?
modes := credentials.AvailableModes()
// -> default build: [ModeEnvVar, ModeLiteral]
// -> built with -tags keychain: [ModeEnvVar, ModeKeychain, ModeLiteral]

// Is keychain compiled in?
if credentials.KeychainAvailable() {
    // show keychain option in a setup UI
}

// Attempt a keychain operation — always fails with ErrCredentialUnsupported
// in the default build. Phase 2 wires these to go-keyring behind -tags keychain.
err := credentials.Store("mytool", "github.auth", secret)
token, err := credentials.Retrieve("mytool", "github.auth")
err = credentials.Delete("mytool", "github.auth")
```

The returned slice from `AvailableModes` is always ordered: env-var first, keychain (if available) next, literal last. Callers that present modes to the user can render them in this order without additional sorting.

### Sentinel Errors

| Error | Meaning |
|-------|---------|
| `ErrCredentialUnsupported` | Keychain operations in builds without the `keychain` tag; resolvers fall through to the next step. |
| `ErrCredentialNotFound` | Keychain is available but no entry exists for the given `<service>/<account>` pair; lets resolvers distinguish "missing" from "unavailable". |

Both wrap cleanly with `errors.Is` / `errors.As` and neither embeds credential material in its message.

## Build Tags

The default GTB build compiles `keychain_stub.go`, which returns `ErrCredentialUnsupported` from every keychain operation and reports `KeychainAvailable() == false`. Tool authors who want OS keychain integration opt in explicitly:

```bash
# Default build: no keychain dependency, no CGO on Linux
go build ./cmd/mytool

# Keychain-enabled build: links go-keyring (CGO on Linux via libsecret)
go build -tags keychain ./cmd/mytool
```

The keychain variant adds a Linux runtime dependency on libsecret/D-Bus, so tools that cross-compile widely or ship CGO-disabled binaries should leave the default off and document the keychain build as an optional variant.

## Consumers

| Subsystem | Relationship to `pkg/credentials` |
|-----------|-----------------------------------|
| `pkg/setup/ai` | Storage-mode selector presented in the wizard uses `AvailableModes()`; the chosen mode decides which config key (`<provider>.api.env` vs `<provider>.api.key`) is written. |
| `pkg/setup/github` | CI refusal for `ModeLiteral`; falls back to manual PAT entry when the OAuth flow cannot launch a browser. |
| `pkg/chat` | `resolveAPIKey` checks `<provider>.api.env` (ref) before `<provider>.api.key` (literal). Env-aware via Viper's `AutomaticEnv`. |
| `pkg/vcs/bitbucket` | `resolveCredentials` reads `bitbucket.username.env` / `bitbucket.app_password.env` before literals. |
| `pkg/cmd/doctor` | `credentials.no-literal` check scans for `ModeLiteral`-style config values and warns. |
| `pkg/cmd/config` | The masker recognises `auth`/`username`/`password`/`api` mid-path segments so `config get`/`config list` render literal secrets as `****<tail>`. |

## Trust Model

| Deployment | Recommended mode |
|-----------|-------------------|
| Local dev | Env-var reference (shell profile, `direnv`) or keychain |
| CI/CD pipelines | Env-var reference, populated by the CI platform's secret injection |
| Containerised / Kubernetes | External secret injection (Kubernetes Secrets, CSI) mounted as env vars |
| Throwaway / air-gapped | Literal value in config, accepting the plaintext-on-disk risk |

Full trust-model guidance is in [`docs/development/security-decisions.md`](../development/security-decisions.md#h-1-2026-04-02-audit-plaintext-credentials-in-config-files).

## Spec and Status

Package added in Phase 1 of [`2026-04-02-credential-storage-hardening.md`](../development/specs/2026-04-02-credential-storage-hardening.md). Phase 2 adds the keychain implementation; Phase 3 adds the `config migrate-credentials` command and the GitHub OAuth+display-once flow.
