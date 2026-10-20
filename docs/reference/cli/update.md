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

Downloads and installs the latest version of the tool. After replacing the binary, it runs the new binary's `init --ci --skip-key` on each existing configuration directory to refresh the config; under `--ci` and off a terminal, `init` skips every credential wizard, so no profile's skip flag is needed.

When the running binary is already the latest release (or is a development build and `--force` was not given), nothing is replaced and the command says so: `already running the current version; nothing to update`, exit 0, and `--output json` reports `"updated": false`. "Update complete" is printed only when the file on disk was replaced.

## Package-managed installs

When the binary was installed by a package manager, `update` refuses to replace
it and exits non-zero, naming the command that upgrades it instead:

| Installed by | Recognised by | Upgrade with |
|---|---|---|
| Homebrew cask | the binary resolves into a `Caskroom/<token>/<version>/` directory | `brew upgrade --cask <token>` |
| Homebrew formula | the binary resolves into a `Cellar/<formula>/<version>/` directory | `brew upgrade <formula>` |
| a `.deb` package | `dpkg-query -S` names a package owning it | `sudo apt-get install --only-upgrade <package>` |
| an `.rpm` package | `rpm -qf` names a package owning it | `sudo dnf upgrade <package>` |

Replacing such a binary would leave the package manager's record describing a
file that is no longer on disk, so its next upgrade or uninstall would act on
the wrong one. Symlinks are followed first, so Homebrew's link in its `bin`
directory counts as the Caskroom or Cellar file it points at. dpkg and rpm are
asked only about a binary under `/usr`, `/bin`, `/sbin` or `/opt` (never
`/usr/local`), and only when their query tool is on `PATH`.

The refusal applies to `--force`, `--version` and `--from-file` alike: those
flags choose what to install, not whether a package-managed binary may be
replaced. It applies to every tool built on GTB, since they share the updater.
A tool installed by the install script, into `$HOME/.local/bin`, updates
itself as before.

## Flags

- `--force, -f`: Force update to the latest version even if already up to date.
- `--version, -v string`: Specific version to update to, as a release tag: `v1.2.3`, or a prerelease such as `v1.2.0-rc.1`. The shorthands `v1` and `v1.2`, build metadata (`+x`) and an unprefixed `1.2.3` are refused.
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
