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
