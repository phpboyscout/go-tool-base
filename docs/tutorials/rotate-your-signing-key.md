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
  signing until the end. The v1 signer role signs with both keys during the
  overlap, so the v2 key has to let it. The next section shows how.
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
  step. This is the OpenPGP `rotation-authority.asc` from
  [Publish your public key](publish-your-public-key.md), in
  `internal/trustkeys/authority/`. A minisign manifest authority in
  `internal/trustkeys/rotation/`, if your tool has one, isn't part of this
  procedure and stays in the safe.
- **A gtb whose `enable signing` has `--overlap-key-id`**, for the
  `gtb enable signing` steps. Run `gtb enable signing --help` to check. Earlier
  releases need the hand-written script this page used to describe, and
  releases before v0.50.4 don't give a generated tool its `authority/` directory.
- **sigillum.** The `keys` and `sign` commands below use the flags of sigillum **v0.6.0**,
  with local keys standing in for KMS. Install it with
  `go install gitlab.com/phpboyscout/sigillum/cmd/sigillum@latest`. They used to be
  `gtb keys` and `gtb sign`; the flags are the same. The `gtb enable signing` steps
  still need gtb.

## Re-mint v1 under the new identity

A key only appears in a WKD bucket if one of its user IDs names that bucket's
email. So v1 has to gain the `release-v2@acme.dev` identity, without becoming a
different key. Mint it again with the new email and the **original** creation
time from your inventory:

```bash
sigillum keys mint \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/acme-release-signing-v1 \
    --name "Acme Releases" \
    --email release-v2@acme.dev \
    --created "2026-06-02T00:00:00Z" \
    --output signing-key-v1.asc --force
```

`--force` is there because `signing-key-v1.asc` already exists from the first
mint, and `mint` refuses to overwrite a file without it. Only the public half
is written, so nothing is lost: the private key stays in KMS.

The fingerprint it logs must exactly match v1's fingerprint from the
inventory. If it doesn't, the `--created` value is wrong. Fix it before going
any further. Nothing has been published yet, so a mistake here costs nothing.

## Mint v2

The new key gets the new identity and no `--created` pin, because it's new:

```bash
sigillum keys mint \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/acme-release-signing-v2 \
    --name "Acme Releases" \
    --email release-v2@acme.dev \
    --output signing-key-v2.asc
```

Record the creation time it logs beside the alias. It's now part of v2's
fingerprint for good.

## Let the v1 signer role sign with v2

Your release job assumes one role, the one in the `SIGNER_ROLE_ARN` CI
variable. Through the overlap it signs with both keys, so that role needs
`kms:Sign` on v2 as well as v1.

The module's key policy refuses `kms:Sign` from anyone but the key's own
signer role and the principals in `mint_principal_arns`, and only the
automation role may change it. So the grant goes in the v2 module, from
version 0.3.0:

```hcl
module "signing_kms_v2" {
  source  = "gitlab.com/phpboyscout/signing-kms/aws"
  version = "0.3.0"

  name = "acme-release-signing-v2"
  # ... the same inputs as v1 ...

  # The overlap's one signer role. Remove it when the window closes.
  mint_principal_arns = [module.signing_kms.signer_role_arn]
}
```

