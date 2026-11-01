---
title: Ship self-updates
description: See how a generated CLI updates itself, watch it replace its own binary from a local archive, and make it refuse a download it cannot verify.
date: 2026-10-30
tags: [tutorial, update, release, checksum, cli]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Ship self-updates

By the end of this, you'll have watched `mytool` replace itself with a newer
build, and made it refuse an update it can't verify. Your tool has shipped an
`update` command since you scaffolded it. This page shows what it does, without
publishing a release anywhere.

Allow about fifteen minutes.

## Before you start

- **The `mytool` project** from [Build your first CLI](build-your-first-cli.md).
- **The same gtb release.** Everything below is verified against **v0.50.0**.
- **Linux or macOS.** The archive steps use `tar` and `sha256sum`. On macOS,
  use `shasum -a 256` in place of `sha256sum`.

## What `update` does

When a user runs `mytool update`, the tool:

1. asks its release source for the latest release. That's the `--repo` you
   gave the generator, so `myorg/mytool` on GitHub.
2. compares that version with its own, and stops if it's already current:
   `already running latest version, v1.2.0`.
3. downloads the archive for the user's platform.
4. verifies it against the release's checksums, and its signature if you sign
   releases.
5. replaces the running binary in place.
6. runs `init` from the new binary over the user's config, to add any keys the
   release introduced. Nothing is asked: the credential steps are skipped.

Until you publish a release, step 1 has nothing to find, so the tool warns on
every run:

```
WARN  failed to check latest version error="... 404 Not Found ..." hints="myorg/mytool has no release to update to yet. This is the expected answer for a tool whose first release has not been cut; it is not a configuration problem."
```

That's expected. The rest of this page uses an archive on disk instead.

## Build two versions

The release pipeline stamps the version into the binary at build time. Do the
same by hand, for an old build and a new one:

```bash
mkdir -p /tmp/mytool-demo/old /tmp/mytool-demo/new
go build -ldflags "-X github.com/myorg/mytool/internal/version.version=v1.0.0" \
  -o /tmp/mytool-demo/old/mytool ./cmd/mytool
go build -ldflags "-X github.com/myorg/mytool/internal/version.version=v1.1.0" \
  -o /tmp/mytool-demo/new/mytool ./cmd/mytool
```

Pack the new one the way a release does, as a `.tar.gz` holding the binary:

```bash
tar -C /tmp/mytool-demo/new -czf /tmp/mytool-demo/mytool_Linux_x86_64.tar.gz mytool
```

The `-X` path matches `.goreleaser.yaml` in your project. If your module path
differs, copy it from there.

## Update from the archive

`--from-file` installs from a local archive instead of the network. It's meant
for machines that can't reach your release source, and it runs the same
extract-and-replace path as a network update:

```bash
/tmp/mytool-demo/old/mytool version
/tmp/mytool-demo/old/mytool update --from-file /tmp/mytool-demo/mytool_Linux_x86_64.tar.gz
/tmp/mytool-demo/old/mytool version
```

```
Version: v1.0.0
...
WARN  no checksum sidecar found, skipping verification expected=/tmp/mytool-demo/mytool_Linux_x86_64.tar.gz.sha256
INFO  writing updated binary name=mytool path=/tmp/mytool-demo/old/mytool
INFO  Initialising configuration
INFO  Configuration file already exists; adding any keys it lacks
INFO  already configured component=greet
INFO  configuration initialised path=/home/you/.mytool/config.yaml
INFO  successfully installed from=/tmp/mytool-demo/mytool_Linux_x86_64.tar.gz to=/tmp/mytool-demo/old/mytool
...
Version: v1.1.0
```

The binary at `old/mytool` is now v1.1.0. The `init` run in the middle is step
6: the new build's config defaults merged into your file.

## Verify what you install

A binary that arrives from somewhere else is one you didn't build on this
machine. Give the archive a checksum, the way a release ships one beside each
download:

```bash
cd /tmp/mytool-demo
sha256sum mytool_Linux_x86_64.tar.gz > mytool_Linux_x86_64.tar.gz.sha256
cd -
```

Rebuild the old version and update again:

```bash
go build -ldflags "-X github.com/myorg/mytool/internal/version.version=v1.0.0" \
  -o /tmp/mytool-demo/old/mytool ./cmd/mytool
/tmp/mytool-demo/old/mytool update --from-file /tmp/mytool-demo/mytool_Linux_x86_64.tar.gz
```

```
INFO  checksum verified file=/tmp/mytool-demo/mytool_Linux_x86_64.tar.gz
...
INFO  successfully installed ...
```

`update` found the `.sha256` file beside the archive and checked it before
extracting anything. A mismatch stops the update, and nothing is replaced.

By default, a missing checksum file is only a warning. To make it a refusal,
add this to `~/.mytool/config.yaml`:

```yaml
update:
  require_checksum: true
```

Delete the `.sha256` file, rebuild the old version, and try once more:

```bash
rm /tmp/mytool-demo/mytool_Linux_x86_64.tar.gz.sha256
go build -ldflags "-X github.com/myorg/mytool/internal/version.version=v1.0.0" \
  -o /tmp/mytool-demo/old/mytool ./cmd/mytool
/tmp/mytool-demo/old/mytool update --from-file /tmp/mytool-demo/mytool_Linux_x86_64.tar.gz
```

```
ERROR update.require_checksum is enabled but no checksum sidecar was found ...
```

The binary is still v1.0.0. Take `require_checksum` out of your config when
you're done, so the rest of your work isn't refused.

A checksum proves the archive matches the checksum file published beside it.
It can't catch someone who controls your release host and replaces both. For
that you need signed releases, which check a signature against a key the
release host can't reach. A local archive carries no signature, so with
`update.require_signature` set, `--from-file` always refuses. See
[Configure self-updating](../how-to/configure-self-updating.md).

## The real loop

In production you don't build archives by hand. The scaffold ships a GoReleaser
config and a release pipeline, so the cycle is:

1. Tag a version and push the tag.
2. CI builds every platform's archive with its checksums, and publishes them as
   a release.
3. Your users run `mytool update`.

A few flags are worth knowing:

- `--version v1.3.0` installs a specific release.
- `--rollback` goes back to the release before this one, and pins the tool
  there until `--unpin`.
- `--pin` keeps the tool on its current version.

## Where this leaves you

Your tool can replace itself with a newer build, check what it downloads before
trusting it, and refuse an update it can't verify. Shipping a fix becomes
tagging a release.

## What to read next

- [Configure self-updating](../how-to/configure-self-updating.md): release
  sources, private repositories, signing and air-gapped installs.
- [update reference](../reference/cli/update.md): every flag, pinning and
  rollback.
