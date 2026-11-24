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

The pre-run check leaves such a binary alone: no check, no reminder that a newer
version exists, and no block under the `enabled` update policy. The package
manager says when there is something to upgrade to.

## Installs a manager declares

A manager gtb cannot recognise by path, such as mise, asdf or aqua, says so
through the environment instead:

| Variable | Applies to | Holds |
|---|---|---|
| `GTB_INSTALL_MANAGER` | every tool built on GTB | the manager's name, such as `mise` |
| `GTB_INSTALL_UPGRADE` | every tool built on GTB | the command that upgrades the tool, printed and never run |
| `<PREFIX>_INSTALL_MANAGER` | one tool, by its env prefix | overrides `GTB_INSTALL_MANAGER` |
| `<PREFIX>_INSTALL_UPGRADE` | one tool, by its env prefix | the upgrade command beside `<PREFIX>_INSTALL_MANAGER` |

With a manager named, `update` refuses before it reads the pin, asks the
release source or prompts. That holds for `--force`, `--version`,
`--rollback`, `--pin=<tag>` and `--from-file` alike:

```
widget is managed by mise (GTB_INSTALL_MANAGER), so it does not update itself
hint: Upgrade it with: mise upgrade widget
```

The tool's own pair wins over the framework's as a pair, so a per-tool manager
is never described with the framework-wide upgrade command. A value that is
empty or only whitespace counts as unset. Before printing, control characters
are escaped as `\uXXXX` and a backslash is doubled, so a Windows path in the
upgrade command shows as `C:\\tools`. The prefix is matched as the config
layer matches it, so an `EnvPrefix` of `widget` or `WIDGET_` reads
`WIDGET_INSTALL_MANAGER`. Only the environment counts. A config file, project file or
flag cannot set these, because the signal belongs to whatever installed the
binary.

Bare `--pin` and `--unpin` install nothing, so they still work. The update
check and `version` go quiet too: see
[Configure self-updating](../../how-to/configure-self-updating.md#installs-a-package-manager-keeps).

## Flags

- `--force, -f`: Force update to the latest version even if already up to date.
- `--version, -v string`: Specific version to update to, as a release tag: `v1.2.3`, or a prerelease such as `v1.2.0-rc.1`. The shorthands `v1` and `v1.2`, build metadata (`+x`) and an unprefixed `1.2.3` are refused.
- `--from-file string`: Path to a local `.tar.gz` release archive for offline installation.
- `--rollback[=tag]`: Roll back to the release before the running one, or to the named older release with `--rollback=v1.1.0`, and pin it. The tag must be attached with `=`.
- `--pin[=tag]`: Stay on the running version, with no network call. `--pin=v1.3.0` installs that release, newer or older, and stays on it.
- `--unpin`: Clear the pin, so `update` and the update check resume. No network call.
- `--allow-version-mismatch`: Install a release whose binary is not the version asked for, with a warning. See [Version check](#version-check).

`--from-file`, `--version`, `--rollback`, `--pin` and `--unpin` are mutually exclusive. `--force` and `--allow-version-mismatch` combine with `--version`, `--rollback` and `--pin`. `--allow-version-mismatch` is refused beside `--from-file`.

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

When the tool's config directory is inside a git checkout, as a CI job's home directory often is, no pin is kept or
read there, because the repository could have supplied it. `--pin` and `--rollback` exit non-zero with a hint before
installing anything, a pin file found there is ignored with a warning, and `--unpin` has nothing to clear and leaves
any pin file there in place. Name the version with `--version` instead.

## Version check

After the checksum and signature checks, and before the binary is replaced, `update` reads the version Go recorded in
the downloaded binary. It must be the version asked for: the one given to `--version`, `--pin` or `--rollback`, or the
latest release's tag. Build metadata such as `+dirty` is ignored.

| The binary records | Result |
|---|---|
| the version asked for | installs |
| another version | refused, with both versions in the hint. `--allow-version-mismatch` installs it with a warning |
| no release version, and `update.require_signature` applies | refused. No flag changes that |
| no release version otherwise | installs with a warning |

A release whose tag is not a full version, such as `v1.2.3`, leaves nothing to compare, so it's treated as one whose
binary records none.

A release fetched by tag must also carry that tag itself, or it is refused, and no flag changes that. A refusal exits
non-zero and leaves the installed binary in place. `--force` has no effect on any of this, and `--from-file` is not
checked, because a local archive has no tag to compare with.

A refused release is either one relabelled on its way to you or a mistake by its publisher. Report it to the
publisher. Where signatures are not required, the recorded version is only as trustworthy as the download.

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
