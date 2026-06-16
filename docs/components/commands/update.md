---
title: Update Command
description: Self-update mechanism to download and install the latest version of the tool.
date: 2026-02-16
tags: [components, commands, update, self-update]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Update Command

The `update` command updates the tool to the latest or specified version.

## Usage

```bash
mytool update [flags]
```

## Description

Downloads and installs the latest version of the tool. After updating, it automatically runs `init` on existing configuration directories to ensure compatibility.

## Flags

- `--force, -f`: Force update to the latest version even if already up to date.
- `--version, -v string`: Specific version to update to (format: `v0.0.0`).
- `--from-file string`: Path to a local `.tar.gz` release archive for offline installation. Mutually exclusive with `--version`.

## Online Update Process

1. Validates version format (if specified).
2. Downloads the target version from GitHub/GitLab.
3. Replaces the current binary.
4. Updates configuration files in standard locations.
5. Displays release notes for the new version.

## Offline Update (Air-Gapped Environments)

When `--from-file` is provided, the command bypasses all network calls:

```bash
# Standard offline update
mytool update --from-file /path/to/mytool_Linux_x86_64.tar.gz
```

If a `.sha256` sidecar file exists alongside the tarball (e.g., `mytool_Linux_x86_64.tar.gz.sha256`), the checksum is verified before extraction. If no sidecar is present, a warning is logged and installation proceeds.

No VCS client, API token, or network access is required for offline updates.

## Implementation

The update command is implemented in `cmd/update/update.go`. Online updates use `pkg/setup.NewUpdater()` while offline updates use `pkg/setup.NewOfflineUpdater()`.

### Injecting updater factories (testing)

`NewCmdUpdate`, `Update`, and the offline path accept `UpdateConfigOption`s. To substitute the updater in tests — without touching any package-level state — pass a factory option:

```go
cmd := update.NewCmdUpdate(props,
    update.WithUpdater(func(ctx context.Context, p *props.Props, version string, force bool) (update.Updater, error) {
        return myFakeUpdater, nil
    }),
    update.WithOfflineUpdater(func(p *props.Props) update.Updater {
        return myFakeOfflineUpdater
    }),
)
```

Each call site receives its own factory, so concurrent (`t.Parallel`) tests cannot clobber one another.

!!! warning "Deprecated: `ExportNewUpdater` / `ExportNewOfflineUpdater`"
    The package-level vars `ExportNewUpdater` and `ExportNewOfflineUpdater` are **deprecated**. They are mutable global test seams that race under parallel tests. They still work (consulted as the default when no option is given) and are retained for one minor release; migrate to `WithUpdater` / `WithOfflineUpdater`.

### Error handling in `init` subcommands

The `init ai`, `init github`, and `init bitbucket` subcommands use cobra `RunE` and **return** configuration errors rather than calling `logger.Fatalf`. Returning the error routes it through the framework's standard error path — user-facing hints, the configurable `ExitFunc`, and the deferred telemetry flush all apply — instead of terminating the process abruptly and bypassing them.

## Background update checks — the ForcedUpdate policy

On every non-`--ci` invocation the root command may run a throttled update
check. Its behaviour is governed by a **three-state policy** so a background
check never silently hijacks an unrelated command:

| Policy | When a newer release is found |
|---|---|
| `disabled` | Log that an update is available and **continue**. No prompt, no block. **Framework default.** |
| `prompt` | Ask "update now?"; **decline** continues with the command, **accept** updates then asks you to re-run. The `gtb` CLI itself uses this. |
| `enabled` | **Block** every command until updated. A declined or unanswerable required update exits **non-zero** (never a masked exit 0). |

**Resolution precedence:** `--ci` / `ci: true` bypass the check entirely → then
the `update.policy` config key → then the tool author's baseline
(`props.Tool.UpdatePolicy`, default `disabled`).

```yaml
update:
  policy: prompt        # disabled | prompt | enabled   (empty = tool baseline)
  check_interval: 24h   # any Go duration; 0 = check every invocation
```

A tool author sets the baselines on the `Tool`:

```go
props.Tool{
    // ...
    UpdatePolicy:        props.UpdatePolicyPrompt,
    UpdateCheckInterval: 7 * 24 * time.Hour, // optional; zero = framework default (24h)
}
```

**Check-interval precedence:** a valid `update.check_interval` config value
(where `0`/`0s` means "check every run") → the `props.Tool.UpdateCheckInterval`
baseline (if positive) → the framework default (24h). A zero-value baseline is
treated as "unset" and falls through to 24h, so an "every run" cadence is only
reachable via runtime config, never as a compiled-in baseline.

**Persistent out-of-date reminder.** The latest version discovered by a check is
cached in the `last_checked` marker's body (its modtime still drives the
`check_interval` throttle — one file, two jobs). While the running binary is
behind that cached version, a single `WARN` is emitted on **every** invocation
(even when the network check is throttled), so a user who declined — or who runs
a `disabled`-policy tool — keeps being reminded to upgrade. `--ci` suppresses it.

**Failed updates exit non-zero.** A successful update that needs a restart exits
0 and asks you to re-run; a *failed* update (e.g. no release asset for the
platform) propagates a non-zero exit rather than masking the failure as success.

See `docs/development/specs/2026-06-16-forced-update-feature.md`.
