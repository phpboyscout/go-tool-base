---
title: Version Command
description: Display version information and check for available updates.
date: 2026-07-23
tags: [components, commands, version]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Version Command

The `version` command displays version information and checks for available updates. It reads nothing from configuration, so it runs on a fresh install before any config file exists (it [skips the missing-config gate](../../explanation/components/setup/root-command.md#the-missing-config-gate)).

## Usage

```bash
mytool version [--check] [--output json]
```

## Description

Prints the current version, build commit, and build date of the tool, as
injected at build time via ldflags. Unless the update command is disabled or
this is a development build, it also contacts the **configured release source**
(GitLab or GitHub, per the tool's `props.Tool.ReleaseSource`) to report whether
a newer version is available.

The latest-version check is **best-effort**: when the release source is
unreachable (offline machine, firewalled network, source outage), the command
still prints the local build information and exits `0`, logging a single
warning (`failed to check latest version`) so you can see why the `Latest`
line is absent. The passive check is bounded by a short timeout
(`setup.VersionCheckTimeout`, 10 seconds), so a black-holing network cannot
stall the command for long.

Pass `--check` when you *want* hard semantics. A scriptable "is there an
update, and can you reach the release source" probe. With `--check`, a failed
lookup exits non-zero with `unable to fetch latest version`, a longer (60
second) timeout applies, and the check runs **even on development builds** so
maintainers can probe the release source from a dev build. The
disabled-update fast path is unaffected: when the `update` command feature is
disabled the network is never contacted.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--check` | `false` | Fail with a non-zero exit when the release source is unreachable; also checks on development builds. |
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

### Degraded JSON Output

When the check was attempted but the release source could not be reached, the
response is still a success envelope carrying the local fields, with an
explicit `check_failed` marker so scripts can distinguish "up to date" from
"could not check". `latest` is absent and `current` is `false` (the answer is
unknown, not "behind"):

```json
{
  "status": "success",
  "command": "version",
  "data": {
    "version": "v1.2.3",
    "commit": "abc123",
    "date": "2023-10-08T10:00:00Z",
    "current": false,
    "check_failed": true
  }
}
```

`check_failed` is omitted entirely on successful (or skipped) checks.

## Implementation

The version command is implemented in `pkg/cmd/version/version.go` and integrates with the updater system to check for newer releases. See the spec [version command: degrade gracefully when the release source is unreachable](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0173-version-command-offline-degradation) for the offline-degradation design.
