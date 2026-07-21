---
title: Mint an OpenPGP signing key
description: Produce the ASCII-armored OpenPGP public key your tool embeds + serves via WKD, sourced from either AWS KMS (production) or a local PEM file (tutorial).
date: 2026-06-08
tags: [how-to, keys, mint, aws-kms, local, signing]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Mint an OpenPGP signing key

This is the step that turns a private key (held in AWS KMS or on disk
as a PEM file) into the **ASCII-armored OpenPGP public key file** you
ship inside your tool's binary and publish at your WKD endpoint.
You'll run this command once per signing key — at initial setup and
again whenever you rotate.

`gtb keys mint` does the bridging from "a `crypto.Signer` somewhere"
to "a valid OpenPGP entity on disk". It does not generate the
private key — that came from either Terraform (KMS) or
`gtb keys generate` (local PEM).

## Prerequisites

- `gtb` installed.
- The private key already exists, either in AWS KMS (production
  path) or as a PEM file on disk (tutorial path).
- For the AWS KMS path: AWS credentials that can call
  `kms:GetPublicKey` and `kms:Sign` on the target key. Usually that
  means assuming the signer IAM role via OIDC or, for a local
  one-off mint, a temporary trust-policy widening — see the
  [`terraform-aws-signing-kms`](https://gitlab.com/phpboyscout/terraform-aws-signing-kms)
  module's `scripts/mint-signing-key/README.md` for the canonical
  recipe.

## The AWS KMS path (production)

```sh
gtb keys mint \
    --backend aws-kms \
    --key-id alias/mytool-release-signing-v1 \
    --kms-region eu-west-2 \
    --name "MyTool Release" \
    --email release@mytool.example \
    --output release.asc
```

What this does:

1. Calls `kms:GetPublicKey` to learn the RSA public half. The
   resulting `*rsa.PublicKey` is what the OpenPGP packet will
   describe.
2. Constructs an OpenPGP entity around the public half: a v4 RSA
   public-key packet, the User ID you supplied, and a positive-cert
   self-signature. **The self-signature is produced by calling
   `kms:Sign` exactly once.** That single round-trip is the only
   time the KMS private half is consulted during a mint.
3. ASCII-armors the resulting bytes and writes them to `release.asc`.
4. Logs the fingerprint at INFO so you can record it next to the
   KMS key alias in your runbook.

A successful mint looks like:

```
INFO  Minted OpenPGP key  backend=aws-kms  key_id=alias/mytool-release-signing-v1
                          output=release.asc
                          creation_time=2026-06-08T12:00:00Z
                          fingerprint=A1B2C3D4...
```

### Flag reference

| Flag | Required | Default | Purpose |
|---|---|---|---|
| `--backend` | yes | — | `aws-kms` or `local` (or any third-party backend blank-imported into your `gtb` binary). |
| `--key-id` | yes | — | KMS key ID, ARN, or alias. Aliases survive rotation; prefer them. |
| `--kms-region` | no | `eu-west-2` | AWS region the key lives in. |
| `--name` | yes | — | OpenPGP UID real name. |
| `--email` | yes | — | OpenPGP UID email. |
| `--output` | no | `release.asc` | Output path for the armored public key. |
| `--force` | no | `false` | Overwrite the output file if it already exists. Without it, `mint` refuses to clobber an existing file and exits with an error. |
| `--created` | no | now | RFC3339 creation time. Pin only when re-minting an existing key — different creation times produce different fingerprints. |

!!! warning "No silent overwrites"
    `gtb keys mint` refuses to overwrite an existing `--output` file. If you
    are intentionally re-minting to the same path, pass `--force`.

### Credentials

The AWS SDK default credential chain is used. Set `AWS_PROFILE`,
export `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` /
`AWS_SESSION_TOKEN`, or use any other SDK-recognised credential
source. For a one-off mint against a tag-pipeline-only signer role,
the canonical recipe is `aws sts assume-role` with MFA, exporting
the resulting temporary credentials, then running `gtb keys mint`.

## The local PEM path (tutorial / no-cloud-KMS)

```sh
gtb keys mint \
    --backend local \
    --key-id signing.pem \
    --name "MyTool Release" \
    --email release@mytool.example \
    --output release.asc
```

What this does:

1. Reads `signing.pem` from disk. Supports unencrypted PKCS#1
   (`-----BEGIN RSA PRIVATE KEY-----`) and unencrypted PKCS#8
   (`-----BEGIN PRIVATE KEY-----`). Encrypted PEMs are not
   supported — decrypt out of band first, or use the `aws-kms`
   backend, which never exposes key material.
2. Builds the OpenPGP entity using the in-memory `*rsa.PrivateKey`
   as the signer.
3. Writes `release.asc` as in the AWS KMS path.

The PEM file pairs naturally with `gtb keys generate --algorithm rsa`,
which produces exactly this format on the private-output path. The
two commands together give you a complete signing chain without any
cloud dependency — perfect for blog posts and integration tests.

## Reproducibility: `--created`

`gtb keys mint` uses `time.Now()` for the OpenPGP creation timestamp
by default. The timestamp is folded into the key's fingerprint, so
two mints of the same KMS key one second apart produce two different
fingerprints.

If you ever need to **re-derive** an existing key (because you lost
`release.asc` but the KMS material is intact), pin `--created` to
the original creation time:

```sh
gtb keys mint ... --created 2026-06-08T00:00:00Z
```

The same KMS material + same UID + same creation time = same
fingerprint. This is also how `gtb keys generate --algorithm rsa`
and `gtb keys mint --backend local` produce identical fingerprints
when chained — see the
[concept doc](../explanation/concepts/release-binary-signing.md) for the
end-to-end flow.

## Smoke-test the result with `gpg`

After the mint, verify the file parses cleanly:

```sh
gpg --show-key --with-fingerprint release.asc
# pub   rsa4096 2026-06-08 [SC]
#       A1B2 C3D4 ... 40-char fingerprint
# uid                      MyTool Release <release@mytool.example>
```

If `gpg` reports `unknown_<n> [INVALID_ALGO]`, you've hit a
version-incompatibility — file a bug; the framework deliberately
produces v4 RSA packets that every modern OpenPGP implementation
accepts.

## What to do with `release.asc`

Two destinations:

1. **Embed in your tool**. Copy the file into your tool's
   `internal/trustkeys/keys/` directory. Go's `//go:embed` directive
   in `internal/trustkeys/trustkeys.go` picks it up automatically;
   it ends up in your binary's trust set at compile time.
2. **Publish via WKD**. Upload the file to your WKD endpoint at
   `https://openpgpkey.<your-domain>/.well-known/openpgpkey/<your-domain>/hu/<z-base32-hash>?l=release`.
   The recipe for the Cloudflare Pages Direct Upload pattern is in
   the [phase2-signing-prep doc](../development/phase2-signing-prep.md).

Both copies must contain the **same fingerprint** — the verifier's
CompositeResolver cross-checks them and refuses to proceed if they
disagree.

## Rotation

Mint v2 of the signing key by re-running `gtb keys mint` against the
new KMS key:

```sh
gtb keys mint --backend aws-kms \
    --key-id alias/mytool-release-signing-v2 \
    ... \
    --output release-v2.asc
```

Both `release.asc` (v1) and `release-v2.asc` go into
`internal/trustkeys/keys/` together for the rotation overlap window
— old releases verify against v1, new releases against v2. Drop the
v1 file once your supported-version window has cleared.

## See also

- [Generate a rotation-authority key](generate-rotation-key.md) — the
  offline-storage flow for the break-glass key.
- [Adding a signing backend](add-signing-backend.md) — recipes for
  GCP KMS, Vault, YubiKey.
- [Release-binary signing concept](../explanation/concepts/release-binary-signing.md)
  — the end-to-end trust model.
- [`openpgpkey`](../explanation/components/openpgpkey.md) — the underlying
  packet-assembly API if you need it programmatically (now the standalone
  `gitlab.com/phpboyscout/go/signing/openpgpkey` module).
