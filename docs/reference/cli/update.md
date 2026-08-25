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

## Downgrade behaviour

If the release source reports a "latest" version that is **older** than the
running binary (a stale or rolled-back release listing), the implicit
`update` refuses to install it and exits non-zero, signature and checksum
verification authenticate an artefact, not its recency, so an implicit
downgrade is treated as a potential rollback attack. To downgrade
intentionally, either:

- pin the target explicitly: `mytool update --version v1.1.0` (an explicit
  version is sufficient intent on its own), or
- force the implicit path: `mytool update --force`.
