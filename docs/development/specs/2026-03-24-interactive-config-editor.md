---
title: "Programmatic Config Access"
description: "Add a config subcommand providing programmatic config management with get, set, list, and validate operations — primarily for CI automation and scripted setup"
date: 2026-03-24
status: IMPLEMENTED
tags:
  - specification
  - config
  - cli
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# SPEC 7: Programmatic Config Access

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   IN PROGRESS

---

## Overview

GTB already provides two mechanisms for managing configuration:

1. **Direct YAML editing** — the config file can be edited by hand.
2. **`init` command + Initialiser pattern** — `init` runs registered `Initialiser` implementations sequentially to guide users through interactive TUI-driven setup. Individual subsystems can be reconfigured independently via `init <subsystem>` (e.g. `init ai`, `init github`), each providing structured forms that only ask for what that subsystem needs.

What is missing is **programmatic, non-interactive access** to individual config keys. There is no way to read a single value from a shell script, set a value in CI without editing YAML directly, or validate the current config independently of running a full initialisation flow.

This spec introduces a `config` subcommand with four operations:

- `config get <key>` — read and display a single config value using dot-notation
- `config set <key> <value>` — write a single config value to the config file
- `config list` — display all resolved config keys and values, masking sensitive entries
- `config validate` — check the current config against required key definitions and report problems

