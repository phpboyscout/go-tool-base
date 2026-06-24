---
title: Telemetry Command
description: CLI commands for managing anonymous usage telemetry — enable, disable, status, and GDPR data reset.
date: 2026-03-31
tags: [components, commands, telemetry, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Telemetry Command

The `telemetry` command provides user-facing controls for managing anonymous usage telemetry.
Users can opt in, opt out, check status, and request deletion of collected data.

## Usage

```bash
mytool telemetry enable
mytool telemetry disable
mytool telemetry status
mytool telemetry reset
```

## Feature Flag

The `telemetry` command is **disabled by default**. Enable it via `props.SetFeatures`:

```go
props.SetFeatures(props.Enable(props.TelemetryCmd))
```

The feature flag controls command availability. Even when enabled, telemetry collection
requires explicit user consent — it is never active by default.

!!! info "When to enable"
    Enable `TelemetryCmd` when you want anonymous usage data to prioritise features,
    identify common errors, and measure adoption. The data collected contains no PII.

## Subcommands

### `telemetry enable`

Opts the user into anonymous usage telemetry. Writes `telemetry.enabled: true` to the
config file and persists it to disk.

If no config file exists (e.g. tools that disable `InitCmd`), the default config directory
(`~/.toolname/`) and config file are created automatically.

```bash
$ mytool telemetry enable
Telemetry enabled. Thank you for helping improve mytool!
No personally identifiable information is collected.
```

### `telemetry disable`

Opts the user out of telemetry. Writes `telemetry.enabled: false` to the config file,
then **immediately drops** all buffered events and deletes any spill files. No pending
data is sent after the user withdraws consent.

```bash
$ mytool telemetry disable
Telemetry disabled.
All pending events have been discarded.
```

### `telemetry status`

Displays the current telemetry state and the anonymised machine ID.

```bash
$ mytool telemetry status
Telemetry: enabled
Machine ID: 4a3f8c1d9e2b6f70
```

Possible states:

| Output | Meaning |
|--------|---------|
| `Telemetry: disabled` | User has not opted in |
| `Telemetry: enabled` | Telemetry active, events sent to remote backend |
| `Telemetry: enabled (local-only)` | Events written to local file only |

### `telemetry reset`

Clears all local telemetry data, sends a GDPR data deletion request to the remote backend,
and disables telemetry. This is the user's path to exercising their right to erasure.

```bash
$ mytool telemetry reset
Deletion request sent for machine ID: 4a3f8c1d9e2b6f70
Local telemetry data cleared. Telemetry disabled.
```

The deletion request is sent via the tool author's configured `DeletionRequestor`. If none
is configured, a `data.deletion_request` event is sent through the existing backend as a
fallback. If the request fails, the user is informed and directed to the help channel.

**What gets cleared:**

- In-memory event buffer
- Spill files on disk
- Local-only telemetry log file

## Non-Interactive Usage

### Environment Variables

For CI/CD pipelines and scripted setup, use environment variables instead of interactive commands:

```bash
# Enable telemetry without prompting
TELEMETRY_ENABLED=true mytool init

# Disable at runtime
TELEMETRY_ENABLED=false mytool some-command

# Local-only mode (no remote transmission)
TELEMETRY_LOCAL=true mytool some-command
```

### Init Integration

When `TelemetryCmd` is enabled alongside `InitCmd`, the `init` command prompts the user
to opt into telemetry. The `--skip-telemetry` flag suppresses this prompt (defaults to
`true` when `CI=true`).

```bash
# Skip telemetry prompt in CI
mytool init --skip-telemetry

# Pre-answer via environment
TELEMETRY_ENABLED=true mytool init
```

## Related Documentation

- [Telemetry Component](../telemetry.md) — backends, privacy controls, event types, and architecture
- [Props](../props.md) — `Collector` field and `TelemetryConfig`
