---
title: Sign and verify on your laptop
description: Make an OpenPGP signing key with gtb, mint the public key you'll publish, sign a checksums file, and verify the signature with plain gpg, all locally with no cloud account.
date: 2026-11-02
tags: [tutorial, signing, openpgp, gpg, release]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Sign and verify on your laptop

By the end of this you'll have signed a release's `checksums.txt` with a key
you made, verified the signature with `gpg`, and watched a tampered copy fail.
It all runs on your laptop with a key on disk. No cloud account, no CI, no cost.

This is the first part of the signing series. A checksum file sits on the same
release page as the binaries it describes, so whoever can swap a binary can
swap the checksum too. A signature is made by a private key the release host
never holds, so it can't be forged that way. This part walks the whole loop by
hand. The later parts move the private key into AWS KMS, and the commands stay
the same apart from one flag.

Allow about fifteen minutes.

## Before you start

- **The `gtb` CLI.** Everything below is verified against **v0.50.0**. See
  [installation](../installation.md).
- **`gpg`**, for the independent check at the end. Most Linux distributions
  ship it. On macOS, `brew install gnupg`.
- **A scratch directory.** You'll create half a dozen files, one of them a
  private key:

```bash
mkdir signing-scratch && cd signing-scratch
```

## Make a signing key

`gtb keys generate` creates a keypair inside the process and writes both
halves to disk:

```bash
gtb keys generate \
    --algorithm rsa \
    --name "Acme Releases" \
    --email "release@acme.dev" \
    --output signing.asc
```

```text
INFO  Generated OpenPGP keypair algorithm=rsa public_output=signing.asc private_output=signing.pem creation_time=2026-11-02T14:14:35Z fingerprint=0DE1DE443AF4B5B39154BC667BEC081C09633311
WARN  Move the private-half file to offline storage now. private_output=signing.pem
```

Your fingerprint and creation time will differ. Copy the `creation_time`
value, because the next step needs it.

Two files come out. `signing.asc` is the public half. `signing.pem` is the
private half, a 4096-bit RSA key in PKCS#1 PEM, and it's the thing you guard.
`gtb` doesn't encrypt it, so keep it on an encrypted disk (LUKS, FileVault) or
wrap it with `age` rather than leaving it lying about.

## Mint the public key you'll publish

You could hand `signing.asc` around as it is. Instead, produce the public key
with `gtb keys mint`, which wraps a *signing backend* in OpenPGP framing and
writes out its public half:

```bash
gtb keys mint \
    --backend local \
    --key-id signing.pem \
    --name "Acme Releases" \
    --email "release@acme.dev" \
    --created "2026-11-02T14:14:35Z" \
    --output release.asc
```

Put your own `creation_time` in `--created`.

```text
INFO  Minted OpenPGP key backend=local key_id=signing.pem output=release.asc creation_time=2026-11-02T14:14:35Z fingerprint=0DE1DE443AF4B5B39154BC667BEC081C09633311
```

The fingerprint matches the one `generate` printed. That's what `--created` is
for. An OpenPGP fingerprint is computed partly from the key's creation time, so
the same private key minted at a different instant gets a different
fingerprint. Leave `--created` off and you'll see exactly that.

Here the backend is `local`, a PEM file on disk. In production it's `aws-kms`,
pointing at a key nobody can download, and minting is the only way to get the
public half of such a key. Doing it the same way now means the rest of the
series changes `--backend` and `--key-id` and nothing else. From here on,
`release.asc` is the public key you publish and embed.

## Sign a checksums file

A real release signs its `checksums.txt`, the list of hashes for every
archive. Make a stand-in and sign it:

```bash
printf 'abc123  acme_linux_amd64\ndef456  acme_darwin_arm64\n' > checksums.txt

gtb sign \
    --backend local \
    --key-id signing.pem \
    --public-key release.asc \
    checksums.txt
```

```text
INFO  Signed file backend=local key_id=signing.pem public_key=release.asc input=checksums.txt output=checksums.txt.sig sig_creation_time=2026-11-02T14:14:51Z fingerprint=0DE1DE443AF4B5B39154BC667BEC081C09633311
```

