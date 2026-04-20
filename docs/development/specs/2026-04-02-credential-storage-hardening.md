---
title: "Credential Storage Hardening"
description: "Harden interactive setup to default to environment variable references for credential storage, with optional OS keychain integration as a pluggable local-development backend."
date: 2026-04-02
status: IN PROGRESS
tags:
  - specification
  - security
  - setup
  - credentials
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Credential Storage Hardening

Authors
:   Matt Cockayne

Date
:   02 April 2026

Status
:   APPROVED

---

## Overview

A security audit (H-1 in `docs/development/reports/security-audit-2026-04-02.md`) identified that API keys for AI providers and VCS tokens are stored as plaintext YAML in config files on disk. While GTB already supports an `auth.env` config key that references an environment variable name instead of a literal value, the interactive setup wizard (`pkg/setup/ai/`, `pkg/setup/github/`) defaults to storing the literal credential directly.

This specification proposes three changes:

1. **Default to environment variable references** during interactive setup, so the recommended secure path is the path of least resistance.
2. **Optional OS keychain integration** as a pluggable backend for local development, where setting environment variables is inconvenient.
3. **Documented trust model** that clarifies credential handling expectations across local dev, CI/CD, and containerised deployments.

### Relationship to Rejected "Secrets Manager / Vault Integration"

The `docs/development/feature-decisions.md` entry (31 March 2026) rejected a `SecretsProvider` interface with Vault/keychain/env-var implementations. That rejection stands -- GTB should not become a secrets management framework. This spec is narrower in scope:

- It changes the **interactive setup wizard's default behaviour**, not the config resolution chain.
- The keychain backend is a **local-development convenience** behind a build tag, not a production secrets abstraction.
- The `pkg/vcs/auth.go` resolution order (`auth.env` > `auth.value` > fallback env) is unchanged.
- No new provider interfaces or dependency-injection seams are added to `pkg/config/` or `pkg/props/`.

### Problem

The current interactive setup flow in both `pkg/setup/ai/ai.go` and `pkg/setup/github/github.go` collects credentials and writes them directly into the config:

```go
// pkg/setup/github/github.go:113
cfg.Set("github.auth.value", ghtoken)

// pkg/setup/ai/ai.go:246 (via providerConfigKey)
cfg.Set(keyPath, aiCfg.APIKey)
```

This means:
- A user running `init` for the first time writes plaintext secrets to `~/.toolname/config.yaml`.
- The `auth.env` approach exists but is not offered during interactive setup.
- Users must know about `auth.env` from documentation to use the secure path.
- Config file permissions are set to `0600` (good), but plaintext secrets in files remain a risk for backups, dotfile syncing, and shared workstations.

### Threat Model

| Threat | Vector | Impact |
|--------|--------|--------|
| Credential leak via config file | Backup software, dotfile sync (e.g. `chezmoi` commit to public repo), shoulder-surfing | Full provider access (AI API quota, GitHub repos, Bitbucket code) |
| Credential leak via log output | Error messages or debug logs that echo config values | Credentials captured in log aggregators, SIEMs, or support tickets |
| Shared workstation exposure | Multiple users on same machine, forgotten login session | Credentials used by unintended actor with system access |
| Compromised local account | Malware reads plaintext config and exfiltrates | Full provider access |
| Credential drift / stale keys | User rotates key externally but forgets to update config | Scripts break silently; user may paste a new literal to "fix" without removing old |
| Accidental commit of config to VCS | User commits `~/.tool/config.yaml` to a public repo | Key publicly exposed; provider revocation required within minutes |
| CI/CD misconfiguration | Tool running in CI writes a literal credential to an artefact | Credentials in build logs, artefact stores, or container images |

### Goals

- Make the env-var-reference path the **default** during interactive setup.
- Preserve backward compatibility — existing `auth.value` configs continue to work without migration.
- Provide an optional keychain backend for users who find env vars inconvenient in local development.
- Document the expected credential management model for each deployment context.
- **Guarantee that credentials are never emitted at log levels above DEBUG**, anywhere in the codebase.
- **Prevent literal storage in CI environments** — the wizard must refuse to write a literal value when `CI=true`, regardless of operator intent.
- **Provide a migration path** for users with existing literal credentials: a `doctor` check that warns, and a `config migrate-credentials` command that converts to env-var references.
- **Cover Bitbucket dual-credential pattern** (`username` + `app_password`) with the same three-mode selector.

### Non-Goals

- Replacing or deprecating `auth.value` — it remains a valid config option.
- Building a generic secrets provider interface or Vault integration.
- Encrypting the config file at rest. (An `age`/`sops` layer is a separate concern; see Future Considerations.)
- Changing the `pkg/vcs/auth.go` resolution order.
- Forcing any particular credential strategy on downstream tool authors.

---

## Design Decisions

**Env-var reference as the default path**: The setup wizard should present "store the name of an environment variable" as the primary option, with "store the literal value in config" as an explicit opt-in. This nudges users toward the secure path without removing the convenient path.

**Three-option credential storage selector**: During interactive setup, the user chooses from: (1) environment variable reference (default), (2) OS keychain (if available), (3) literal value in config file. This keeps the UX simple while surfacing all options.

**Keychain behind build tag**: OS keychain integration requires CGO on Linux (libsecret/D-Bus) and platform-specific APIs. This dependency is gated behind a `keychain` build tag so that the default CGO-disabled build is unaffected. The `go-keyring` library provides a cross-platform abstraction with macOS Keychain, Linux libsecret, and Windows Credential Manager support.