A mint principal may sign only a SHA-256 digest with RSASSA PKCS#1 v1.5, which
is exactly what a release signature is. The input is documented for a mint
ceremony role, and a dedicated input for a rotation's second signer is
[requested](https://gitlab.com/phpboyscout/iac/terraform-aws-signing-kms/-/work_items/5).
Until it exists, this is the grant to use.

A v2 key in another AWS account needs that account's key policy to trust the
v1 role, and the v1 role's own policy to allow `kms:Sign` on the v2 key ARN.

## Give the rotation authority the new identity

The rotation authority sits in the WKD bucket with the release keys, so it
needs the new UID too, keeping its fingerprint. A minisign manifest authority
in `internal/trustkeys/rotation/` has no user ID and isn't on WKD, so nothing
about it changes here. This is
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
sigillum keys wkd \
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
gtb enable signing --email release-v2@acme.dev
cp signing-key-v1.asc signing-key-v2.asc internal/trustkeys/keys/
cp rotation-authority.asc internal/trustkeys/authority/
rm -f internal/trustkeys/keys/rotation-authority.asc
```

`gtb enable signing` runs first because, on a tool scaffolded before v0.50.4,
it's what creates `authority/`. The `rm` clears the copy an older version of
[Require signed updates](require-signed-updates.md) put in `keys/`. A key in
both directories counts twice in the embedded set, which then can't match the
new bucket, and every copy of that release would refuse to update.

The re-minted v1 replaces the old file. It's the same key, so the fingerprint
set is v1, v2 and the rotation authority, which is exactly what the new bucket
serves.

Don't switch signing to v2 yet. Copies already installed only trust v1, and
they need to be able to verify the release that brings them v2.

## Sign with both keys during the overlap

For the overlap window, every release carries two signatures in one
`checksums.txt.sig`: one from v1 for the installed copies, one from v2 for the
new ones. Verifiers skip a packet whose key they don't have.

Open the window by naming v2 as the overlap key:

```bash
gtb enable signing \
    --overlap-key-id alias/acme-release-signing-v2 \
    --overlap-public-key internal/trustkeys/keys/signing-key-v2.asc
```

The manifest records v2 beside v1:

```yaml
signing:
  enabled: true
  backend: aws-kms
  key_id: alias/acme-release-signing-v1
  public_key: internal/trustkeys/keys/signing-key-v1.asc
  overlap:
    key_id: alias/acme-release-signing-v2
    public_key: internal/trustkeys/keys/signing-key-v2.asc
```

The generated `signs:` entry now runs `sigillum sign` twice, through `sh`. The
second run carries `--append`, which adds v2's packet to the file v1 just
wrote. A failure in the first run fails the entry, so a release can't ship with
v2's packet alone. Add `--overlap-kms-region` when v2 lives in a different
region from v1.

`gtb enable signing` refuses an overlap key outside `internal/trustkeys/keys/`,
or one that isn't there yet. It can't see your releases, so the order is
yours to keep: the release that embeds v2 ships before the window opens.

### Coming from the hand-written script

Earlier versions of this page replaced the signs block with
`scripts/sign-release.sh` and put `.goreleaser.yaml` under `.gtb/ignore`. gtb
never rewrites a signs block it didn't write, so hand it back first:

```bash
gtb ignore remove .goreleaser.yaml
rm scripts/sign-release.sh
```

Delete the `signs:` block from `.goreleaser.yaml` and the `SIGN_*` and
`AWS_ROLE_ARN_2` variables from the release job. Then run the
`gtb enable signing --overlap-key-id ...` command above, which writes a fresh
block. The job's `AWS_ROLE_ARN` stays, because the one role now signs with both
keys.

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

1. **Stop signing with v1.** Promote the overlap key:

   ```bash
   gtb enable signing --promote-overlap
   ```

   v2 becomes the only signing key, and the signs block calls `sigillum sign`
   once again. If you abandon the rotation instead, `--end-overlap` keeps v1
   and drops v2.
2. **Move signing to v2's own role.** Point the `SIGNER_ROLE_ARN` CI variable
   at `acme-release-signing-v2-signer`, the role the v2 module made. Then take
   the v1 signer role out of the v2 module's `mint_principal_arns` and apply.
   In that order, a release never runs without a role that can sign.
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
update. If those copies embed the manifest authority from
[Publish your public key](publish-your-public-key.md#and-a-manifest-authority),
and your releases are published on a channel that carries it, a rotation
manifest moves them to the new key without a reinstall. The static channel
carries it, and so do GitHub, Gitea and Codeberg releases. GitLab releases
don't yet, and copies that update from them need the reinstall at the end of
this section. See
[which channels carry a manifest](../how-to/generate-rotation-key.md#which-channels-carry-the-manifest).

A key that is lost but not compromised needs nothing revoked, so the manifest only adds
the new key: `sigillum keys rotate --reason scheduled` with `--not-after` set
and `--add` naming it. If you've already published a manifest, such as the
fire drill, `--previous` names the latest one. The first manifest needs none.
Pass the keys your shipped copies embed as `--anchor` (and the OpenPGP
rotation authority as `--anchor-only`), and `keys rotate` checks the result as
those copies' updater would before you publish it.
Commit the two files it writes to `release/rotation/`, as
[Generate the rotation-authority key](../how-to/generate-rotation-key.md#what-happens-when-you-need-to-use-it)
describes. A compromised key is the runbook's compromise case instead.
Otherwise, tell your users to reinstall once from the release page.
The new binary trusts the new key, and self-update works again from there. That's the
cost of losing a key early, and it's why the overlap matters: never retire v1
before it has closed.

Don't sign a release with the rotation authority to bridge the gap. It's
there to vouch for a new key set, not to sign releases, and your tool refuses a
release signed by it.

Keep the rotation authority's private half offline: an encrypted USB stick
and a printed copy, kept apart, with the printed copy typed back in once to
prove it reads.
[Generate the rotation-authority key](../how-to/generate-rotation-key.md)
walks through it for the minisign manifest authority (the how-to calls that
key the rotation-authority key), and the same storage suits the OpenPGP one. A break-glass key you can't
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
