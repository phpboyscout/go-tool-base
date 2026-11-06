---
title: Rotate your signing key
description: Move a gtb-generated tool's release signing to a new KMS key without breaking a single installed copy, using a new WKD identity per key generation, fingerprint-pinned re-mints, and a dual-signed overlap window.
date: 2026-11-02
tags: [tutorial, signing, rotation, kms, wkd, openpgp]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Rotate your signing key

By the end of this you'll have moved release signing from your v1 KMS key to
a new v2 key, and every copy of your tool already installed will have kept
verifying its updates throughout. Keys get rotated on a schedule, or because
one is suspect, or because the account holding it is going away. This is how
to do it without locking anyone out.

This is the last part of the signing series. It assumes everything the earlier
parts built: a KMS key, OIDC signing in CI, keys published on WKD, enforcement
in your tool, and GoReleaser signing every release.

Allow an afternoon for the ceremony and the first release, then weeks of
overlap before you retire the old key. The procedure is the one gtb itself
followed for its own rotation in July 2026.

## Why rotation needs a procedure

There's no automatic rotation for a signing key, and the module turns it off
on purpose. A new key means a new public half, and copies of the old public
half are compiled into every binary you've shipped. You can't change those.

Two rules fall out of how your tool checks updates:

- **The embedded keys and the WKD keys must match exactly.** The tool fetches
  the WKD keys for the email compiled into it and refuses any update if their
  fingerprints differ from the embedded set. That refusal is always fatal.
- **A shipped binary's embedded set never changes.** So the WKD bucket it reads
  must never change either.

Put together: once you've published a WKD bucket, it's frozen. Adding v2 to it
would make every installed copy refuse every update. So each generation of
your tool gets its own WKD identity. Old binaries keep reading the frozen
`release@acme.dev` bucket. New binaries read a new `release-v2@acme.dev`
bucket that holds the larger set.

## Before you start

- **The v2 KMS key**: a second instance of the module, named
  `acme-release-signing-v2`, applied as in
  [Put the signing key in AWS KMS](put-the-signing-key-in-kms.md). Don't edit
  the v1 module: its `name` and `key_spec` are fixed, and v1 has to keep
  signing until the end.
- **An inventory of every key you've embedded**: fingerprint, user ID email,
  and creation time for each. For a key file:

```bash
gpg --show-keys --with-colons signing-key-v1.asc | \
  awk -F: '/^pub/{print "created:", strftime("%Y-%m-%dT%H:%M:%SZ", $6, 1)}
           /^fpr/{print "fpr:", $10} /^uid/{print "uid:", $10}'
```

- **A copy of your published WKD tree** that's byte-identical to what's served.
  Check each file's `sha256sum` against the same file fetched with `curl`.
- **The rotation authority's private half**, out of offline storage, for one
  step.
- **The same gtb release.** Everything below is checked against **v0.50.0**,
  with local keys standing in for KMS.

## Re-mint v1 under the new identity

A key only appears in a WKD bucket if one of its user IDs names that bucket's
email. So v1 has to gain the `release-v2@acme.dev` identity, without becoming a
different key. Mint it again with the new email and the **original** creation
time from your inventory:

```bash
gtb keys mint \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/acme-release-signing-v1 \
    --name "Acme Releases" \
    --email release-v2@acme.dev \
    --created "2026-06-02T00:00:00Z" \
    --output signing-key-v1.asc
```

The fingerprint it logs must exactly match v1's fingerprint from the
inventory. If it doesn't, the `--created` value is wrong. Fix it before going
any further. Nothing has been published yet, so a mistake here costs nothing.

## Mint v2

The new key gets the new identity and no `--created` pin, because it's new:

```bash
gtb keys mint \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/acme-release-signing-v2 \
    --name "Acme Releases" \
    --email release-v2@acme.dev \
    --output signing-key-v2.asc
```

Record the creation time it logs beside the alias. It's now part of v2's
fingerprint for good.

## Give the rotation authority the new identity

The rotation authority needs the new UID too, keeping its fingerprint. This is
the one step that touches its private half, so do it on a trusted machine, in
a throwaway keyring:

```bash
export GNUPGHOME="$(mktemp -d)"
gpg --import rotation-authority.priv.asc
gpg --quick-add-uid <rotation-authority-fingerprint> \
    "Acme Rotation Authority <release-v2@acme.dev>"
gpg --armor --export <rotation-authority-fingerprint> > rotation-authority.asc
gpg --show-keys rotation-authority.asc
rm -rf "$GNUPGHOME" && unset GNUPGHOME
```