**Keychain stored as `auth.keychain` config key**: When the keychain backend is selected, the config stores `auth.keychain: "service/account"` instead of `auth.value`. The `ResolveToken` function in `pkg/vcs/auth.go` gains a new resolution step that checks `auth.keychain` between `auth.env` and `auth.value`.

**No changes to AI credential resolution**: The AI providers (`pkg/chat/`) resolve keys from config (`anthropic.api.key`, etc.) and environment variables (`ANTHROPIC_API_KEY`, etc.) independently of the VCS `auth.env`/`auth.value` pattern. This spec extends the AI setup to also support env-var-reference and keychain storage modes, writing `anthropic.api.env`, `openai.api.env`, `gemini.api.env` config keys alongside the existing literal key paths.

**Setup wizard detects CI automatically**: The existing `CI=true` detection in setup initialisers already skips interactive auth in CI. This spec adds a note in the wizard explaining that CI/CD environments should use native secret injection mechanisms.

---

## Public API

### Modified: `pkg/vcs/auth.go`

The `ResolveToken` resolution order gains a keychain step:

```go
// ResolveToken resolves an authentication token from a config subtree.
// Resolution order:
//  1. cfg.auth.env      -- name of an environment variable to read
//  2. cfg.auth.keychain -- keychain service/account identifier (requires keychain build tag)
//  3. cfg.auth.value    -- literal token value stored in config
//  4. fallbackEnv       -- a well-known environment variable (pass "" to skip)
func ResolveToken(cfg config.Containable, fallbackEnv string) string
```

When built without the `keychain` tag, step 2 is a no-op. The function signature does not change.

### New: `pkg/credentials/` package

A small package providing credential storage mode constants and a helper for the setup wizard:

```go
package credentials

// Mode represents how a credential is stored.
type Mode string

const (
    // ModeEnvVar stores the name of an environment variable containing the credential.
    ModeEnvVar Mode = "env"
    // ModeKeychain stores the credential in the OS keychain (requires keychain build tag).
    ModeKeychain Mode = "keychain"
    // ModeLiteral stores the credential value directly in the config file.
    ModeLiteral Mode = "literal"
)

// AvailableModes returns the credential storage modes available in this build.
// ModeKeychain is only included when the keychain build tag is active.
func AvailableModes() []Mode

// KeychainAvailable reports whether OS keychain support was compiled in.
func KeychainAvailable() bool
```

This package contains no external dependencies in its default build. The keychain-tagged files import `github.com/zalando/go-keyring` (pure Go on macOS/Windows, CGO on Linux).

### Modified: `pkg/setup/ai/ai.go`

The `AIConfig` struct and form flow are extended:

```go
type AIConfig struct {
    Provider      string
    APIKey        string
    ExistingKey   string
    StorageMode   credentials.Mode  // new: how to persist the credential
    EnvVarName    string            // new: env var name when StorageMode == ModeEnvVar
}
```

The key form gains a storage mode selector before the key input. When `ModeEnvVar` is selected, the form prompts for the environment variable name (with a sensible default like `ANTHROPIC_API_KEY`) instead of the literal key. When `ModeKeychain` is selected, the literal key is collected but stored via the keychain API rather than written to config.

### Modified: `pkg/setup/github/github.go`

The `configureAuth` method is updated to present the same three-option storage mode selector. When env-var mode is chosen, it writes `github.auth.env` instead of `github.auth.value`.

---

## Internal Implementation

### Setup Wizard Flow (AI example)

```
Stage 0: CI Detection
  if CI=true: print "Literal-mode credentials are refused in CI. Configure
                      <TOOL>_ANTHROPIC_API_KEY via your CI platform's
                      secret injection and re-run." then exit 1
  else proceed to Stage 1.

Stage 1: Select AI Provider
  [Claude (Anthropic) / OpenAI / Gemini (Google)]

Stage 2: Credential Storage Method
  > Store as environment variable reference (recommended)
    Store in OS keychain [only shown if keychain build tag active AND
                           keychain probe succeeds]
    Store literal value in config file [warn: plaintext on disk]

Stage 3a (env var): Environment Variable Name
  Default: ANTHROPIC_API_KEY
  Hint: "If you use multiple tools with conflicting keys, prefix with
         your tool name, e.g. MYTOOL_ANTHROPIC_API_KEY."
  Validation: ^[A-Z][A-Z0-9_]{0,63}$

Stage 3b (keychain): API Key
  [password input — terminal raw mode, no echo, cleared after read]
  -> stored via go-keyring with service=<toolname> account=anthropic.api
  -> config records: anthropic.api.keychain: "<toolname>/anthropic.api"
  -> API key variable is explicitly zeroed on function exit

Stage 3c (literal): API Key
  [password input]
  -> prompt: "This will write the key in plaintext to <path>. Continue?"
  -> on confirm, written to config as today
  -> config file permissions verified at 0600 after write
```

### Setup Wizard Flow (GitHub — OAuth + env-var interaction)

