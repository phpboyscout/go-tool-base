---
title: Version Command
description: Display version information and check for available updates.
date: 2026-02-16
tags: [components, commands, version]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Version Command

The `version` command displays version information and checks for available updates.

## Usage

```bash
mytool version
```

## Description

Prints the current version, build commit, and build date of the tool. Additionally, it checks for newer versions available and notifies the user if an update is available.

## Output Example

```bash
$ mytool version
INFO version=v1.2.3 Build=abc123 Built On=2023-10-08T10:00:00Z
INFO You are running the latest version
```

### Update Notification

If a newer version is found on GitHub, the tool will display a warning:

```bash
$ mytool version
INFO version=v1.2.3 Build=abc123 Built On=2023-10-08T10:00:00Z
WARN A new version is available: v1.2.4
```

## Implementation

The version command is implemented in `pkg/cmd/version/version.go` and integrates with the updater system to check for newer releases.