`gpg --show-keys` should list both user IDs under the same fingerprint:

```text
      3023D464AF06360994F6E4A555E8928F70BD4408
uid                      Acme Rotation Authority <release@acme.dev>
uid                      Acme Rotation Authority <release-v2@acme.dev>
```

Then put the private half back in storage. Only the public export leaves the
ceremony.

## Publish the new bucket, and only the new bucket

Build a tree for the new identity alone. Passing only the new email means the
frozen bucket can't be regenerated by accident:

```bash
gtb keys wkd \
    --domain acme.dev \
    --email release-v2@acme.dev \
    --output ./wkd-new \
    signing-key-v1.asc signing-key-v2.asc rotation-authority.asc
```

```text
INFO  WKD bucket email=release-v2@acme.dev hash=yxidni38ndxos3irk9hcm9bjnnemdx8r keys=3
INFO  WKD tree complete output=./wkd-new method=advanced emails=1 files=2
```

Check the bucket holds exactly the three keys your next release will embed:

```bash
gpg --show-keys wkd-new/.well-known/openpgpkey/acme.dev/hu/yxidni38ndxos3irk9hcm9bjnnemdx8r
```

Copy only the new `hu/` file into your staging copy of the live tree, refusing
to overwrite anything already there:

```bash
cp --update=none wkd-new/.well-known/openpgpkey/acme.dev/hu/yxidni38ndxos3irk9hcm9bjnnemdx8r \
   wkd-staging/.well-known/openpgpkey/acme.dev/hu/
```

Check every existing file is still byte-identical to production, then deploy
the way you did before. Afterwards, compare each served file's `sha256sum`
with the staged one. The old bucket matching proves every installed copy is
untouched.

## Ship a release that trusts the new set

In your tool, replace the embedded keys with the new-identity set and point it
at the new bucket:

```bash
cp signing-key-v1.asc signing-key-v2.asc internal/trustkeys/keys/
cp rotation-authority.asc internal/trustkeys/authority/
gtb enable signing --email release-v2@acme.dev
```

The re-minted v1 replaces the old file. It's the same key, so the fingerprint
set is v1, v2 and the rotation authority, which is exactly what the new bucket
serves.

Don't switch signing to v2 yet. Copies already installed only trust v1, and
they need to be able to verify the release that brings them v2.

## Sign with both keys during the overlap

For the overlap window, every release carries two signatures in one
`checksums.txt.sig`: one from v1 for the installed copies, one from v2 for the
new ones. `sigillum sign --append` adds a second signature packet to an existing
signature file, and verifiers skip a packet whose key they don't have.