```
Stage 1: Credential Storage Method (same three options)

Stage 2a (env var):
  "We can run `gh auth login` to obtain a token for you. We will NOT
   write it to config — you will copy it to your shell profile instead."
  [yes / no / skip]

  If yes:
    run ghLogin() to get token
    display token in a protected input ONCE:
      "Copy this token and set ANTHROPIC_API_KEY in your shell profile.
       This prompt will not be shown again.
       Token: ghp_xxxxxxxxxxxxxxxxxxxx"
    await explicit confirmation ("I have saved the token") before clearing
    write config: github.auth.env: GITHUB_TOKEN
    zero the token variable immediately after confirmation

  If no:
    prompt for env var name only (user has token via other means)
    write config: github.auth.env: <chosen name>

Stage 2b (keychain):
  run ghLogin() -> token
  store under <toolname>/github.auth
  write config: github.auth.keychain: "<toolname>/github.auth"
  zero the token variable

Stage 2c (literal):
  run ghLogin() -> token
  write config: github.auth.value: <token>
  zero the token variable in this function; the config file is 0600
```

### Setup Wizard Flow (Bitbucket — dual credentials)

```
Stage 2a (env var):
  Prompt for two env var names (defaults BITBUCKET_USERNAME,
                                           BITBUCKET_APP_PASSWORD).
  Write: bitbucket.username.env, bitbucket.app_password.env

Stage 2b (keychain):
  Collect username and app_password.
  Serialise as JSON: {"username":"...","app_password":"..."}
  Store under <toolname>/bitbucket.auth.
  Write: bitbucket.keychain: "<toolname>/bitbucket.auth"

Stage 2c (literal):
  Collect both, write bitbucket.username and bitbucket.app_password.
```

### Config Output by Storage Mode

**Environment variable reference (default):**
```yaml
ai:
  provider: claude
anthropic:
  api:
    env: ANTHROPIC_API_KEY
```

**Keychain:**
```yaml
ai:
  provider: claude
anthropic:
  api:
    keychain: "gtb/mytool/anthropic.api.key"
```

**Literal (existing behaviour):**
```yaml
ai:
  provider: claude
anthropic:
  api:
    key: sk-ant-api03-...
```

### Keychain Integration (build-tagged)

File: `pkg/credentials/keychain_enabled.go` (build tag: `keychain`)

```go
//go:build keychain

package credentials

import "github.com/zalando/go-keyring"

func init() {
    keychainCompiled = true
}

// Store saves a credential to the OS keychain.
func Store(service, account, secret string) error {
    return keyring.Set(service, account, secret)
}

// Retrieve loads a credential from the OS keychain.
func Retrieve(service, account string) (string, error) {
    return keyring.Get(service, account)
}

// Delete removes a credential from the OS keychain.
func Delete(service, account string) error {
    return keyring.Delete(service, account)
}
```

File: `pkg/credentials/keychain_stub.go` (default, no build tag)

```go
//go:build !keychain

package credentials

import "github.com/cockroachdb/errors"

func init() {
    keychainCompiled = false
}

func Store(_, _, _ string) error {
    return errors.New("keychain support not compiled (build with -tags keychain)")
}

func Retrieve(_, _ string) (string, error) {
    return "", errors.New("keychain support not compiled (build with -tags keychain)")
}

func Delete(_, _ string) error {
    return errors.New("keychain support not compiled (build with -tags keychain)")
}
```

### Token Resolution Update

In `pkg/vcs/auth.go`, the `tokenFromConfig` function is extended. Note the careful use of `strings.TrimSpace` and empty-value fall-through — a key that is present but empty must not short-circuit the resolution chain:

```go
func tokenFromConfig(cfg config.Containable) string {
    if cfg == nil {
        return ""
    }

    // Priority 1: environment variable reference.
    // If auth.env is set but the referenced env var is unset or empty,
    // fall through rather than returning "" immediately.
    if envName := strings.TrimSpace(cfg.GetString("auth.env")); envName != "" {
        if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
            return token
        }
    }

    // Priority 2: OS keychain (returns ErrCredentialUnsupported without tag).
    if kcRef := strings.TrimSpace(cfg.GetString("auth.keychain")); kcRef != "" {
        service, account := parseKeychainRef(kcRef) // "svc/acct" -> ("svc","acct")
        if token, err := credentials.Retrieve(service, account); err == nil && token != "" {
            return token
        }
        // Errors are logged at DEBUG only — never at higher level, per R1/R2.
    }

    // Priority 3: literal value.
    if val := cfg.GetString("auth.value"); val != "" {
        return val
    }

    return ""
}
```

The AI provider credential resolution (`pkg/chat/claude.go`, `pkg/chat/openai.go`, `pkg/chat/gemini.go`) is updated similarly to check `<provider>.api.env` and `<provider>.api.keychain` before falling back to `<provider>.api.key`. These functions share a new helper in `pkg/chat/credentials.go`:

```go
// ResolveAPIKey resolves an AI provider API key from config using the same
// three-mode precedence as VCS tokens. The keyPrefix is the provider-specific
// config root, e.g. "anthropic.api".
func ResolveAPIKey(p *props.Props, cfg Config, keyPrefix string, envFallback string) string
```

### Bitbucket Dual-Credential Resolution

A new helper in `pkg/vcs/bitbucket/credentials.go` handles the dual-credential pattern:

```go
type BitbucketCredentials struct {
    Username    string
    AppPassword string
}

func ResolveBitbucketCredentials(cfg config.Containable) (BitbucketCredentials, error)
```

Resolution order per field is the same three-mode pattern (env > keychain > literal). For keychain mode, a single entry `<toolname>/bitbucket.auth` stores a JSON blob `{"username":"...","app_password":"..."}` — unmarshalled on retrieval. A corrupt or incomplete blob returns an error (does not fall through to literal).

### `config migrate-credentials` Command

