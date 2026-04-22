---
title: "Credentials — Storage Mode Taxonomy"
description: "pkg/credentials describes how GTB-backed tools persist user-supplied secrets (AI API keys, VCS tokens, Bitbucket app passwords). The setup wizard, config masking, doctor checks, and runtime resolvers reason about Mode values from this package."
date: 2026-04-20
tags: [component, security, credentials, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Credentials — Storage Mode Taxonomy

`pkg/credentials` is the shared taxonomy for how GTB — and tools built on GTB — persist user-supplied secrets. It defines three storage modes, a [`Backend`](#backend-interface) interface, a capability check (`KeychainAvailable`), a live-reachability probe (`Probe`), and two sentinel errors. Consumers include the interactive setup wizards (`pkg/setup/ai`, `pkg/setup/github`), the runtime credential resolvers (`pkg/chat`, `pkg/vcs`), the doctor subsystem, and the config masker.

The OS-keychain implementation lives in a dedicated subpackage (`github.com/phpboyscout/go-tool-base/pkg/credentials/keychain`). Downstream tools opt in by blank-importing that package from their `cmd/*/main`; regulated or air-gapped consumers omit the import, run with the stub backend, and Go's linker dead-code elimination keeps `go-keyring`, `godbus`, and `wincred` out of their linked binary. SBOM tools that inspect the compiled artefact (syft, cyclonedx-gomod on the linked binary) see a clean dependency surface in that case.

## Storage Modes

Three `Mode` values are supported:

| Mode | Value | What gets written to config | Where the secret lives |
|------|-------|-----------------------------|-----------------------|
| `ModeEnvVar` | `"env"` | The **name** of an env var (`GITHUB_TOKEN`, `ANTHROPIC_API_KEY`, …) | Process environment, shell profile, or CI secret injection |
| `ModeKeychain` | `"keychain"` | A `<service>/<account>` reference | OS keychain (macOS Keychain, Linux Secret Service, Windows Credential Manager) — **only available when the `pkg/credentials/keychain` subpackage is imported** |
| `ModeLiteral` | `"literal"` | The secret itself | The config file |

`ModeEnvVar` is the recommended default and the only mode permitted under `CI=true`. `ModeLiteral` is supported for backward compatibility and throwaway environments; the setup wizard refuses it under CI and the doctor `credentials.no-literal` check warns on its presence.

## API

```go
import (
    "context"

    "github.com/phpboyscout/go-tool-base/pkg/credentials"
)

// Which modes can this build offer to the user?
modes := credentials.AvailableModes()
// -> without keychain subpackage imported: [ModeEnvVar, ModeLiteral]
// -> with keychain subpackage imported:   [ModeEnvVar, ModeKeychain, ModeLiteral]

// Is a keychain-capable backend registered?
if credentials.KeychainAvailable() {
    // ModeKeychain is a possible option — but still gate the UI
    // on Probe() before offering it, because a headless Linux host
    // without a Secret Service provider or a locked macOS keychain
    // will reject writes at use time.
}

// Is the keychain reachable right now? Performs a canary
// Set→Get→Delete round-trip. Short-circuits to false when the stub
// backend is installed, so callers can use Probe alone as the gate.
// Pass a bounded context to guard against remote backends that hang.
ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
defer cancel()
if credentials.Probe(ctx) {
    // offer ModeKeychain in the setup UI
}

// Backend operations. Fail with ErrCredentialUnsupported under the
// stub backend; wired to go-keyring when the keychain subpackage is
// imported.
err := credentials.Store(ctx, "mytool", "github.auth", secret)
token, err := credentials.Retrieve(ctx, "mytool", "github.auth")
err = credentials.Delete(ctx, "mytool", "github.auth")
```

The returned slice from `AvailableModes` is always ordered: env-var first, keychain (if available) next, literal last. Callers that present modes to the user can render them in this order without additional sorting. `Probe` is idempotent and safe to call from any goroutine.

### Backend interface

```go
type Backend interface {
    Store(ctx context.Context, service, account, secret string) error
    Retrieve(ctx context.Context, service, account string) (string, error) // must return ErrCredentialNotFound on a clean miss
    Delete(ctx context.Context, service, account string) error             // idempotent
    Available() bool
}

// Swap the active backend — typically during main() init.
credentials.RegisterBackend(myBackend)
```

Every method takes a `context.Context` so remote-store backends (Hashicorp Vault, AWS SSM, 1Password Connect) honour deadlines and cancellation. OS-keychain backends accept the context for interface uniformity but don't propagate it — platform APIs (Keychain Services, Secret Service, Credential Manager) don't expose cancellation. See [docs/how-to/custom-credential-backend.md](../how-to/custom-credential-backend.md) for a worked example implementing a custom backend.

The zero-dep default backend is `stubBackend`; every call returns `ErrCredentialUnsupported`. `credtest.MemoryBackend` (in `pkg/credentials/credtest`) is an in-process implementation useful for unit tests of resolvers and setup flows without touching the host keychain.

### Sentinel Errors

| Error | Meaning |
|-------|---------|
| `ErrCredentialUnsupported` | No keychain-capable backend is registered. Resolvers fall through to the next step. |
| `ErrCredentialNotFound` | Backend is present but no entry exists for the given `<service>/<account>` pair; lets resolvers distinguish "missing" from "unavailable". |

Both wrap cleanly with `errors.Is` / `errors.As` and neither embeds credential material in its message.

## Activating the keychain backend

To activate OS keychain support in a tool built on GTB:

```go
// cmd/mytool/main.go
import (
    _ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"  // registers Backend at init
    // …
)
```

The blank import runs the subpackage's `init()`, which calls `credentials.RegisterBackend` with a `go-keyring`-backed `Backend`. From that point on, `KeychainAvailable()` reports true, `AvailableModes()` includes `ModeKeychain`, and `Probe()` performs its live round-trip.

Tools that want to strip keychain support from a regulated build remove the blank import (or put it in a dedicated file like `cmd/mytool/keychain.go` that's easy to delete per build). With no import, Go's linker dead-code elimination keeps `go-keyring`, `godbus`, and `wincred` out of the binary — nothing reaches a D-Bus session bus, and the SBOM of the linked artefact is clean. The same mechanism gates keychain in `cmd/gtb` itself: deleting `cmd/gtb/keychain.go` and rebuilding produces a keychain-free `gtb` binary.

Note: the `go-keyring` dep chain is listed in the root module's `go.mod` as `// indirect` because `cmd/gtb` uses it by default. A binary-level SBOM remains the source of truth for what's actually linked into a specific artefact — source-level SBOMs generated from `go.sum` alone will show the full chain and require filtering against build-graph reachability.

## Consumers

| Subsystem | Relationship to `pkg/credentials` |
|-----------|-----------------------------------|
| `pkg/setup/ai` | Storage-mode selector uses `AvailableModes()` gated on `Probe()`; the chosen mode decides whether `<provider>.api.env`, `<provider>.api.keychain`, or `<provider>.api.key` is written. Keychain mode also writes the secret via `credentials.Store` — it never hits the config file. |
| `pkg/setup/github` | CI refusal for `ModeLiteral`; falls back to manual PAT entry when the OAuth flow cannot launch a browser. |
| `pkg/chat` | `resolveAPIKey` walks five steps: direct → `<provider>.api.env` (ref) → `<provider>.api.keychain` (lookup) → `<provider>.api.key` (literal) → well-known env fallback. Env-aware via Viper's `AutomaticEnv`. |
| `pkg/vcs` | `ResolveToken` walks `auth.env` → `auth.keychain` → `auth.value` → fallback env. Used by GitHub, GitLab, Gitea, and the direct provider. |
| `pkg/vcs/bitbucket` | `resolveCredentials` walks `bitbucket.<field>.env` → shared `bitbucket.keychain` JSON blob (`{username, app_password}`) → literals → well-known env. A corrupt keychain blob aborts resolution rather than silently falling back to a stale literal. |
| `pkg/cmd/doctor` | `credentials.no-literal` check scans for `ModeLiteral`-style config values and warns. |
| `pkg/cmd/config` | The masker recognises `auth`/`username`/`password`/`api` mid-path segments so `config get`/`config list` render literal secrets as `****<tail>`. |

## Trust Model

| Deployment | Recommended mode |
|-----------|-------------------|
| Local dev | Env-var reference (shell profile, `direnv`) or keychain |
| CI/CD pipelines | Env-var reference, populated by the CI platform's secret injection |
| Containerised / Kubernetes | External secret injection (Kubernetes Secrets, CSI) mounted as env vars |
| Throwaway / air-gapped | Literal value in config, accepting the plaintext-on-disk risk |
| Regulated / compliance-audited | Env-var reference only; do **not** import `pkg/credentials/keychain` in the tool's `main` |

Full trust-model guidance is in [`docs/development/security-decisions.md`](../development/security-decisions.md#h-1-2026-04-02-audit-plaintext-credentials-in-config-files).

## Spec and Status

Phases 1 and 2 of [`2026-04-02-credential-storage-hardening.md`](../development/specs/2026-04-02-credential-storage-hardening.md) are implemented: env-var reference as the default, keychain mode behind an opt-in subpackage import with `Probe()`-gated wizard UX, and Bitbucket JSON-blob support. Phase 3 (the `config migrate-credentials` command and the GitHub OAuth+display-once flow) is still pending.