The generated signs block calls `sigillum sign` once and can't name a second key
yet ([#156](https://gitlab.com/phpboyscout/go-tool-base/-/work_items/156)).
For the window, replace it with a script, `scripts/sign-release.sh`:

```sh
#!/bin/sh
# Usage: sign-release.sh <signature> <artifact>
set -eu
sig=$1 artifact=$2

sigillum --ci sign --backend "$SIGN_BACKEND" --key-id "$SIGN_KEY_ID" \
  --public-key "$SIGN_PUBLIC_KEY" --output "$sig" "$artifact"

if [ -n "${SIGN_KEY_ID_2:-}" ]; then
  AWS_ROLE_ARN="${AWS_ROLE_ARN_2:-${AWS_ROLE_ARN:-}}" \
    sigillum --ci sign --append --backend "$SIGN_BACKEND" --key-id "$SIGN_KEY_ID_2" \
    --public-key "$SIGN_PUBLIC_KEY_2" --output "$sig" "$artifact"
fi
```

Make it executable, point the signs block at it, and tell gtb to leave the
release config alone while it's hand-edited:

```yaml
signs:
  - id: checksums
    cmd: scripts/sign-release.sh
    env:
      - "AWS_ACCESS_KEY_ID="
      - "AWS_SECRET_ACCESS_KEY="
    args: ["${signature}", "${artifact}"]
    artifacts: checksum
    signature: "${artifact}.sig"
    output: true
```

Keep the `env:` lines on GitLab, where the generated block has them too. The
goreleaser component hands the job the release store's keys, and static keys
outrank the OIDC token, so both signatures would otherwise fail. On GitHub leave
them out, because the role step supplies its credentials through those variables.

```bash
chmod +x scripts/sign-release.sh
gtb ignore add .goreleaser.yaml
```

In the release job, set the first key to v1 and the second to v2:

```yaml
  variables:
    SIGN_BACKEND: aws-kms
    SIGN_KEY_ID: alias/acme-release-signing-v1
    SIGN_PUBLIC_KEY: internal/trustkeys/keys/signing-key-v1.asc
    SIGN_KEY_ID_2: alias/acme-release-signing-v2
    SIGN_PUBLIC_KEY_2: internal/trustkeys/keys/signing-key-v2.asc
    AWS_ROLE_ARN: ${SIGNER_ROLE_ARN}
    AWS_ROLE_ARN_2: ${SIGNER_ROLE_ARN_V2}
```

The same OIDC token works for both roles, because both trust your project's
tag pipelines. `sigillum sign` reads the key's region from `--kms-region`, which
defaults to `eu-west-2`; add it to both commands in the script if yours
differs.

## Prove both generations verify

Before you rely on the window, check a released `checksums.txt.sig` against
each generation. The frozen bucket is, byte for byte, the key set every
installed copy holds, so use it as the old keyring:

```bash
gpg --list-packets checksums.txt.sig | grep -c 'signature packet'
```

```text
2
```

```bash
export GNUPGHOME="$(mktemp -d)"
curl -s https://openpgpkey.acme.dev/.well-known/openpgpkey/acme.dev/hu/y84sdmnksfqswe7fxf5mzjg53tbdz8f5 | gpg -q --import
gpg --verify checksums.txt.sig checksums.txt
rm -rf "$GNUPGHOME"

export GNUPGHOME="$(mktemp -d)"
gpg -q --import signing-key-v2.asc
gpg --verify checksums.txt.sig checksums.txt
rm -rf "$GNUPGHOME" && unset GNUPGHOME
```

Each check should give one good signature and one it can't check:

```text
gpg: Good signature from "Acme Releases <release@acme.dev>" [unknown]
gpg: Can't check signature: No public key
```

The `No public key` line is the other generation's packet being skipped. It's
correct, not an error.

## Close the window

Leave the overlap running until you're confident the slow movers have
updated: weeks, not hours, depending on how often your users run the tool.
Then:

1. **Stop signing with v1.** Delete the `_2` variables and set the first key
   to v2. The script's second pass goes quiet.
2. **Hand the release config back to gtb**, if you like:
   `gtb ignore remove .goreleaser.yaml`, then
   `gtb enable signing --key-id alias/acme-release-signing-v2 --public-key internal/trustkeys/keys/signing-key-v2.asc`.
3. **Only now retire the v1 KMS key.** Destroying it any earlier strands every
   copy that hasn't updated.

Keep v1's public half embedded, and keep the frozen bucket published, for as
long as anything v1 signed should verify. For released software that's
indefinitely.

Expect your own tests to complain during the rotation. A test that pins your
embedded fingerprints should fail when v2 arrives; updating it in the same
change is the deliberate acknowledgement it exists to force.

## When the old key is gone

Everything above assumes v1 can still sign through the overlap. If it's gone
(the account closed early, access lost, or you daren't use it again), there's
no overlap to run. Mint a fresh release key and follow the same procedure
from the top: a new identity and a new WKD bucket, the new key set embedded,
and every release signed with the new key alone.

Copies that only trust v1 can't verify those releases, so they'll refuse to
update. Tell your users to reinstall once from the release page. The new
binary trusts the new key, and self-update works again from there. That's the
cost of losing a key early, and it's why the overlap matters: never retire v1
before it has closed.

Don't sign a release with the rotation authority to bridge the gap. It's
there to vouch for a new key set, not to sign releases, and your tool refuses a
release signed by it.

Store the rotation authority's private half as
[Generate the rotation-authority key](../how-to/generate-rotation-key.md)
describes: a `paperkey` printout and an encrypted USB stick, kept apart, and
the backup typed back in once to prove it reads. A break-glass key you can't
restore isn't one.

## The whole chain

The key is born in KMS and never leaves it. Its public half is minted out of
the HSM, published on WKD somewhere your release host can't touch, and
embedded in your binary. Every tagged release is signed through KMS over
short-lived OIDC credentials. Every installed copy checks its own updates
against two independent copies of the key. And when the key has to change,
it changes without stranding anyone.

## What to read next

- [Release-binary signing](../explanation/concepts/release-binary-signing.md):
  the whole design in one place.
- [Generate the rotation-authority key](../how-to/generate-rotation-key.md):
  storage and recovery for the break-glass key.
- [Sign release artefacts](../how-to/sign-releases.md): `--append` and every
  other `sign` flag.