A new `config migrate-credentials` subcommand guides users from literal mode to env-var mode:

```go
// cmd/config/migrate.go
//
// Usage: <tool> config migrate-credentials [--dry-run] [--target env|keychain]
//
// For each literal credential in the config:
//   1. Display the key path and a masked view of the current value.
//   2. Prompt for the target env var name (default: provider-standard).
//   3. Instruct the user to set the env var in their shell profile.
//   4. Wait for confirmation that the env var is set.
//   5. Verify the env var is set to the expected value.
//   6. Rewrite the config: remove auth.value, add auth.env.
//   7. Restrict the new config file to 0600.
//
// --dry-run prints the planned changes without modifying config.
// --target keychain routes step 2–5 through keychain storage instead.
```

The command is gated behind the same `setup.Register` registration pattern as other config subcommands. It is emitted by the generator as a default-enabled subcommand for new tools.

---

## Project Structure

### New Files

| File | Purpose |
|------|---------|
| `pkg/credentials/mode.go` | `Mode` type, constants, `AvailableModes()`, `KeychainAvailable()` |
| `pkg/credentials/keychain_enabled.go` | Build-tagged keychain implementation |
| `pkg/credentials/keychain_stub.go` | Stub for builds without keychain tag |
| `pkg/credentials/mode_test.go` | Unit tests for mode helpers |
| `pkg/credentials/keychain_test.go` | Unit tests for keychain (build-tagged) |

### Modified Files

| File | Change |
|------|--------|
| `pkg/vcs/auth.go` | Add `auth.keychain` resolution step |
| `pkg/vcs/auth_test.go` | Tests for keychain resolution path |
| `pkg/setup/ai/ai.go` | Storage mode selector in forms, env-var/keychain write paths |
| `pkg/setup/ai/ai_test.go` | Tests for new form stages and config output |
| `pkg/setup/github/github.go` | Storage mode selector, env-var write path |
| `pkg/setup/github/github_test.go` | Tests for new auth storage modes |
| `pkg/chat/claude.go` | Check `anthropic.api.env` and `anthropic.api.keychain` |
| `pkg/chat/openai.go` | Check `openai.api.env` and `openai.api.keychain` |
| `pkg/chat/gemini.go` | Check `gemini.api.env` and `gemini.api.keychain` |
| `pkg/chat/client.go` | Add `ConfigKeyClaudeEnv`, `ConfigKeyOpenAIEnv`, `ConfigKeyGeminiEnv` constants |

---

## Generator Impact

### Template Changes

The generator templates that scaffold `init` commands for new tools need updating:

- `internal/generator/templates/` files that reference AI or VCS init should include the storage mode selector in the generated setup flow.
- The default config asset template should include comments documenting `auth.env` as the recommended approach.
- No structural changes to the generation pipeline itself.

### Regeneration

Existing generated projects using `auth.value` are unaffected. The `regenerate` command will update init templates if the user accepts the diff.

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| Env var referenced but not set at runtime | `ResolveToken` falls through to next priority; no error. Caller decides if empty token is fatal. |
| Keychain not compiled but `auth.keychain` in config | `credentials.Retrieve` returns error; `tokenFromConfig` falls through to `auth.value`. Warning logged at DEBUG level. |
| Keychain compiled but OS keychain unavailable (e.g. headless Linux without D-Bus) | `go-keyring` returns a descriptive error; `tokenFromConfig` falls through. Setup wizard detects this and disables the keychain option. |
| User selects env-var mode but enters empty var name | Form validation rejects empty input; re-prompts. |
| User selects env-var mode; variable exists but is empty | Same as "env var not set" -- falls through to next priority. |
| Keychain store fails during setup | Error surfaced to user with hint: "Keychain storage failed. You can use environment variable mode instead." |

All errors use `cockroachdb/errors` with `WithHint` for user-facing guidance.

---

## Testing Strategy

### Unit Tests

| Area | Tests |
|------|-------|
| `pkg/credentials/` | `AvailableModes` returns correct set per build tag; `KeychainAvailable` accuracy |
| `pkg/credentials/` (keychain tag) | `Store`/`Retrieve`/`Delete` round-trip with mock keyring; `Retrieve` returns `ErrCredentialNotFound` for missing entries; `Retrieve` returns wrapped error for unavailable keychain |
| `pkg/credentials/` (no tag) | Stub functions return `ErrCredentialUnsupported` |
| `pkg/vcs/auth.go` | Existing tests pass unchanged; new tests for `auth.keychain` priority |
| `pkg/vcs/auth.go` | `auth.env` > `auth.keychain` > `auth.value` precedence verified |
| `pkg/vcs/auth.go` | Empty `auth.env` value falls through (does not short-circuit); empty env var value falls through |
| `pkg/vcs/auth.go` | Whitespace-only values are treated as empty |
| `pkg/vcs/auth.go` | **R2**: Errors do not include the credential value in their message |
| `pkg/vcs/bitbucket/credentials.go` | Dual-credential resolution; env-var mode requires both vars; keychain JSON blob round-trip; corrupt JSON aborts (no fall-through) |
| `pkg/setup/ai/` | Form injects storage mode; env-var mode writes `<provider>.api.env`; literal mode writes `<provider>.api.key`; keychain mode writes `<provider>.api.keychain` |
| `pkg/setup/ai/` | **R5**: `CI=true` + literal mode → exit 1 with actionable error |
| `pkg/setup/ai/` | Literal mode write verifies file mode is `0600` after write |
| `pkg/setup/github/` | Env-var mode writes `github.auth.env`; literal mode writes `github.auth.value` |
| `pkg/setup/github/` | Env-var mode after OAuth: token is zeroed after display; config receives env reference only |
| `pkg/chat/claude.go` | Resolves from `anthropic.api.env` before `anthropic.api.keychain` before `anthropic.api.key` |
| `pkg/chat/openai.go` | Same precedence for OpenAI |
| `pkg/chat/gemini.go` | Same precedence for Gemini |
| `pkg/cmd/doctor/` | **R6**: `credentials.no-literal` check fires when any literal credential is present |
| `pkg/cmd/config/migrate.go` | Migration command: dry-run prints plan; env-var confirmation verifies value; keychain target stores correctly |