That writes `checksums.txt.sig`, a detached, ASCII-armoured OpenPGP signature:

```text
-----BEGIN PGP SIGNATURE-----
...
-----END PGP SIGNATURE-----
```

## See it refuse the wrong key

`--public-key` names the identity you claim to be signing as, and `gtb sign`
checks the backend's key against it before signing. To see that check fire,
make a second key and claim to be it:

```bash
gtb keys generate --algorithm rsa --name Other --email other@acme.dev --output other.asc

gtb sign \
    --backend local \
    --key-id signing.pem \
    --public-key other.asc \
    --output wrong.sig \
    checksums.txt
```

```text
ERROR computing signature: signer's RSA public half does not match the public key block — wrong key?
```

It exits 1 and writes nothing, so a release job pointed at the wrong key fails
instead of shipping a signature nobody can verify.

## Verify it with gpg

Your tool will check this signature itself on every self-update, and a later
part of the series wires that in. For now, prove the signature is sound with a
tool that knows nothing about gtb.

Importing a key changes your `gpg` keyring, so use a throwaway one:

```bash
export GNUPGHOME="$(mktemp -d)"

gpg --import release.asc
gpg --verify checksums.txt.sig checksums.txt
```

```text
gpg: key 7BEC081C09633311: public key "Acme Releases <release@acme.dev>" imported
gpg: Total number processed: 1
gpg:               imported: 1
gpg: Signature made Mon 02 Nov 2026 14:14:51 UTC
gpg:                using RSA key 0DE1DE443AF4B5B39154BC667BEC081C09633311
gpg: Good signature from "Acme Releases <release@acme.dev>" [unknown]
gpg: WARNING: This key is not certified with a trusted signature!
gpg:          There is no indication that the signature belongs to the owner.
Primary key fingerprint: 0DE1 DE44 3AF4 B5B3 9154  BC66 7BEC 081C 0963 3311
```

`Good signature` is the line that matters. The warning is about `gpg`'s web of
trust: you haven't told it you trust this key, which says nothing about whether
the signature is valid. What `gtb sign` produces is an ordinary OpenPGP
detached signature, so anyone can check it without gtb.

## Watch a tampered file fail

Change one hash in `checksums.txt`, as an attacker swapping a binary would
have to, and verify again:

```bash
sed -i.orig 's/abc123/abc124/' checksums.txt
gpg --verify checksums.txt.sig checksums.txt
```

```text
gpg: Signature made Mon 02 Nov 2026 14:14:51 UTC
gpg:                using RSA key 0DE1DE443AF4B5B39154BC667BEC081C09633311
gpg: BAD signature from "Acme Releases <release@acme.dev>" [unknown]
```

`gpg` exits 1. That failure is the reason signing exists: the manifest no
longer matches its signature, and a tool that requires a valid signature
refuses the update. `checksums.txt.orig` is the untouched copy if you want to
see `Good signature` again.

## Clean up

The private keys are the only files that matter. Delete them when you've
finished, along with the throwaway keyring:

```bash
rm signing.pem other.pem
rm -rf "$GNUPGHOME" && unset GNUPGHOME
```

## Where this leaves you

You've made a key, minted the public half you'd publish, signed a manifest,
and verified it with a tool that has nothing to do with gtb. That's the
complete trust loop in miniature: a private key signs, a public key verifies.
It stays that shape whether the private half is a `.pem` on your laptop or a
key held in AWS KMS.

The local key is also the weak point. It's a file, and files get copied. The
next part of the series moves the private key somewhere it can't be copied at
all.

## What to read next

- [Release-binary signing](../explanation/concepts/release-binary-signing.md):
  the chain of trust from your release pipeline to the person running
  `mytool update`.
- [Mint an OpenPGP signing key](../how-to/mint-signing-key.md) and
  [Sign release artefacts](../how-to/sign-releases.md): the same two commands,
  with every flag, for when you're doing it for real.
- [Ship self-updates](ship-self-updates.md): where a signature gets checked,
  and what `update.require_signature` does.
