---
title: "SPEC 7: Interactive Config Editor"
description: "Add a config subcommand providing interactive configuration management with get, set, list, edit, and validate operations"
date: 2026-03-24
status: DRAFT
tags:
  - specification
  - config
  - cli
  - ux
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# SPEC 7: Interactive Config Editor

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   DRAFT

---

## Overview

Configuration in GTB is currently managed by editing YAML files directly or running the `init` command, which only handles initial setup. There is no way to inspect, modify, or validate individual configuration values interactively or programmatically after initialisation.

This spec introduces a `config` subcommand with five operations:

- `config get <key>` — read and display a single config value using dot-notation
- `config set <key> <value>` — write a single config value to the config file
- `config list` — display all config keys and values, masking sensitive entries
- `config edit` — launch a TUI form for interactive bulk editing
- `config validate` — check the current config against a schema and report problems

The command is gated behind a `ConfigCmd` feature flag in `props.FeatureCmd`, enabled by default.

---

## Design Decisions

### Dot-notation key access

Viper already supports dot-notation for nested keys (e.g. `github.token`). The `get` and `set` subcommands expose this directly, keeping the mental model consistent with how keys appear in YAML and in code via `viper.GetString("github.token")`.

### Sensitive value masking

The `list` and `get` commands mask values for keys matching known sensitive patterns (tokens, passwords, secrets). The masking strategy reuses the approach from `pkg/setup/ai/ai.go`: display only the last 4 characters, replacing the rest with asterisks. A key is considered sensitive if its name contains `token`, `password`, `secret`, or `key` (case-insensitive).

### Config writes via Viper

All write operations go through `viper.Set()` followed by `viper.WriteConfig()`. This preserves Viper as the single source of truth and ensures writes respect the active config file path. If no config file exists, `viper.SafeWriteConfig()` is used to create one.

### TUI edit form

The `config edit` subcommand uses `charmbracelet/huh` forms to present editable fields grouped by section. Sensitive fields use `huh.Input` with `EchoMode` set to password mode. The form is built dynamically from the current config keys.

### Schema validation

The `config validate` subcommand checks the current configuration against required key definitions already present in `validateConfig` within `root.go`. Validation reports missing required fields, type mismatches, and values that fail format checks (e.g. URLs, email addresses). Output is a list of diagnostics with severity levels (error, warning).

---

## Public API Changes

### New package: `pkg/cmd/config/`

```go
// NewCmdConfig returns the top-level config command with all subcommands attached.
func NewCmdConfig(props *props.Props) *cobra.Command

// NewCmdGet returns the "config get <key>" subcommand.
func NewCmdGet(props *props.Props) *cobra.Command

// NewCmdSet returns the "config set <key> <value>" subcommand.
func NewCmdSet(props *props.Props) *cobra.Command

// NewCmdList returns the "config list" subcommand.
func NewCmdList(props *props.Props) *cobra.Command

// NewCmdEdit returns the "config edit" TUI subcommand.
func NewCmdEdit(props *props.Props) *cobra.Command

// NewCmdValidate returns the "config validate" subcommand.
func NewCmdValidate(props *props.Props) *cobra.Command
```

### Feature flag addition

```go
// In props.FeatureCmd
ConfigCmd bool // default: true
```

### Sensitive key helper (pkg/config or pkg/cmd/config)

```go
// MaskSensitive returns the value with all but the last 4 characters replaced
// by asterisks. Returns the full asterisk string if the value is 4 characters
// or fewer.
func MaskSensitive(value string) string

// IsSensitiveKey returns true if the key name matches a sensitive pattern.
func IsSensitiveKey(key string) bool
```

---

## Internal Implementation

### `config get`

1. Accept a single positional argument: the dot-notation key.
2. Read the value via `props.Config` (the `config.Containable` interface).
3. If the key does not exist in Viper, return an error using `cockroachdb/errors`.
4. If `IsSensitiveKey` returns true, apply `MaskSensitive` unless `--unmask` flag is set.
5. Print the value to stdout.

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
4. For each key, apply `MaskSensitive` if `IsSensitiveKey` returns true.
5. Render as a formatted table (key, value columns) using lipgloss styling.

### `config edit`

1. Build a `huh.Form` dynamically from `viper.AllSettings()`.
2. Group fields by top-level config section.
3. Sensitive fields use password echo mode.
4. On form completion, apply all changed values via `viper.Set()` and write config.
5. Display a summary of changed fields.

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
    edit.go            # NewCmdEdit TUI implementation
    edit_test.go
    validate.go        # NewCmdValidate implementation
    validate_test.go
    sensitive.go       # MaskSensitive, IsSensitiveKey helpers
    sensitive_test.go