### Security-Specific Tests

| Test | Purpose |
|------|---------|
| `TestCredentialsNotLogged` | Configure each provider with a recognisable token value; run the full `init` → `update` → `doctor` flow with a capturing logger at DEBUG. Assert the token string appears only in DEBUG log entries tagged `sensitive=true`, nowhere else. |
| `TestSensitiveConfigMasking` | `pkg/cmd/config/sensitive.go` masks all new credential keys (`*.auth.keychain`, `*.api.env`, `bitbucket.username`, etc.). |
| `TestConfigFilePermissionsEnforced` | After every setup path, config file is `0600` (verified via `Fs.Stat`). |
| `TestKeychainErrorDoesNotLeakValue` | `Retrieve` errors do not include any portion of a credential. |
| `TestCIRefusesLiteralMode` | With `CI=true`, setup wizard refuses literal-mode selection with exit 1. |
| `TestMigrateCommandDryRun` | Dry-run does not mutate config; verify via file-mtime stability. |
| `TestDoctorCredentialsCheck` | `doctor` finds all literal-credential patterns across AI, GitHub, GitLab, Bitbucket, Gitea config namespaces. |

### Integration Tests

| Tag | Scope |
|-----|-------|
| `INT_TEST_CREDENTIALS` | Keychain round-trip on supported platforms (macOS, Linux with libsecret). Skipped when `DBUS_SESSION_BUS_ADDRESS` is unset. |
| `INT_TEST_SETUP` | Full `init` flow verifying config file output per storage mode, including post-setup `doctor` check pass. |
| `INT_TEST_E2E_CLI` | End-to-end BDD scenarios covering CI refusal, migration command, and keychain-enabled build. |

### E2E / BDD

A Gherkin scenario for the `init` command should verify the storage mode selector is presented and that the resulting config file contains the expected key structure. Example:

```gherkin
Feature: Credential storage during setup

  Scenario: Default setup stores environment variable reference
    Given a clean config directory
    When I run "init ai" and select "Claude" with env-var storage mode "ANTHROPIC_API_KEY"
    Then the config file should contain key "anthropic.api.env" with value "ANTHROPIC_API_KEY"
    And the config file should not contain key "anthropic.api.key"

  Scenario: Literal storage mode writes key directly
    Given a clean config directory
    When I run "init ai" and select "Claude" with literal storage mode and key "sk-test-key"
    Then the config file should contain key "anthropic.api.key" with value "sk-test-key"
    And the config file should not contain key "anthropic.api.env"
```

---

## Trust Model & Deployment Guidance

This section documents the expected credential management approach for each deployment context. It should be included in `docs/components/setup/` and referenced from the setup wizard output.

### Local Development (workstation)

**Recommended:** Environment variable reference (`auth.env`) or OS keychain (`auth.keychain`).

- Set credentials in shell profile (`~/.zshrc`, `~/.bashrc`) or a tool like `direnv`.
- Alternatively, use the OS keychain for credentials that are inconvenient to manage as env vars.
- The config file contains only a reference, not the secret itself.
- Config files can be safely committed to dotfile repos or backed up.

### CI/CD Pipelines

**Recommended:** Environment variables injected by the CI platform.

- GitHub Actions: `${{ secrets.ANTHROPIC_API_KEY }}`
- GitLab CI: `$ANTHROPIC_API_KEY` from CI/CD variables
- The `CI=true` detection in setup initialisers skips interactive auth automatically.
- Config should use `auth.env` pointing to the CI-injected variable name.
- No GTB-specific secrets management is needed -- the CI platform handles it.

### Containerised / Kubernetes Deployments

**Recommended:** External secret injection. GTB is not involved in credential management.

- Kubernetes Secrets mounted as env vars or files
- CSI secret store drivers (Vault, AWS Secrets Manager, Azure Key Vault)
- Config files baked into images should contain only `auth.env` references, never literal values.
- The keychain backend is not applicable in containerised environments (no D-Bus, no Keychain).

### Literal Value (legacy / quick-start)

**Acceptable for:** Throwaway environments, local experimentation, air-gapped systems.

- Config file must have `0600` permissions (already enforced by setup).
- User accepts the risk of plaintext secrets on disk.
- The setup wizard displays a warning when this option is selected.

---

## Migration & Compatibility

### Backward Compatibility

- Existing configs with `auth.value` or `<provider>.api.key` continue to work without any changes.
- The resolution order places `auth.env` and `auth.keychain` at higher priority, so adding an env var or keychain entry for a credential that also has a literal value will use the env var / keychain transparently.
- No config migration is required. Users can adopt the new storage modes incrementally.

### Config File Format

