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
- `--from-file string`: Path to a local `.tar.gz` release archive for offline installation.
- `--rollback[=tag]`: Roll back to the release before the running one, or to the named older release with `--rollback=v1.1.0`, and pin it. The tag must be attached with `=`.
- `--pin[=tag]`: Stay on the running version, with no network call. `--pin=v1.3.0` installs that release, newer or older, and stays on it.
- `--unpin`: Clear the pin, so `update` and the update check resume. No network call.

`--from-file`, `--version`, `--rollback`, `--pin` and `--unpin` are mutually exclusive. `--force` combines with `--version`, `--rollback` and `--pin`.

## Rollback and pin

`update --rollback` with no tag installs the highest released version below the running one, among the 20 newest
releases. From a stable version, prereleases are not candidates; from a prerelease, they are. The archive is checked
against the release's signed checksums exactly as a forward update is, and `update.require_checksum` and
`update.require_signature` apply unchanged.

Every downgrade a person asks for is confirmed first, at the terminal:
`Roll back mytool from v1.2.0 to v1.1.0? This installs an older release.`

| Downgrade by | At a terminal | Unattended (CI, or no terminal) |
|---|---|---|
| `--rollback` | asks | refused unless `--force` |
| `--version` or `--pin=<tag>` naming an older release | asks | proceeds, as before |
| any of them with `--force` | does not ask | does not ask |

A declined or refused downgrade exits non-zero and replaces nothing.

A successful rollback pins the version it installed. A pin is honoured as follows:

| Command | While pinned |
|---|---|
| `update` | does nothing, exits 0 and names the pin and `--unpin`. `--output json` reports `"updated": false` and `"pinned"` |
| `update --force` | updates to the latest release and moves the pin to it |
| `update --version X`, `--rollback[=X]`, `--pin=X` | installs X and moves the pin to X, after confirmation when X is older |
| `update --unpin` | clears the pin |

The pre-run update check leaves a pinned tool alone: no check, no reminder, and no block under the `enabled` update
policy. The pin moves only after a successful install, so a failed or declined one leaves it where it was.

On the static release channel, only releases published since the tool moved to the channel can be fetched. An older
release named with `--rollback=<tag>` or `--version` is not found, and the error says to install it from its archive
with `--from-file`.

A binary older than the release that introduced pins does not read the pin. Rolling back to one leaves a pin its
update check ignores.

## Downgrade behaviour

If the release source reports a "latest" version that is **older** than the
running binary (a stale or rolled-back release listing), the implicit
`update` refuses to install it and exits non-zero, signature and checksum
verification authenticate an artefact, not its recency, so an implicit
downgrade is treated as a potential rollback attack. To downgrade
intentionally, either:

- roll back: `mytool update --rollback`, which asks first and then pins the
  older release (see [Rollback and pin](#rollback-and-pin)),
- name the target: `mytool update --version v1.1.0`, which asks first at a
  terminal and proceeds unattended, or
- force the implicit path: `mytool update --force`.
