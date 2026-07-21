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

For the catalogue of keys the framework reads (types, defaults, env mapping), see the
[Configuration Keys reference](../config/index.md).

## Usage

```bash
mytool config get <key> [--output text|json|yaml] [--unmask]
mytool config set <key> <value>
mytool config unset <key>
mytool config list [--output text|json|yaml]
mytool config path [--writable] [--output text|json|yaml]
mytool config edit [--editor <cmd>]
mytool config validate
```

!!! note "File-layer-only operations"
    `set`, `unset`, and `edit` mutate **only the writable config file** — the
    layer `path` reports as `writable`. Values resolved from environment
    variables or CLI flags are not file-backed and cannot be unset or edited;
    `unset` refuses with *not found* for a key that is present only via those
    layers. The result of every write is re-validated against the same schema
    as `config validate`, so these commands can never leave a config the
    validator would reject.

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
persisted to the config file on disk. The written file is left owner-only (`0600`).

```bash
mytool config set log.level debug
mytool config set feature.enabled true
```

**Credential safety.** When the write would place a recognised credential in a
project-local `.<tool>.yaml` — the repo-root config layer, which the store
writes to ahead of your private config when one is present — `set` warns that
the file may be committed to version control. In an interactive terminal it
asks you to confirm; declining aborts and leaves the file untouched. In a
non-interactive session (CI, a pipe) it warns and proceeds. The write is never
blocked outright — a project-local secret can be deliberate — but prefer
env-var or OS-keychain storage (see the tool's `init`) for anything sensitive.
A credential is recognised by a token-shaped value or a known credential key;
env-var and keychain *references* (`…​.env`, `…​.keychain`) are not secrets and
never trip the warning.

### `config unset <key>`

Remove a single configuration value — the inverse of `config set`. Only the writable
file layer is affected. The post-removal config is re-validated **layered over the
tool's embedded defaults** before it is written: removing a key the defaults supply
(e.g. `log.level`) succeeds and the resolved value falls back to the shipped default,
while a removal that would leave the resolved configuration invalid is refused and
leaves the file untouched.

```bash
mytool config unset feature.enabled
mytool config unset ai.model --output json
```

A key that exists only via an environment variable or flag is reported as *not found*,
because there is nothing in the file for `unset` to remove.

### `config path`

Print the config file(s) that contributed to the live configuration. Because configuration
is merged across a precedence chain, several files can contribute; by default `path` lists
each contributing file in merge order plus the `writable` target that `set`/`unset`/`edit`
mutate.

```bash
# All contributing files plus the writable target
mytool config path

# Only the writable target — handy in scripts
mytool config path --writable

# Structured output
mytool config path --output json
```

When no config file is currently loaded, `path` prints only the writable target (the path a
future `set` would create) and notes that no file is loaded — it does not error.

**Flags:**

| Flag | Description | Default |
| :--- | :--- | :--- |
| `--writable` | Print only the single writable target path | `false` |
| `--output` | Output format: `text`, `json`, `yaml` | `text` |

### `config edit`

Open the writable config file in your editor, re-validate it on save, and persist the change
only if it is valid. The editor is resolved from `--editor`, then `$VISUAL`, then `$EDITOR`,
then a platform default (`vi` on Unix, `notepad` on Windows). Editor commands are parsed with
shell-quoting awareness, so both `EDITOR="code --wait"` and quoted paths containing spaces work.

```bash
mytool config edit
mytool config edit --editor "code --wait"
```

`edit` requires an interactive terminal; in CI or a non-interactive pipeline it refuses and
points you at `set`/`unset`. On a non-zero editor exit the original file is left untouched. On
invalid YAML or a schema validation failure the edit is aborted and your unsaved work is
preserved at a reported temp-file path so nothing is lost.

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
any required fields are missing or invalid (e.g. `log.level` outside
`debug`/`info`/`warn`/`error`).

```bash
mytool config validate
```

Unknown-key warnings are reserved for keys that look like genuine mistakes. A
key under a framework section (`log.*`, `update.*`, `server.*`, the provider and
forge sections, …) or one the tool declares in its own embedded defaults is a
recognised key, not flagged — the schema cannot enumerate every feature and
tool key. A key matching neither still warns as a possible typo.
