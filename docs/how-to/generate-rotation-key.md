---
title: Generate the rotation-authority key
description: Mint the offline "break-glass" Ed25519 keypair that authorises future signing-key rotations, with no external tool required.
date: 2026-06-08
tags: [how-to, keys, generate, rotation, ed25519, offline-storage]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generate the rotation-authority key

The rotation-authority key is your **break-glass key**. It's never
used in normal operation. It exists so that — if the primary signing
key is ever lost or compromised — you can authorise a new signing key
without breaking the chain of trust for installs that already have
your tool.

This is a 5-minute one-off operation. Do it now, store the private
half offline, and forget about it (until you don't have to).

## Prerequisites

- `gtb` installed.
- A trusted workstation. Anywhere is fine — the private key is
  encrypted and the operator's workstation is the only place it
  briefly exists in memory. No `gpg` install, no `openssl`, no
  external tools.
- A safe place to put the private half. A home safe with an
  encrypted USB stick *and* a printed paper backup is the standard
  pattern.

## The command

```sh
gtb keys generate \
    --algorithm ed25519 \
    --name "MyTool Rotation Authority" \
    --email rotation@mytool.example \
    --output rotation-authority.asc
```

Two files appear in the current directory:

- `rotation-authority.asc` — the **armored OpenPGP public** half. Safe to
  commit. You'll embed this in your tool's
  `internal/trustkeys/keys/` and publish it alongside your signing
  key via WKD.
- `rotation-authority.priv.asc` — the **armored OpenPGP secret** half.
  Compatible with `gpg --import` for inspection. **This file is
  what needs to go offline.**

!!! warning "No silent overwrites"
    `gtb keys generate` refuses to overwrite an existing key file (public or
    private). A private key on disk is irreplaceable, so the default is to
    fail rather than clobber it in place. Pass `--force` only when you
    deliberately want to overwrite an existing key. The private half is always
    written with `0600` permissions.

A successful run logs:

```
INFO  Generated OpenPGP keypair  algorithm=ed25519  public_output=rotation-authority.asc
                                 private_output=rotation-authority.priv.asc
                                 creation_time=2026-06-08T12:00:00Z
                                 fingerprint=A1B2C3D4...
WARN  Move the private-half file to offline storage now.  private_output=rotation-authority.priv.asc
```

## Move the private half offline

Take the recommended belt-and-braces approach — two storage
locations to survive a single-medium failure:

### 1. Encrypted USB stick (covers USB bit-rot via the paper backup)

```sh
# Plug in a USB stick. Find its device name via `lsblk`. CAREFULLY.
# This wipes the device.
sudo cryptsetup luksFormat /dev/sdX             # set a strong passphrase
sudo cryptsetup luksOpen   /dev/sdX rotation-usb
sudo mkfs.ext4 /dev/mapper/rotation-usb
sudo mkdir -p /mnt/rotation
sudo mount /dev/mapper/rotation-usb /mnt/rotation

# Copy. The .asc file is small — milliseconds.
sudo cp rotation-authority.priv.asc /mnt/rotation/

# Tidy up.
sudo umount /mnt/rotation
sudo cryptsetup luksClose rotation-usb
```

Label the USB stick with the **fingerprint** (top 8 hex chars are
enough) so future-you can match the stick to the right key without
unlocking it first.

### 2. Paper backup (covers USB bit-rot)

Install `paperkey` once on your operating system (Debian/Ubuntu:
`apt install paperkey`; macOS: `brew install paperkey`). It also
needs `gpg` available, only to strip ASCII armor from the input —
no keyring is created.

```sh
# paperkey only accepts binary OpenPGP packets, not ASCII armor, so
# dearmor on the fly. No intermediate file touches disk.
gpg --dearmor < rotation-authority.priv.asc \
  | paperkey --output rotation-authority.paperkey.txt
lpr rotation-authority.paperkey.txt        # or any printer; print it on paper
shred -u rotation-authority.paperkey.txt   # wipe the temporary file
```

Tape the printout into a notebook or laminate it into a sleeve. Put
it next to the USB stick in the safe. Write the fingerprint on the
outside of the sleeve too.

### 3. Wipe the local copy

```sh
shred -u rotation-authority.priv.asc
```

The file is now in two physically-redundant places. If anyone asks,
the answer to "where's the rotation key" should be "in the safe",
not "let me grep my home directory".

## What to put in your password manager

Two entries, both clearly labelled **"DO NOT USE except for key
rotation"**:

- The LUKS passphrase for the encrypted USB.
- The fingerprint of the rotation-authority key.

Both are needed to recover. Don't put them in the same vault folder
as your everyday secrets — clutter is the enemy of "I know what
this is when I haven't looked at it in 3 years".

## Test the paper backup once, before you walk away

It's much easier to discover that your printer eats a stripe of pixels
*now* than 18 months from now when you actually need to recover.
With a different offline machine (or a fresh `$GNUPGHOME` on the
same one):

```sh
mkdir -p /tmp/test-recovery
export GNUPGHOME=/tmp/test-recovery
chmod 700 $GNUPGHOME

# Type the paperkey output back in by hand. Yes, all of it.
cat > retyped.txt
# ... type the printed bytes; Ctrl-D when done ...

# paperkey --pubring also wants binary, so dearmor the public half
# the same way as during backup.
gpg --dearmor < rotation-authority.asc > rotation-authority.pub.gpg
paperkey --pubring rotation-authority.pub.gpg --secrets retyped.txt | gpg --import
shred -u rotation-authority.pub.gpg retyped.txt

# Compare:
gpg --list-secret-keys --keyid-format long
# Should show the same fingerprint as `rotation-authority.asc`.
```

If the fingerprints match, the paper backup is recoverable. Wipe
`/tmp/test-recovery` and forget it ever happened.

If the fingerprints **don't** match, your printer or your typing
introduced an error. Reprint and retest before declaring the paper
backup good.

## Embed the public half

Drop `rotation-authority.asc` into your tool's `internal/trustkeys/keys/`
directory alongside the signing key. Go's `//go:embed all:keys`
directive picks it up; it ships baked into every binary.

```
mytool/
└── internal/
    └── trustkeys/
        ├── trustkeys.go            ← uses //go:embed all:keys
        └── keys/
            ├── release.asc         ← the signing key
            └── rotation-authority.asc        ← this file
```

## Publish via WKD too

The verifier cross-checks the embedded key against the WKD-served
copy, so the rotation key needs to be at the WKD endpoint as well.
See the [phase2-signing-prep doc](../development/phase2-signing-prep.md)
for the Cloudflare Pages Direct Upload recipe.

## What happens when you need to use it

(Hopefully never. But: signing-key compromise, lost KMS access,
deliberate algorithm migration.)

The rotation flow itself is operational and lives outside this
how-to. The short version: you sign a `rotate-keys.json` manifest
with the rotation-authority key naming the new signing key, ship it
alongside your next release, and existing installs adopt the new key
on next update. Detailed runbook: TBD (Phase 4 of the parent
[remote-update-checksum-verification](../development/specs/2026-04-02-remote-update-checksum-verification.md)
spec).

The whole point of generating the rotation key now is that it
exists *before* you need it. The runbook can come later.

## See also

- [Mint the signing key](mint-signing-key.md) — the everyday signing
  key flow, paired with this rotation key in the trust set.
- [Release-binary signing concept](../explanation/concepts/release-binary-signing.md)
  — why two keys, what each protects against.
