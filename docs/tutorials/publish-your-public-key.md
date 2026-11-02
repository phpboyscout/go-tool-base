---
title: Publish your public key
description: Mint the OpenPGP public key straight out of AWS KMS, create an offline rotation-authority key, and publish both as a Web Key Directory on a host your release platform can't touch.
date: 2026-11-02
tags: [tutorial, signing, aws, kms, openpgp, wkd]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Publish your public key

By the end of this you'll have the public half of your KMS signing key as an
OpenPGP key file, an offline rotation-authority key beside it, and both
published as a Web Key Directory (WKD) on a host your release platform has no
access to.

This is the fourth part of the signing series.
[Sign from CI without stored keys](sign-from-ci-without-stored-keys.md) left
your pipeline able to sign through KMS. But a signature is no use to someone
who can't get the matching public key, and KMS deals in raw signing
operations, not OpenPGP keys. This part builds the public key from KMS without
touching the private half, then puts it where an attacker who owns your
release host can't rewrite it.

Allow about thirty minutes, plus whatever your static host and DNS take.

## Before you start

- **The KMS key** from [Put the signing key in AWS KMS](put-the-signing-key-in-kms.md),
  and its alias.
- **AWS credentials allowed `kms:GetPublicKey` and `kms:Sign` on that key.**
  Minting reads the public half and then signs with it, as the next step
  explains. The key policy gives `kms:Sign` to the signer role alone, so run
  the mint as the signer role or from a pipeline that assumes it.
- **The `gtb` CLI.** The commands below are checked against **v0.50.0**.
- **A domain you control**, and a static host for it that is separate from
  your code and release hosting. More on why below.

## Mint the public key from KMS

`gtb keys mint` builds an OpenPGP public key from a signing backend.
In [Sign and verify on your laptop](sign-and-verify-on-your-laptop.md) the
backend was a `.pem` file. Now it's `aws-kms`:

```bash
gtb keys mint \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/acme-release-signing-v1 \
    --name "Acme Releases" \
    --email release@acme.dev \
    --created "2026-06-02T00:00:00Z" \
    --output signing-key-v1.asc
```

```text
INFO  Minted OpenPGP key backend=aws-kms key_id=alias/acme-release-signing-v1 output=signing-key-v1.asc creation_time=2026-06-02T00:00:00Z fingerprint=...
```

An OpenPGP key isn't only public-key material. It carries a self-signature,
which binds the name and email to the key, and making that normally needs the
private key. `mint` hands the signing to the backend instead, so the
self-signature is one more `kms:Sign` call. KMS does the maths inside its HSM
and returns a signature. No private byte leaves it, not even to sign the key's
own identity.

Pick `--created` once and never change it. The fingerprint is computed partly
from the creation time, so minting the same KMS key with a different
timestamp gives what every tool treats as a different key. The key you embed
in your binary in the next part has to match this one byte for byte. The KMS
key's own creation date is a sensible choice:

```bash
aws kms describe-key --key-id alias/acme-release-signing-v1 \
    --query KeyMetadata.CreationDate --output text
```

Write the value down beside the key's alias. You'll need it again whenever
you re-mint.

## Make a rotation-authority key

You won't use this until the rotation part of the series, but it's far easier
to make now than in a hurry later. The rotation authority is a break-glass
key: an offline key whose only job is to vouch for a new signing key if the
KMS one ever has to be replaced.

Because it's for emergencies, it doesn't live in KMS. Generate it on a trusted
machine, ideally one that's offline:

```bash
gtb keys generate \
    --algorithm ed25519 \
    --name "Acme Rotation Authority" \
    --email release@acme.dev \
    --output rotation-authority.asc
```

```text
INFO  Generated OpenPGP keypair algorithm=ed25519 public_output=rotation-authority.asc private_output=rotation-authority.priv.asc creation_time=2026-11-02T14:50:17Z fingerprint=3023D464AF06360994F6E4A555E8928F70BD4408
WARN  Move the private-half file to offline storage now. private_output=rotation-authority.priv.asc
```

