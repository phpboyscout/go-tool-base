---
title: "Credential Storage Hardening"
description: "Harden interactive setup to default to environment variable references for credential storage, with optional OS keychain integration as a pluggable local-development backend."
date: 2026-04-02
status: DRAFT
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
:   DRAFT

---

## Overview

A security audit (H-1 in `docs/development/security-audit-2026-04-02.md`) identified that API keys for AI providers and VCS tokens are stored as plaintext YAML in config files on disk. While GTB already supports an `auth.env` config key that references an environment variable name instead of a literal value, the interactive setup wizard (`pkg/setup/ai/`, `pkg/setup/github/`) defaults to storing the literal credential directly.

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

### Goals

- Make the env-var-reference path the **default** during interactive setup.
- Preserve backward compatibility -- existing `auth.value` configs continue to work without migration.
- Provide an optional keychain backend for users who find env vars inconvenient in local development.
- Document the expected credential management model for each deployment context.

### Non-Goals

- Replacing or deprecating `auth.value` -- it remains a valid config option.
- Building a generic secrets provider interface or Vault integration.
- Encrypting the config file at rest.
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
Stage 1: Select AI Provider
  [Claude (Anthropic) / OpenAI / Gemini (Google)]

Stage 2: Credential Storage Method
  > Store as environment variable reference (recommended)
    Store in OS keychain [only shown if keychain build tag active]
    Store literal value in config file

Stage 3a (env var): Environment Variable Name
  Default: ANTHROPIC_API_KEY
  "Set this variable in your shell profile or CI/CD secrets"

Stage 3b (keychain): API Key
  [password input]
  -> stored via go-keyring with service="gtb/<toolname>" account="anthropic.api.key"

Stage 3c (literal): API Key
  [password input]
  -> written to config as today
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

In `pkg/vcs/auth.go`, the `tokenFromConfig` function is extended:

```go
func tokenFromConfig(cfg config.Containable) string {
    if cfg == nil {
        return ""
    }

    // Priority 1: environment variable reference
    if cfg.Has("auth.env") {
        if token := os.Getenv(cfg.GetString("auth.env")); token != "" {
            return token
        }
    }

    // Priority 2: OS keychain (no-op without build tag)
    if cfg.Has("auth.keychain") {
        if token, err := credentials.Retrieve(cfg.GetString("auth.keychain"), ""); err == nil && token != "" {
            return token
        }
    }

    // Priority 3: literal value
    if cfg.Has("auth.value") {
        return cfg.GetString("auth.value")
    }

    return ""
}
```

The AI provider credential resolution (`pkg/chat/claude.go`, `pkg/chat/openai.go`, `pkg/chat/gemini.go`) is updated similarly to check `<provider>.api.env` and `<provider>.api.keychain` before falling back to `<provider>.api.key`.

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
| `pkg/credentials/` (keychain tag) | `Store`/`Retrieve`/`Delete` round-trip with mock keyring |
| `pkg/credentials/` (no tag) | Stub functions return descriptive errors |
| `pkg/vcs/auth.go` | Existing tests pass unchanged; new tests for `auth.keychain` priority |
| `pkg/vcs/auth.go` | `auth.env` > `auth.keychain` > `auth.value` precedence verified |
| `pkg/setup/ai/` | Form injects storage mode; env-var mode writes `<provider>.api.env`; literal mode writes `<provider>.api.key`; keychain mode writes `<provider>.api.keychain` |
| `pkg/setup/github/` | Env-var mode writes `github.auth.env`; literal mode writes `github.auth.value` |
| `pkg/chat/claude.go` | Resolves from `anthropic.api.env` before `anthropic.api.key` |
| `pkg/chat/openai.go` | Resolves from `openai.api.env` before `openai.api.key` |
| `pkg/chat/gemini.go` | Resolves from `gemini.api.env` before `gemini.api.key` |

### Integration Tests

| Tag | Scope |
|-----|-------|
| `INT_TEST_CREDENTIALS` | Keychain round-trip on supported platforms (macOS, Linux with libsecret) |
| `INT_TEST_SETUP` | Full `init` flow verifying config file output per storage mode |

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

## Future Considerations

- **Credential rotation reminders**: The keychain backend could track credential age and warn when keys are older than a configurable threshold.
- **`doctor` command credential audit**: The `doctor` command could check for literal values in config and suggest migration to env-var or keychain mode.
- **`config migrate-credentials` subcommand**: A one-shot command to convert existing `auth.value` entries to `auth.env` references, with interactive guidance on setting the corresponding environment variables.
- **Encrypted config file**: A full-file encryption layer (e.g. `age`/`sops`) is a separate concern that could complement this work but is out of scope.

---

## Implementation Phases

### Phase 1: Env-Var Default in Setup Wizard

**Scope:** Change the interactive setup to default to env-var references. No new dependencies.

1. Add `pkg/credentials/mode.go` with `Mode` type and constants (stub keychain as unavailable).
2. Modify `pkg/setup/ai/ai.go` to present storage mode selector; default to `ModeEnvVar`.
3. Modify `pkg/setup/github/github.go` to present storage mode selector; default to `ModeEnvVar`.
4. Add `anthropic.api.env`, `openai.api.env`, `gemini.api.env` config key support to `pkg/chat/`.
5. Update tests for all modified packages.
6. Add trust model documentation to `docs/components/setup/`.

### Phase 2: Keychain Integration

**Scope:** Add optional OS keychain backend behind build tag. New dependency: `go-keyring`.

1. Add `pkg/credentials/keychain_enabled.go` and `keychain_stub.go`.
2. Extend `pkg/vcs/auth.go` with `auth.keychain` resolution step.
3. Extend AI provider credential resolution with keychain fallback.
4. Update setup wizard to show keychain option when `KeychainAvailable()` is true.
5. Add integration tests gated by `INT_TEST_CREDENTIALS`.
6. Update goreleaser to produce a keychain-enabled variant (or document the build tag for downstream tool authors).

### Phase 3: Documentation and Tooling

**Scope:** Polish and supporting tooling.

1. Add `doctor` check that warns about literal credentials in config.
2. Add BDD scenarios for the setup credential flow.
3. Update generator templates for new tool scaffolding.
4. Add migration guide for users converting from literal to env-var storage.

---

## Open Questions

1. **Keychain service naming convention**: Should the keychain service name be `gtb/<toolname>/<key>` or `<toolname>/<key>`? The `gtb/` prefix makes it clear these are GTB-managed credentials, but downstream tool authors may prefer their own branding.

2. **Default env var names**: Should the wizard suggest provider-standard env var names (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `GITHUB_TOKEN`) or tool-specific prefixed names (`MYTOOL_ANTHROPIC_API_KEY`)? Provider-standard names are more portable but risk collisions in multi-tool setups.

3. **Keychain build tag vs runtime detection**: The spec uses a build tag to gate keychain support. An alternative is to always compile in keychain support and detect availability at runtime. The build tag approach avoids CGO on Linux but means the default binary lacks keychain support. Which trade-off is preferred?

4. **GitHub OAuth flow and env-var mode**: The GitHub initialiser currently runs `gh auth login` and captures the token. If the user selects env-var mode, should the wizard (a) still run the OAuth flow and tell the user to set the resulting token as an env var, or (b) skip the OAuth flow entirely and just prompt for the env var name? Option (a) is more helpful for first-time setup; option (b) is simpler.
