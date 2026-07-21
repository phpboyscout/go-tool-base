---
title: Version Command
description: Display version information and check for available updates.
date: 2026-02-16
tags: [components, commands, version]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Version Command

The `version` command displays version information and checks for available updates. It reads nothing from configuration, so it runs on a fresh install before any config file exists (it [skips the missing-config gate](../../explanation/components/setup/root-command.md#the-missing-config-gate)).

## Usage

```bash
mytool version [--output json]
```

## Description

Prints the current version, build commit, and build date of the tool, as
injected at build time via ldflags. Unless the update command is disabled or
this is a development build, it also contacts the **configured release source**
(GitLab or GitHub, per the tool's `props.Tool.ReleaseSource`) to report whether
a newer version is available.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `text` | Output format: `text` or `json`. (Global persistent flag.) |

## Output Example

Plain text (the default), one field per line. `Build` and `Date` are printed
only when injected at build time:

```bash
$ mytool version
Version: v1.2.3
Build:   abc123
Date:    2023-10-08T10:00:00Z
```

### Update Notification

If the configured release source reports a newer version, a `Latest` line is
appended:

```bash
$ mytool version
Version: v1.2.3
Build:   abc123
Date:    2023-10-08T10:00:00Z
Latest:  v1.2.4 (update available)
```

### JSON Output

With `--output json`, the command emits a structured envelope whose `data`
field is a typed `VersionInfo` payload:

```json
{
  "status": "success",
  "command": "version",
  "data": {
    "version": "v1.2.3",
    "commit": "abc123",
    "date": "2023-10-08T10:00:00Z",
    "latest": "v1.2.4",
    "current": false
  }
}
```

`commit`, `date`, and `latest` are omitted when empty; `current` is `true` when
the running version matches the latest release (or when the update check is
skipped).

## Implementation

The version command is implemented in `pkg/cmd/version/version.go` and integrates with the updater system to check for newer releases.