You get an armoured public key, `rotation-authority.asc`, and an armoured
secret-key block, `rotation-authority.priv.asc`, in the same format
`gpg --export-secret-keys` writes. Move the private half to offline storage
now. An encrypted USB stick and a paper backup isn't overdoing it for a key
you might not touch for two years and will badly need when you do.

It carries the same `release@acme.dev` address as the signing key, not a
separate `rotation@` one, and that's deliberate. WKD groups keys by the email
in their user ID. The shared address puts the rotation authority in the same
published bucket as the signing key, and so in the same trust set your binary
embeds. Give it a different email and it drops out of that bucket, the
published and embedded key sets stop matching, and every update fails the
check you'll set up in the next part.

## Build the WKD tree

A Web Key Directory is how a client finds a public key from an email address:
a fixed layout of files under `.well-known/openpgpkey/` on a web server, with
each address's keys at a path derived from a hash of its local part.
`gtb keys wkd` builds the tree in Go, with no `gpg` needed:

```bash
gtb keys wkd \
    --domain acme.dev \
    --email release@acme.dev \
    --output ./wkd-staging \
    signing-key-v1.asc rotation-authority.asc
```

```text
INFO  WKD bucket email=release@acme.dev hash=y84sdmnksfqswe7fxf5mzjg53tbdz8f5 keys=2
INFO  wrote path=wkd-staging/.well-known/openpgpkey/acme.dev/policy
INFO  wrote path=wkd-staging/.well-known/openpgpkey/acme.dev/hu/y84sdmnksfqswe7fxf5mzjg53tbdz8f5
INFO  WKD tree complete output=./wkd-staging method=advanced emails=1 files=2
```

The hash depends only on the address, so yours for `release@acme.dev` is the
same. The tree holds two files:

- **`policy`**, which the WKD spec requires. It's empty.
- **`hu/<hash>`**, with both keys inside, because both carry
  `release@acme.dev`.

Pass `--submission-address auto` if you also want a `submission-address`
file. Nothing in this series needs one.

The default `--method` is `advanced`, which serves the tree from a dedicated
`openpgpkey.acme.dev` subdomain. That's why `acme.dev` appears inside the
path. With `--method direct` it's served from `acme.dev` itself. Use
`advanced` unless you have a reason not to, and know that it needs DNS and
TLS for `openpgpkey.acme.dev`.

## Publish it where your release host can't reach

Don't put the WKD tree on the same host, or under the same account, as your
code and releases. This is the step that's tempting to cut short, and it's
the one the whole scheme depends on.

Your binary will carry an embedded copy of the public key. On every
self-update it also fetches the published key over WKD and checks that the
two agree before it trusts a download. If an attacker who controls your
release platform could rewrite the WKD tree too, they'd replace both keys
with their own, sign a malicious release, and the check would compare one
forged key against another copy of it. The check only protects anyone if
the published key and the embedded key come from infrastructure an attacker
has to breach separately.

So publish to a different host with its own credentials. A static host such
as Cloudflare Pages in Direct Upload mode works: build the tree locally and
push it with the Wrangler CLI, under a token that can edit that Pages project
and nothing else, with no Git integration to your code repository. For the
advanced method, point DNS for `openpgpkey.acme.dev` at that host and let it
terminate TLS.

Whatever you choose, ask one question: could a single stolen credential
change both the key in the binary's update path and the key on the server?
If it could, start again.

## Where this leaves you

Your KMS key now has a published, fetchable OpenPGP public key, made without
its private half ever leaving KMS. The offline rotation authority sits beside
it, and both live somewhere your release platform can't quietly rewrite.

What's missing is a tool that checks. The next part embeds the public key in
your binary and turns on enforcement, so it refuses any update whose
signature doesn't hold.

## What to read next

- [Mint an OpenPGP signing key](../how-to/mint-signing-key.md): every `mint`
  flag, both backends.
- [Release-binary signing](../explanation/concepts/release-binary-signing.md):
  why the published and embedded keys must come from separate places.
