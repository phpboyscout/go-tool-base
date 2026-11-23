---
title: Generate the rotation-authority key
description: Mint the offline "break-glass" Ed25519 key that signs key-rotation manifests, embed its minisign public key in your tool, and store the private half offline, with no external tool required.
date: 2026-06-08
tags: [how-to, keys, generate, rotation, ed25519, minisign, offline-storage]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generate the rotation-authority key

The rotation-authority key is your **break-glass key**. It's never
used in normal operation. It exists so that, if a release signing key
is ever lost or compromised, you can sign a rotation manifest that
tells installed copies of your tool which release keys to trust,
without asking anyone to reinstall. That works on
[the channels that carry the manifest](#which-channels-carry-the-manifest);
GitLab releases don't yet.

This is a 5-minute one-off operation. Do it now, store the private
half offline, and forget about it (until you don't have to). A copy
of your tool can only follow a manifest if it was built with this
key embedded, so the key has to exist well before the day you need it.

## Prerequisites

- `sigillum` v0.6.0 or later (`go install gitlab.com/phpboyscout/sigillum/cmd/sigillum@latest`, or a binary from the [sigillum releases](https://gitlab.com/phpboyscout/sigillum/-/releases)). v0.6.0 added `keys rotate`, the command this key exists for.
- A trusted workstation. Anywhere is fine: the operator's workstation
  is the only place the private key briefly exists in memory. No `gpg`
  install, no `openssl`, no external tools.
- A safe place to put the private half. A home safe with an
  encrypted USB stick *and* a printed paper backup is the standard
  pattern.

## The command

```sh
sigillum keys generate \
    --algorithm ed25519 --private-format pem \
    --name "MyTool Rotation Authority" \
    --email rotation@mytool.example \
    --output manifest-authority.asc
```

`--private-format pem` matters. A rotation manifest is signed by
`sigillum keys rotate`, whose `local` backend reads a PKCS#8 PEM and
refuses the default armored OpenPGP secret with `unsupported key
type`. Generate the key this way the first time, because the private
half goes into a safe afterwards.

Two files appear in the current directory:

- `manifest-authority.pem`: the **PKCS#8 PEM private** half, three
  lines of text. **This file is what needs to go offline.**
- `manifest-authority.asc`: an OpenPGP public key for the same
  keypair. Rotation doesn't use it, so delete it.

The `manifest-authority` name keeps these files apart from the OpenPGP
`rotation-authority.asc` that [Publish your public key](../tutorials/publish-your-public-key.md)
creates. That is a different key, and the signing tutorials still need it.

!!! warning "No silent overwrites"
    `sigillum keys generate` refuses to overwrite an existing key file (public or
    private). A private key on disk is irreplaceable, so the default is to
    fail rather than clobber it in place. Pass `--force` only when you
    deliberately want to overwrite an existing key. The private half is always
    written with `0600` permissions.

Now take the minisign public key from it. This is what your tool
checks a rotation manifest against:

```sh
sigillum keys minisign --backend local --key-id manifest-authority.pem \
    > manifest-authority.pub
```

The key goes to stdout as one `RW…` line. The log line on stderr names
its `minisign_key_id`, a 16-character hex ID. Note that ID down: it
labels the backups below.

The private half can live in a signing backend instead, such as an
AWS KMS Ed25519 key. `keys minisign` and `keys rotate` take
`--backend aws-kms` for it. The rest of this page is for the offline
key.

## Embed the public key

Commit `manifest-authority.pub` to your tool's
`internal/trustkeys/rotation/` directory:

```
mytool/
└── internal/
    └── trustkeys/
        ├── trustkeys.go                  ← embeds the key directories
        ├── trustkeys_test.go             ← fails on a key embedded twice
        ├── keys/
        │   └── release.asc               ← the signing key
        └── rotation/
            └── manifest-authority.pub    ← this file
```

Every `*.pub` file there is embedded at build time, and the generated
root passes them as `props.Tool.Signing.RotationAuthorityKeys`. With
the directory empty your tool never reads a manifest, so this file is
what turns rotation on. A minisign key never joins the release trust
set, so it can't sign a release.

Don't put it in `keys/`. Every key there may sign a release.

A project generated before gtb had `rotation/` gets it from
`gtb regenerate project`. Copies of your tool built before the key was
embedded can't apply a manifest, and keep the trust they shipped with.

!!! note "An existing OpenPGP authority key"
    [Publish your public key](../tutorials/publish-your-public-key.md), and
    earlier versions of this page, create an OpenPGP rotation-authority key
    that goes in `internal/trustkeys/authority/`, where the generated root
    passes it as `props.Tool.Signing.AnchorOnlyKeys`. Keep it there while WKD
    buckets your shipped binaries read still list it, because the embedded and
    WKD key sets must match. It can't sign a manifest, so generate this key as
    well.

## Move the private half offline

Take the recommended belt-and-braces approach, two storage
locations to survive a single-medium failure:

### 1. Encrypted USB stick

```sh
# Plug in a USB stick. Find its device name via `lsblk`. CAREFULLY.
# This wipes the device.
sudo cryptsetup luksFormat /dev/sdX             # set a strong passphrase
sudo cryptsetup luksOpen   /dev/sdX rotation-usb
sudo mkfs.ext4 /dev/mapper/rotation-usb
sudo mkdir -p /mnt/rotation
sudo mount /dev/mapper/rotation-usb /mnt/rotation

sudo cp manifest-authority.pem /mnt/rotation/

# Tidy up.
sudo umount /mnt/rotation
sudo cryptsetup luksClose rotation-usb
```

Label the USB stick with the **minisign key ID** so future-you can
match the stick to the right key without unlocking it first.

### 2. Paper backup (covers USB bit-rot)

An Ed25519 PEM is three short lines, so print it as it is:

```sh
lpr manifest-authority.pem        # or any printer; print it on paper
```

Tape the printout into a notebook or laminate it into a sleeve. Put
it next to the USB stick in the safe. Write the minisign key ID on the
outside of the sleeve too.

### 3. Test the paper backup once, before you walk away

It's much easier to discover that your printer eats a stripe of pixels
*now* than 18 months from now when you actually need to recover. Type
the printout back in by hand, then check it gives the same key:

```sh
umask 077                 # the retyped file holds the private key
cat > retyped.pem
# ... type the three printed lines; Ctrl-D when done ...

sigillum keys minisign --backend local --key-id retyped.pem
# Should print the same RW… line as manifest-authority.pub.
shred -u retyped.pem
```

If the lines match, the paper backup is recoverable. If they don't,
your printer or your typing introduced an error. Reprint and retest
before declaring the paper backup good.

### 4. Wipe the local copy

```sh
shred -u manifest-authority.pem
```

The file is now in two physically-redundant places. If anyone asks,
the answer to "where's the rotation key" should be "in the safe",
not "let me grep my home directory".

## What to put in your password manager

Two entries, both clearly labelled **"DO NOT USE except for key
rotation"**:

- The LUKS passphrase for the encrypted USB.
- The minisign key ID of the rotation-authority key.

Both are needed to recover. Don't put them in the same vault folder
as your everyday secrets. Clutter is the enemy of "I know what
this is when I haven't looked at it in 3 years".

## What happens when you need to use it

(Hopefully never. But: signing-key compromise, lost KMS access,
deliberate algorithm migration.)

You sign a rotation manifest with this key: `sigillum keys rotate`,
with `--backend local --key-id` pointing at the recovered PEM. It
writes `rotate-keys.json` and `rotate-keys.json.minisig`, which add
replacement release keys and, after a compromise, revoke the old one.
Commit both files to `release/rotation/` in your repository, and every
release from then on carries them, on the channels that can.

### Which channels carry the manifest

- **The static channel** stages them into the release and lists them in
  `release.json`.
- **GitHub, Gitea and Codeberg, on the forge channel,** attach them to the
  release, once signing is on. A goreleaser `before` hook checks them first, by
  the same rules the static channel applies, and stops the release if they fail.
- **GitLab, on the forge channel,** doesn't carry them yet. colophon creates
  the release and links only what goreleaser recorded, and an uploaded extra
  file isn't recorded
  ([colophon#76](https://gitlab.com/phpboyscout/colophon/-/work_items/76)).
  Until that lands, a GitLab project that may need to rotate should release on
  the static channel.
- **Bitbucket** can't release on the forge channel, because goreleaser has no
  Bitbucket release support.

A signed project gets the two lines in `.goreleaser.yaml` from
`gtb generate project`, and from `gtb regenerate project` when the file is
unedited. `gtb enable signing` adds them to an edited file too, leaving the rest
of it as it was, and `gtb disable signing` removes them. So a project whose
regenerate kept its own `.goreleaser.yaml` picks them up by running
`gtb enable signing` again. gtb leaves the file alone and prints both lines
for you to add when it can't place them safely, which covers:

- a file in `.gtb/ignore`, or one with a `signs:` block of your own;
- a file that repeats a key anywhere, which goreleaser refuses to load;
- a `before:` whose first entry isn't `hooks:`, or a `release:` that already
  lists `extra_files`;
- a `before:` or `release:` key written any other way: with anything after
  the colon (a comment, an anchor, a flow-style value), quoted, behind a byte
  order mark, or in a later YAML document;
- a `before:` with no value, or hooks that aren't plain strings;
- any edit that would change another value in the file, such as a
  `release:` block ending in a `|+` scalar;
- CRLF line endings, or a file without a final newline;
- a file that has one of the two lines but not the other, or has their text
  only in comments or anywhere other than `before.hooks` and
  `release.extra_files`.

The lines to add are:

```yaml
before:
  hooks:
    - go tool releasemanifest --verify-only --rotation release/rotation --rotation-authority internal/trustkeys/rotation

release:
  extra_files:
    - glob: release/rotation/rotate-keys.json{,.minisig}
```

`gtb disable signing` removes only the lines gtb wrote, with their comments,
and keeps any hooks or release files you added beside them. It leaves the
file alone and prints both lines for you to remove when they no longer read
exactly as gtb wrote them (you edited a line or its comment), or when taking
them out would change anything else in the file. The CRLF, final-newline and
later-document cases above apply to it too.

`--verify-only` needs go-tool-base v0.53.0 or later in the project's `go.mod`,
plus the `releasemanifest` line in its `tool` block, which a regenerate adds.

Each copy with this key embedded
applies the manifest on its next update, before it checks that
release's signature, so a release signed by the replacement key
installs on the same update.

- [Issue a rotation manifest](https://sigillum.phpboyscout.uk/how-to/issue-a-rotation-manifest/)
  in the sigillum docs is the runbook, covering the fire drill, a
  scheduled rotation and a compromise.
- [Emergency rotation: the rotation manifest](../explanation/components/setup/signature-verification.md#emergency-rotation-the-rotation-manifest)
  describes what the updater does with it.
- The [static release channel reference](../reference/static-release-channel.md)
  describes how a committed manifest reaches each release, and the
  checks that stop a release carrying one that is unsigned or about to
  expire.

Run the fire drill once the key is embedded and released: an empty
manifest, published on an ordinary release. It changes nothing, and
it proves the key, the pipeline and the updater work before the day
you need them.

## See also

- [Mint the signing key](mint-signing-key.md): the everyday signing
  key flow.
- [Release-binary signing concept](../explanation/concepts/release-binary-signing.md):
  why two keys, and what each protects against.
- [Rotate your signing key](../tutorials/rotate-your-signing-key.md):
  the scheduled rotation, where the old key signs through an overlap
  and no manifest is needed.
