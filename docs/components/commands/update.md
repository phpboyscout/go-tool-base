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
