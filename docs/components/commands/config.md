---
title: Config Command
description: Programmatic read/write access to individual configuration values for CI pipelines and scripted setup.
date: 2026-03-29
tags: [components, commands, config, ci]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Config Command

The `config` command provides programmatic read/write access to individual configuration values.
Its primary audience is CI pipelines and tool authors automating setup — not humans doing
interactive reconfiguration (use `init <subsystem>` for that instead).

## Usage

```bash
mytool config get <key> [--output text|json|yaml] [--unmask]
mytool config set <key> <value>
mytool config list [--output text|json|yaml]
mytool config validate
```

## Feature Flag

The `config` command is **disabled by default**. Enable it via `props.SetFeatures`:

```go
props.SetFeatures(props.Enable(props.ConfigCmd))
```

!!! info "When to enable"
    Enable `ConfigCmd` for developer-facing CLI tools where local YAML config management
    is relevant. For containerized services, leave it disabled — configuration arrives via
    environment variables or mounted secrets, not YAML files.

## Subcommands

### `config get <key>`

Read a single configuration value and emit it to stdout for shell script consumption.

Sensitive values (tokens, passwords, secrets) are masked by default. Use `--unmask` to
reveal the raw value.

```bash
# Plain text output
mytool config get log.level

# JSON output (useful in CI for structured parsing)
mytool config get github.auth.token --output json

# Reveal masked value
mytool config get github.auth.token --unmask
```

**Flags:**

| Flag | Description | Default |
| :--- | :--- | :--- |
| `--output` | Output format: `text`, `json`, `yaml` | `text` |
| `--unmask` | Disable sensitive value masking | `false` |

### `config set <key> <value>`

Write a single configuration value. The value is type-coerced (bool → int64 → string) and
persisted to the config file on disk.

```bash
mytool config set log.level debug
mytool config set feature.enabled true
```

### `config list`

List all resolved configuration values, sorted alphabetically. Sensitive values are masked.

```bash
# Human-readable table
mytool config list

# Machine-readable JSON (for CI inspection)
mytool config list --output json
```

### `config validate`

Validate the current configuration against the tool's required schema. Exits non-zero if
any required fields are missing or invalid.

```bash
mytool config validate
```

## Sensitive Value Masking

The masking system uses two independent strategies:

1. **Key-name matching** — checks the leaf segment of the dotted key path against known
   patterns: `token`, `password`, `secret`, `key`, `apikey`, `auth`.
2. **Value-content matching** — checks the value against known token regexps (e.g. GitHub
   PATs: `ghp_...`, `github_pat_...`). This covers cases like `github.auth.value` where
   the key name `value` is not sensitive but the content may be a token.

### Custom patterns

Tool authors can extend the masker via functional options on `NewCmdConfig`:

```go
import (
    cmdconfig "github.com/phpboyscout/go-tool-base/pkg/cmd/config"
    "regexp"
)

cmdconfig.NewCmdConfig(props,
    cmdconfig.WithKeyPattern("credential"),
    cmdconfig.WithValuePattern(regexp.MustCompile(`^sk-[A-Za-z0-9]{32}$`)),
)
```

## Relationship with `init`

| Workflow | Command |
| :--- | :--- |
| First-run bootstrap | `init` |
| Re-configure a subsystem interactively | `init <subsystem>` (e.g. `init ai`, `init github`) |
| Read a single value in a script or CI | `config get <key>` |
| Write a single value in a script or CI | `config set <key> <value>` |
| Inspect all resolved config | `config list` |
| Validate config against schema | `config validate` |

Both `InitCmd` and `ConfigCmd` should be disabled in containerized services where local
YAML config is not applicable.

## Implementation

- **`pkg/cmd/config/`** — Command implementations (`get`, `set`, `list`, `validate`)
- **`pkg/cmd/config/sensitive.go`** — `Masker` type with dual-strategy detection
- Feature flag: `props.ConfigCmd` (default: disabled)