```

---

## Testing Strategy

### Unit Tests

- **Unit tests** for every subcommand handler using testify assertions.
- **`MaskSensitive` / `IsSensitiveKey`** tested with table-driven tests covering edge cases (empty string, exactly 4 chars, various key patterns).
- **`config set`** tests use afero in-memory filesystem to verify config file writes without touching disk.
- **`config get`** tests set up Viper with known values and assert correct output, including masking behaviour.
- **`config list`** tests verify alphabetical ordering, masking of sensitive keys, and table formatting.
- **`config validate`** tests provide configs with missing keys, wrong types, and valid configs to assert correct diagnostic output.
- **`config edit`** tests use `teatest` for bubbletea/huh model testing where feasible; integration-level tests verify that form submission writes correct values.
- **Mocks** generated via mockery/v3 for `config.Containable` and any other interfaces.
- **Coverage target:** 90%+ for all files in `pkg/cmd/config/`.

### Integration Tests

- **Config file round-trip**: Write values via `config set`, read back via `config get`, verify consistency across the Viper config layer and on-disk YAML.
- **Schema validation end-to-end**: Load a multi-file config with embedded defaults, run `config validate`, assert correct diagnostics for missing required keys and type mismatches.
- Gate with `testutil.SkipIfNotIntegration(t, "config")` in a dedicated `config_integration_test.go` file.

### E2E BDD Tests (Godog) — **Strong fit**

The `config` subcommand introduces five user-facing CLI operations that are natural Given/When/Then scenarios. Feature file: `features/cli/config.feature`.

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

  Scenario: JSON output for get command
    Given the init directory contains a config file:
      """
      log:
        level: warn
      """
    When I run gtb with "config get log.level --config {init_dir}/config.yaml --output json"
    Then the exit code is 0
    And stdout is valid JSON
```

The `config edit` subcommand (interactive TUI) is **not** testable via Godog — test outcomes only (verify that form submission writes correct values) using `teatest` in unit tests.

**Note:** The `config get` command also unblocks config precedence E2E testing (deferred from the Godog BDD strategy Phase 3). Once implemented, add scenarios verifying file → env → flag precedence.

---

## Backwards Compatibility

No breaking changes. The `config` subcommand is purely additive. Existing config file formats are unchanged. The feature flag defaults to enabled but can be disabled by tools that do not want to expose config management to end users.

---

## Future Considerations

- **Config profiles:** support multiple named config files (e.g. `--profile staging`) for switching between environments.
- **Config diff:** show differences between current config and defaults, or between two config files.
- **Remote config:** read/write config from remote sources (e.g. environment variables, Vault) through Viper's existing remote provider support.
- **Config export/import:** export config as JSON/YAML for sharing, import from a file or stdin.
- **Structured output:** the `config` subcommand should support `--output json` from the start (see Spec 8).

---

## Implementation Phases

### Phase 1: Core read operations

- Implement `config get` and `config list` subcommands.
- Implement `MaskSensitive` and `IsSensitiveKey` helpers with full test coverage.
- Register `ConfigCmd` feature flag.
- Wire `config` command into root command registration.

### Phase 2: Write operations and validation

- Implement `config set` subcommand with type coercion.
- Extract validation rules from `root.go` into a shared function.
- Implement `config validate` subcommand.

### Phase 3: TUI editor

- Implement `config edit` with dynamic `huh.Form` generation.
- Add section grouping and sensitive field handling.
- Add teatest-based tests.

---

## Verification

- [ ] `config get github.token` returns a masked value.
- [ ] `config get github.token --unmask` returns the full value.
- [ ] `config get nonexistent.key` returns a clear error message.
- [ ] `config set github.token <value>` writes to the config file and is readable via `config get`.
- [ ] `config list` displays all keys alphabetically with sensitive values masked.
- [ ] `config edit` opens a TUI form, allows editing, and writes changes on submit.
- [ ] `config validate` reports missing required fields and type mismatches.
- [ ] `config validate` exits 0 when config is valid, non-zero otherwise.
- [ ] All tests pass: `just test-pkg pkg/cmd/config`.
- [ ] Coverage is 90%+ for `pkg/cmd/config/`.
- [ ] Feature flag `ConfigCmd: false` prevents the command from registering.
