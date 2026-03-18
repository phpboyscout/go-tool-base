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

## Update Process

1. Validates version format (if specified).
2. Downloads the target version from GitHub.
3. Replaces the current binary.
4. Updates configuration files in standard locations.
5. Displays release notes for the new version.

## Implementation

The update command is implemented in `cmd/update/update.go` and uses the `pkg/setup.NewUpdater()` system for downloading and installing updates.