The command is gated behind a `ConfigCmd` feature flag, **disabled by default**, and is only valuable in tools with local file-based configuration (see [When to Enable](#when-to-enable)).

---

## When to Enable

Both `ConfigCmd` and `InitCmd` are only relevant when a tool manages configuration in local YAML files — typically a developer-facing CLI tool. They should be disabled for tools deployed as containerised services or accessed via API, where configuration comes from environment variables or mounted secrets.

| Deployment model | `InitCmd` | `ConfigCmd` |
|-----------------|-----------|-------------|
| Developer CLI tool | Enable | Enable |
| Containerised web service | Disable | Disable |
| Library / embedded SDK | Disable | Disable |

Feature flags are controlled via `props.SetFeatures`:

```go
props.SetFeatures(
    props.Enable(props.InitCmd),
    props.Enable(props.ConfigCmd),
)
```

This mirrors the existing `UpdateCmd`, `McpCmd`, `DoctorCmd` pattern documented in `CLAUDE.md`.

---

## Relationship with `init` and Initialisers

These two systems serve different audiences and workflows:

| | `init <subsystem>` | `config get/set/list` |
|--|------------------|----------------------|
| **Audience** | Humans doing first-run or targeted reconfiguration | CI pipelines, shell scripts, tool authors |
| **Interaction** | Interactive TUI with per-subsystem guided forms | Non-interactive; reads/writes single keys |
| **Scope** | A whole subsystem (e.g. all AI keys at once) | Any individual key in dot-notation |
| **Discovery** | Registered via `setup.Register()` in `init()` | Operates on the live Viper config directly |
| **Use case** | "I want to change my AI provider" | "Set `github.url.api` to a GHE URL in CI" |

**Non-goal:** `config` does not replace or duplicate `init <subsystem>`. Humans who want to interactively reconfigure a subsystem should use `init <subsystem>`, which provides the structured guided experience for that area. `config set` is for scripted key-by-key access.

---

## Design Decisions

### Dot-notation key access

Viper already supports dot-notation for nested keys (e.g. `github.token`). The `get` and `set` subcommands expose this directly, keeping the mental model consistent with how keys appear in YAML and in code via `viper.GetString("github.token")`.

### Sensitive value masking

Masking uses two independent detection strategies applied in combination — a value is masked if **either** triggers:

1. **Key name patterns** — the dot-notation key's final segment (leaf name) matches a known sensitive substring: `token`, `password`, `secret`, `key`, `apikey`, `auth` (case-insensitive). This catches keys like `ai.claude.key` or `github.ssh.key.path`.

2. **Value content patterns** — the value itself matches a known credential regular expression, regardless of the key name. This catches cases where a sensitive value is stored under a non-obvious key such as `github.auth.value`. Built-in patterns cover common credential formats (e.g. GitHub PATs: `ghp_[A-Za-z0-9]{36}`, `github_pat_[A-Za-z0-9_]{82}`).

Both the key-name list and the value-content regexes are **extensible**: tool authors can register additional patterns via a `Masker` type using a functional options pattern, rather than modifying the defaults. This allows tools built on GTB to mask their own credential formats without forking the masking logic.

The masking strategy itself reuses the approach from `pkg/setup/ai/ai.go`: display only the last 4 characters of the detected secret, replacing the rest with asterisks (or full asterisks if the value is 4 characters or fewer). An `--unmask` flag on `get` bypasses all masking.

### Output formats

`get` and `list` support `--output text|json|yaml` (default: `text`) to enable CI and shell script consumption without text parsing. JSON output is particularly useful for piping into tools like `jq`.

### Config writes via Viper

All write operations go through `viper.Set()` followed by `viper.WriteConfig()`. This preserves Viper as the single source of truth and ensures writes respect the active config file path. If no config file exists, `viper.SafeWriteConfig()` is used to create one.

### Schema validation

The `config validate` subcommand checks the current configuration against required key definitions already present in `validateConfig` within `root.go`. Validation reports missing required fields, type mismatches, and values that fail format checks (e.g. URLs). Output is a list of diagnostics with severity levels (error, warning).

---

## Public API Changes

### New package: `pkg/cmd/config/`

```go
// NewCmdConfig returns the top-level config command with all subcommands attached.
// MaskerOptions extend the built-in sensitive key and value patterns.
func NewCmdConfig(props *props.Props, opts ...MaskerOption) *cobra.Command

// NewCmdGet returns the "config get <key>" subcommand.
func NewCmdGet(props *props.Props) *cobra.Command

// NewCmdSet returns the "config set <key> <value>" subcommand.
func NewCmdSet(props *props.Props) *cobra.Command

// NewCmdList returns the "config list" subcommand.
func NewCmdList(props *props.Props) *cobra.Command

// NewCmdValidate returns the "config validate" subcommand.
func NewCmdValidate(props *props.Props) *cobra.Command
```

### Feature flag addition

```go
// In props.FeatureCmd
ConfigCmd bool // default: false
```

### Sensitive masking (`pkg/cmd/config`)

```go
// Masker detects and masks sensitive config values. The zero value is not
// useful; use NewMasker to construct one with defaults.
type Masker struct { /* unexported */ }

// MaskerOption configures a Masker.
type MaskerOption func(*Masker)

// WithKeyPattern registers an additional key-name substring (case-insensitive)
// that marks a key as sensitive. Extends the built-in list; does not replace it.
func WithKeyPattern(pattern string) MaskerOption

// WithValuePattern registers an additional compiled regexp that, when matched
// against a value, marks it as sensitive regardless of the key name.
func WithValuePattern(re *regexp.Regexp) MaskerOption

// NewMasker constructs a Masker with built-in key patterns and value regexes,
// extended by any provided options.
func NewMasker(opts ...MaskerOption) *Masker

// IsSensitive returns true if the key name matches a sensitive key pattern
// OR the value matches a sensitive value pattern.
func (m *Masker) IsSensitive(key, value string) bool

// Mask returns the value with all but the last 4 characters replaced by
// asterisks. Returns the full asterisk string if the value is 4 characters
// or fewer.
func (m *Masker) Mask(value string) string

// MaskIfSensitive applies Mask only when IsSensitive returns true.
func (m *Masker) MaskIfSensitive(key, value string) string
```

Built-in key patterns: `token`, `password`, `secret`, `key`, `apikey`, `auth`.

Built-in value patterns: GitHub classic PAT (`ghp_[A-Za-z0-9]{36}`), GitHub fine-grained PAT (`github_pat_[A-Za-z0-9_]{82}`).

The `Masker` is constructed once at command initialisation and threaded through the `get`, `list`, and `validate` subcommands. The root `NewCmdConfig` accepts `...MaskerOption` so tool authors can extend the defaults at the point where they wire up the command.

---

## Internal Implementation

### `config get`

1. Accept a single positional argument: the dot-notation key.
2. Read the value via `props.Config` (the `config.Containable` interface).
3. If the key does not exist in Viper, return an error using `cockroachdb/errors`.
4. Unless `--unmask` flag is set, call `masker.MaskIfSensitive(key, value)` — this applies masking if either the key name or the value content matches a sensitive pattern.
5. Render according to `--output` flag: `text` prints the raw value, `json` wraps in `{"key": "...", "value": "..."}`, `yaml` renders as `key: value`.

### `config set`

1. Accept two positional arguments: key and value.
2. Attempt type coercion: if the value parses as bool or int, store the typed value; otherwise store as string.
3. Call `viper.Set(key, value)`.
4. Write config via `viper.WriteConfig()`. If no config file exists, use `viper.SafeWriteConfig()`.
5. Print confirmation message.

### `config list`

1. Retrieve all settings via `viper.AllSettings()`.
2. Flatten the nested map into dot-notation keys.
3. Sort keys alphabetically.
4. For each key, call `masker.MaskIfSensitive(key, value)` — masking triggers on either the key name or the value content matching a sensitive pattern.
5. Render according to `--output` flag: `text` renders a formatted two-column table (key, value) using lipgloss styling; `json` renders a flat JSON object; `yaml` renders the full nested YAML structure.

### `config validate`

1. Load validation rules from the existing `validateConfig` logic in `root.go`. Extract this into a shared, testable function if not already.
2. Iterate over rules, checking each against current config values.
3. Collect diagnostics: `{Key, Severity, Message}`.
4. Render diagnostics as a table or list. Exit with non-zero status if any errors are found.

---

## Project Structure

```
pkg/cmd/config/
    config.go          # NewCmdConfig, parent command setup
    get.go             # NewCmdGet implementation
    get_test.go
    set.go             # NewCmdSet implementation
    set_test.go
    list.go            # NewCmdList implementation
    list_test.go
    validate.go        # NewCmdValidate implementation
    validate_test.go
    sensitive.go       # MaskSensitive, IsSensitiveKey helpers
    sensitive_test.go
```

---

## Testing Strategy

### Unit Tests

- **`Masker`** tested with table-driven tests covering: key-name pattern matching (including `github.auth.value` → not masked by key, but masked by value regex), built-in GitHub PAT value patterns, custom patterns via `WithKeyPattern` and `WithValuePattern`, `Mask` edge cases (empty string, exactly 4 chars, long values), and that default patterns are not replaced by custom ones.
- **`config set`** tests use afero in-memory filesystem to verify config file writes without touching disk.
- **`config get`** tests set up Viper with known values and assert correct output, including masking behaviour and all three `--output` formats.
- **`config list`** tests verify alphabetical ordering, masking of sensitive keys, and all three `--output` formats.
- **`config validate`** tests provide configs with missing keys, wrong types, and valid configs to assert correct diagnostic output.
- **Mocks** generated via mockery/v3 for `config.Containable` and any other interfaces.
- **Coverage target:** 90%+ for all files in `pkg/cmd/config/`.

### Integration Tests

- **Config file round-trip**: Write values via `config set`, read back via `config get`, verify consistency across the Viper config layer and on-disk YAML.
- **Schema validation end-to-end**: Load a multi-file config with embedded defaults, run `config validate`, assert correct diagnostics for missing required keys and type mismatches.
- Gate with `testutil.SkipIfNotIntegration(t, "config")` in a dedicated `config_integration_test.go` file.

### E2E BDD Tests (Godog) — **Strong fit**

The `config` subcommand introduces four user-facing CLI operations with clear Given/When/Then semantics. Feature file: `features/cli/config.feature`.

```gherkin
@cli @smoke
Feature: CLI Config Command
  Background:
    Given the gtb binary is built
    And a temporary init directory

  Scenario: Get a config value
    Given the init directory contains a config file:
      """
      log:
        level: debug
      """
    When I run gtb with "config get log.level --config {init_dir}/config.yaml"
    Then the exit code is 0
    And stdout contains "debug"

  Scenario: Set a config value
    Given the init directory contains a config file:
      """
      log:
        level: info
      """
    When I run gtb with "config set log.level debug --config {init_dir}/config.yaml"
    Then the exit code is 0
    And the config file in the init directory contains "level: debug"

  Scenario: List config values with sensitive masking
    Given the init directory contains a config file:
      """
      github:
        auth:
          value: secret-token-123
      log:
        level: info
      """
    When I run gtb with "config list --config {init_dir}/config.yaml"
    Then the exit code is 0
    And stdout contains "log.level"
    And stdout does not contain "secret-token-123"

  Scenario: Validate config reports missing keys
    Given the init directory contains a config file:
      """
      custom:
        key: value
      """
    When I run gtb with "config validate --config {init_dir}/config.yaml"
    Then the exit code is not 0

  Scenario: JSON output for CI consumption
    Given the init directory contains a config file:
      """
      log:
        level: warn
      """
    When I run gtb with "config get log.level --config {init_dir}/config.yaml --output json"
    Then the exit code is 0
    And stdout is valid JSON
```

**Note:** Once `config get` is implemented, it unblocks config precedence E2E testing (deferred from the Godog BDD strategy Phase 3). Add scenarios verifying file → env → flag precedence once the command is in place.

---

## Backwards Compatibility

No breaking changes. The `config` subcommand is purely additive. Existing config file formats are unchanged. The feature flag defaults to **disabled** — tools must explicitly opt in, ensuring tools that do not need local config management are unaffected.

---

## Future Considerations

- **`config edit` TUI**: An interactive editor that builds a `huh.Form` dynamically from all current config keys (grouped by section) could complement `config set` for humans who prefer a guided form over key-by-key commands. This was deferred because the `init <subsystem>` pattern already provides superior per-subsystem TUI forms; a flat combined editor adds limited value over `init <subsystem>` + manual YAML editing.
- **Config profiles**: support multiple named config files (e.g. `--profile staging`) for switching between environments.
- **Config diff**: show differences between current config and defaults, or between two config files.
- **Config export/import**: export config as JSON/YAML for sharing, import from a file or stdin.
- **Remote config**: read/write config from remote sources (e.g. environment variables, Vault) through Viper's existing remote provider support.

---

## Implementation Phases

### Phase 1: Core read operations

- Implement `config get` and `config list` subcommands with `--output text|json|yaml` support.
- Implement `MaskSensitive` and `IsSensitiveKey` helpers with full test coverage.
- Register `ConfigCmd` feature flag (default: disabled).
- Wire `config` command into root command registration when flag is enabled.

### Phase 2: Write operations and validation

- Implement `config set` subcommand with type coercion.
- Extract validation rules from `root.go` into a shared function.
- Implement `config validate` subcommand.

---

## Verification

- [ ] `config get github.token` returns a masked value.
- [ ] `config get github.token --unmask` returns the full value.
- [ ] `config get nonexistent.key` returns a clear error message.
- [ ] `config get log.level --output json` returns valid JSON.
- [ ] `config set github.token <value>` writes to the config file and is readable via `config get`.
- [ ] `config list` displays all keys alphabetically with sensitive values masked.
- [ ] `config list --output json` returns a valid JSON object of all keys.
- [ ] `config validate` reports missing required fields and type mismatches.
- [ ] `config validate` exits 0 when config is valid, non-zero otherwise.
- [ ] All tests pass: `just test-pkg pkg/cmd/config`.
- [ ] Coverage is 90%+ for `pkg/cmd/config/`.
- [ ] Feature flag `ConfigCmd: false` (default) prevents the command from registering.
- [ ] Feature flag `ConfigCmd: true` registers the command alongside `init` in a CLI tool.