No breaking changes to config structure. New keys (`auth.keychain`, `<provider>.api.env`, `<provider>.api.keychain`) are additive.

### API Stability

- `pkg/vcs/auth.ResolveToken` signature is unchanged -- the keychain step is an internal implementation detail.
- `pkg/credentials/` is a new package, starting at Beta stability tier.
- `pkg/setup/ai/AIConfig` struct gains new fields -- this is an internal type used only within the setup package, not part of the stable API surface.

---

## Non-Functional Requirements

### Testing & Quality Gates

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 90 % for `pkg/credentials/` and `pkg/chat/credentials.go` |
| Branch coverage | ≥ 90 % for resolution precedence logic (`tokenFromConfig`, `ResolveAPIKey`, Bitbucket dual-cred resolver) |
| Race detector | `go test -race ./pkg/credentials/... ./pkg/setup/... ./pkg/vcs/... ./pkg/chat/...` must pass |
| Fuzz testing | Required for `parseKeychainRef` (small but easy to fuzz); optional for JSON-blob unmarshal (covered by standard library) |
| Security-specific tests | R1–R6 tests from [Security Requirements](#security-requirements) are non-optional and gate CI |
| `TestCredentialsNotLogged` | Must assert no recognisable token bytes appear in any log entry above DEBUG across the full `init` → `update` → `doctor` flow |
| Integration tests | `INT_TEST_CREDENTIALS=1` covers keychain round-trip on macOS and Linux-with-libsecret; skipped when `DBUS_SESSION_BUS_ADDRESS` is unset |
| Build matrix | The default build (no tag) AND the `-tags keychain` build must both compile and pass tests |
| CI refusal test | `TestCIRefusesLiteralMode` runs with `CI=true` in the test environment to confirm literal-mode selection exits non-zero |
| Golangci-lint | No new findings; no `//nolint` directives |
| E2E / BDD | Gherkin scenarios covering: fresh setup with env-var default, CI refusal of literal, OAuth+env-var flow, Bitbucket dual-cred, keychain round-trip (gated by build tag), `doctor` fires on literal, `migrate-credentials` dry-run and apply |
| Migration reversibility | Round-trip test: start from literal config → run `migrate-credentials --target env` → verify config is env-var form → verify tool reads credentials correctly |

### Documentation Deliverables

| Artefact | Scope |
|----------|-------|
| `docs/components/credentials.md` | New. Package reference: `Mode` enum, `Store`/`Retrieve`/`Delete`, keychain service naming, build-tag story. |
| `docs/components/setup.md` | Update. Credential storage modes in the setup wizard; link to `credentials.md`. |
| `docs/how-to/configure-credentials.md` | New. End-user guide: env-var setup, keychain setup (with build instructions), choosing between modes, multi-tool prefix strategy. |
| `docs/how-to/migrate-literal-credentials.md` | New. Step-by-step migration using the `config migrate-credentials` command, plus manual migration. |
| `docs/about/security.md` | Update. Credential handling trust model: local dev, CI/CD, containers; what GTB does and does not protect against. |
| `docs/migration/<version>-credential-storage.md` | New. Describes the setup-wizard default change, new config keys, CI behaviour change, doctor check. |
| Package doc comments | New/updated in `pkg/credentials/`, `pkg/vcs/auth.go`, `pkg/chat/credentials.go`, `pkg/vcs/bitbucket/credentials.go`. Each package doc explicitly states: (a) resolution precedence, (b) which fields are credentials and must not be logged, (c) build-tag behaviour where applicable. |
| CLAUDE.md | Update. Add to "Testing" and "Architecture" sections: "Credentials must never be logged above DEBUG; configure via `auth.env` / `auth.keychain` / `<provider>.api.env`; literal values are a last resort and are refused in CI." |
| Trust Model section | Already in this spec (lines referencing Local Development / CI/CD / Containerised). Must be mirrored to `docs/about/security.md` so it is discoverable outside the spec. |
| BDD feature files | New in `features/cli/credentials.feature`. Living documentation for the setup and migration flows. |

### Observability

| Event | Level | Fields |
|-------|-------|--------|
| Credential resolved from env var | DEBUG | `source=env`, `key=<env_var_name>`; never the value |
| Credential resolved from keychain | DEBUG | `source=keychain`, `service`, `account`; never the value |
| Credential resolved from literal config | DEBUG | `source=literal`, `key`; never the value |
| Credential not found (all modes) | DEBUG when optional, ERROR when required | `key`, `attempted_sources`; never a partial value |
| Keychain unavailable (e.g. D-Bus missing) | DEBUG | `error_kind=unavailable` (NOT the OS error detail, which may include paths) |
| Keychain entry missing | DEBUG | `error_kind=not_found`, `service`, `account` |
| Keychain entry corrupt (e.g. invalid JSON for Bitbucket blob) | ERROR (fatal) | `service`, `account`; hint text guides re-running setup |
| Literal mode selected in CI | ERROR (fatal, exit 1) | Hint text directs to CI secret injection |
| `doctor credentials.no-literal` | WARN | List of offending config keys (not values); hint text names the migration command |
| Migration command — plan step | INFO | Keys to migrate, target mode; never the value |
| Migration command — env var verification | DEBUG | `key`, `env_var_name`, `verified=true|false`; never the value |

**Redaction invariants**:
1. Credential values never appear at or above INFO level. DEBUG entries tagged `sensitive=true` are the only exception and are stripped from any telemetry export.
2. Error wrappers constructed in this subsystem must not embed the credential value in their message or detail.
3. The config-masking in `pkg/cmd/config/sensitive.go` is extended to every key pattern introduced by this spec.
4. Keychain library errors are wrapped before surfacing — we never pass through the raw `keyring` error, which can contain paths and other context.

### Performance Bounds

| Metric | Bound | Notes |
|--------|-------|-------|
| `ResolveToken` / `ResolveAPIKey` | ≤ 1 ms for env-var and literal paths | Simple string lookups |
| Keychain `Retrieve` | ≤ 50 ms typical; ≤ 500 ms worst case (D-Bus round-trip) | No retries on failure — fall through |
| Memory | O(1) beyond the credential string itself | Retrieved values zeroed on function exit where the language permits |
| Wizard latency | No new latency on non-interactive paths | Interactive paths gain the storage-mode selector (one extra prompt) |
| Doctor check | ≤ 10 ms for full credential audit | Scans the loaded config, no filesystem or network I/O |

### Security Invariants

Summarised from the [Security Requirements](#security-requirements) and [Resolved Decisions](#resolved-decisions):

1. **R1**: Credential values are never emitted at or above INFO (enforced by `TestCredentialsNotLogged`).
2. **R2**: Error messages redact credential values.
3. **R3**: Keychain errors distinguish missing, unavailable, and corrupt cases.
4. **R4**: Config files containing any credential are `0600`.
5. **R5**: Literal mode is refused in CI.
6. **R6**: A `doctor` check surfaces literal credentials.
7. Interactive token display in GitHub OAuth+env-var flow is one-shot and requires explicit user acknowledgement before clearing.
8. Tokens collected during OAuth flow are zeroed in process memory as soon as they have been displayed or written.
9. Bitbucket dual-credentials are stored as a JSON blob in keychain mode; corrupt blobs abort rather than falling through.
10. No keychain code is linked in the default build; a clear error guides users who configure keychain without the `keychain` build tag.

---

---

## Future Considerations

- **Credential rotation reminders**: The keychain backend could track credential age and warn when keys are older than a configurable threshold.
- **`doctor` command credential audit**: The `doctor` command could check for literal values in config and suggest migration to env-var or keychain mode.
- **`config migrate-credentials` subcommand**: A one-shot command to convert existing `auth.value` entries to `auth.env` references, with interactive guidance on setting the corresponding environment variables.
- **Encrypted config file**: A full-file encryption layer (e.g. `age`/`sops`) is a separate concern that could complement this work but is out of scope.

---

## Implementation Phases

### Phase 1: Env-Var Default in Setup Wizard + Security Invariants

**Scope:** Change the interactive setup to default to env-var references and establish the security requirements. No new dependencies.

1. Add `pkg/credentials/mode.go` with `Mode` type, constants, and sentinel errors (`ErrCredentialNotFound`, `ErrCredentialUnsupported`).
2. Add stub `pkg/credentials/keychain_stub.go` returning `ErrCredentialUnsupported`.
3. Modify `pkg/setup/ai/ai.go` to present storage mode selector; default to `ModeEnvVar`; CI refuses literal (R5).
4. Modify `pkg/setup/github/github.go` to present storage mode selector; OAuth+env-var flow per Resolved Decision #4; token zeroed after display.
5. Modify `pkg/vcs/bitbucket/credentials.go` for dual-credential pattern.
6. Add `<provider>.api.env` config key support to `pkg/chat/` (new `ResolveAPIKey` helper).
7. Extend `pkg/cmd/config/sensitive.go` to mask all new credential key patterns.
8. Add `pkg/cmd/doctor/credentials.go` check `credentials.no-literal` (R6).
9. Add `TestCredentialsNotLogged` end-to-end test (R1).
10. Add trust model documentation to `docs/components/setup/`.

### Phase 2: Keychain Integration

**Scope:** Add optional OS keychain backend behind build tag. New dependency: `github.com/zalando/go-keyring`.

1. Add `pkg/credentials/keychain_enabled.go` (build tag `keychain`).
2. Extend `pkg/vcs/auth.go` with `auth.keychain` resolution step (Priority 2); empty/whitespace fall-through per spec.
3. Extend `pkg/chat/credentials.go` with keychain fallback for each AI provider.
4. Extend `pkg/vcs/bitbucket/credentials.go` with JSON-blob keychain storage.
5. Update setup wizard to show keychain option when `KeychainAvailable()` **and** a probe (`keyring.Set` + `keyring.Delete` on a canary) succeeds.
6. Add `INT_TEST_CREDENTIALS` integration tests.
7. Update goreleaser to produce a keychain-enabled variant (`<tool>-keychain_<os>_<arch>.tar.gz`) alongside the default build. Document the build tag for downstream tool authors.

### Phase 3: Migration Tooling and Documentation

**Scope:** Help existing users move off literal mode.

1. Add `pkg/cmd/config/migrate.go` implementing `config migrate-credentials` with `--dry-run` and `--target env|keychain`.
2. Add Gherkin scenarios covering: fresh setup (env-var default), CI refusal, OAuth+env-var flow, Bitbucket dual-credential, keychain round-trip, doctor check, migration command.
3. Update generator templates so scaffolded tools include the `migrate-credentials` subcommand by default.
4. Add migration guide entry at `docs/migration/<version>-credential-storage.md` with before/after config examples.
5. Update `docs/about/security.md` with credential-handling trust model.

---

## Resolved Decisions

1. **Keychain service naming is `<toolname>/<key-path>`** (no `gtb/` prefix). Rationale: downstream tools built on GTB are the primary users; a `gtb/` prefix would leak the framework name into the user's keychain UI. Individual tool authors can see their tool's credentials clearly labelled. Examples:
   - `mytool/github.auth` → GitHub token for "mytool"
   - `mytool/anthropic.api` → Anthropic API key for "mytool"
   - `mytool/bitbucket.auth` → Bitbucket credentials (stored as a JSON blob `{"username":..., "app_password":...}` to preserve the dual-credential pattern)

2. **Default env var names: provider-standard with `_` sanitisation; tool-specific prefix optional.** The wizard suggests the provider-standard name (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `GITHUB_TOKEN`, `BITBUCKET_USERNAME`/`BITBUCKET_APP_PASSWORD`) because these are the conventions the wider ecosystem uses (`anthropic-py`, `openai-python`, `gh`, etc.). This maximises portability. The wizard allows the user to override with a tool-specific name if they anticipate multi-tool collisions on the same workstation.

3. **Keychain via build tag `keychain`** (not runtime detection). Rationale:
   - The default build matrix (CGO-disabled, FIPS-enabled, cross-compiled) cannot include libsecret.
   - Runtime detection requires the binary to link libsecret anyway, defeating the cross-compile goal.
   - A build tag is a clear opt-in for tool authors: shipping a keychain-enabled binary is an explicit release decision with platform implications.
   - GoReleaser can produce a second binary variant (`<tool>-keychain`) for users who want it, documented in the release notes.
   - The default build errors clearly on keychain config (`"keychain support not compiled (build with -tags keychain)"`) rather than silently falling back.

4. **GitHub OAuth flow + env-var mode: run OAuth, show the token to the user once, prompt them to set it as an env var before proceeding.** Option (a) is more helpful for first-time setup — a user who selects env-var mode almost always wants the convenience of OAuth without the storage risk. The wizard displays the token in a secure input form (terminal raw mode, no echo) and instructs the user to copy it into their password manager or shell profile. The token is **not** written to disk in any form. If the user cancels at this step, the wizard offers: retry with literal mode, retry the OAuth flow, or skip configuration.

5. **Literal mode is refused when `CI=true`** regardless of operator intent. A CI build that writes a literal credential to config almost certainly leaks it to build artefacts or logs. The wizard instead prints a clear error and exits non-zero, directing the user to CI-platform secret injection. The `CI` env-var detection already exists in `pkg/setup/` and is reused here.

6. **Keychain backend library: `github.com/zalando/go-keyring`.** Pure Go on macOS and Windows; CGO-via-libsecret on Linux. Chosen over `99designs/keyring` for a tighter feature set matching our needs (simpler API, fewer backends meaning less attack surface, widely adopted in Go tooling).

7. **Bitbucket dual credentials: stored as a JSON blob in keychain mode, or as two env vars in env-var mode.** Bitbucket requires `username` + `app_password`. The wizard handles this by either:
   - **env-var mode**: prompting for two env var names (defaults `BITBUCKET_USERNAME`, `BITBUCKET_APP_PASSWORD`), writing `bitbucket.username.env` and `bitbucket.app_password.env`.
   - **keychain mode**: storing a single JSON entry `{"username":"...","app_password":"..."}` in the keychain under `<toolname>/bitbucket.auth`. The retrieval code unmarshals before use.
   - **literal mode**: unchanged, writing both values to config.

## Security Requirements

These requirements apply across the implementation and must be verified by tests:

### R1: No credential emission above DEBUG level

Credentials must never appear in log output at INFO, WARN, ERROR, or FATAL levels. This is enforced by:
- The existing config-masking code (`pkg/cmd/config/sensitive.go`) is extended to cover all new credential config keys.
- A lint check (or test assertion) verifies that every handler for config-containing errors goes through `errorhandling.SanitiseError()` before logging.
- The setup wizard never prints a credential value it has collected (except the GitHub OAuth "display once" prompt, which writes to the terminal directly and not through the logger).

### R2: Error messages redact credential values

Errors from `tokenFromConfig`, `Retrieve`, and their callers must not include the credential value in their message. They may include the *name* of the env var or keychain entry (to help diagnose missing configuration) but never the secret itself.

### R3: Keychain `Retrieve` errors distinguish "missing" from "unavailable"

- **Missing entry** (keychain is functional but no entry exists) → return `ErrCredentialNotFound`; fall through to next resolution step.
- **Unavailable** (D-Bus unreachable, keychain locked, stub build) → return wrapped error with `WithHint`; log at DEBUG; fall through to next resolution step.
- **Corrupted entry** (JSON unmarshal failure for Bitbucket blob) → return wrapped error; do NOT fall through; abort with clear message so user can correct.

### R4: Config file permissions remain `0600`

Unchanged from audit fix M-3. Every path that writes credentials to config verifies the file mode afterward and fails if it cannot set `0600`.

### R5: `CI=true` rejects literal mode

Covered by Resolved Decision #5. The test suite includes a Gherkin scenario and unit tests.

### R6: `doctor` check for literal credentials

A new `doctor` check named `credentials.no-literal` warns when any of the following config keys contain non-empty values:
- `<provider>.api.key` (AI providers)
- `github.auth.value`
- `bitbucket.app_password`
- `gitlab.auth.value`, `gitea.auth.value`, etc.

The warning includes the hint: "Run `<tool> config migrate-credentials` to convert to environment variable references."
