---
title: Sign release artefacts with `gtb sign`
description: Produce an ASCII-armored OpenPGP detached signature over a release file using a KMS-held key (or any other backend registered with the signing module, `gitlab.com/phpboyscout/go/signing`), then verify with gpg or the in-tool verifier.
date: 2026-06-09
tags: [how-to, signing, kms, openpgp, release]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Sign release artefacts with `gtb sign`

`gtb sign` produces an armored OpenPGP detached signature over a
single file using the same backend abstraction `gtb keys mint`
uses. In CI, that's typically AWS KMS via OIDC — no operator ever
holds the signing-key secret, no `gpg` install needed on the
runner.

This how-to walks the operator side (local signing for testing,
plus the CI integration with OIDC) and the verification path
(`gpg --verify` and the in-tool verifier).

## Prerequisites

- `gtb` ≥ the version that ships [Spec 2026-06-09-sign-command](../development/specs/2026-06-09-sign-command.md).
- The public-key file (`release.asc`) corresponding to the signing key, **already published** — embedded in `internal/trustkeys/keys/` and served via WKD per [How-to: publish via WKD](publish-wkd.md). `gtb sign` reads identity (creation time, UID) from this file.
- For the `aws-kms` backend: AWS credentials in the standard SDK chain. In local terminal that means `aws login` + `aws configure export-credentials --format env`; in CI it means an OIDC assume-role step (see below).

## One file, one sig — local invocation

```sh
gtb sign \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/gtb-release-signing-v1 \
    --public-key ./release.asc \
    --output checksums.txt.sig \
    checksums.txt
```

The logged INFO line includes the public-key fingerprint so you can
spot-check it matches the trust anchor:

```
INFO Signed file backend=aws-kms key_id=alias/gtb-release-signing-v1
     public_key=./release.asc input=checksums.txt output=checksums.txt.sig
     sig_creation_time=2026-06-09T15:11:32Z
     fingerprint=6E2072BBF83DFAAF006300C495DDAC333C37AA35
```

`--output` defaults to `<input>.sig` if omitted. The command refuses
to write a `.sig` over its own input. The guard compares
`filepath.Clean`'d paths and falls back to `os.SameFile`, so a
different spelling of the same file (`./x` vs `x`, an absolute vs
relative form, or a symlink) is still refused rather than silently
clobbering the input.

### Reproducible signatures

Add `--created <rfc3339>` to pin the signature's creation timestamp.
Two re-runs of `gtb sign` over the same content with the same key
and the same `--created` produce **byte-identical** `.sig` files —
useful when you want to reproduce-from-scratch a previous release's
artefacts in a SLSA-style chain.

```sh
gtb sign \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/gtb-release-signing-v1 \
    --public-key ./release.asc \
    --created 2026-06-09T15:11:32Z \
    checksums.txt
```

## Local signing with the `local` backend

For tutorials, development, or air-gapped builds:

```sh
gtb keys generate --algorithm rsa --rsa-bits 4096 \
    --name "Test Signer" --email test@example.org \
    --output release.asc --private-output release.pem

gtb sign \
    --backend local \
    --key-id ./release.pem \
    --public-key ./release.asc \
    checksums.txt
```

`local` accepts PKCS#1 and PKCS#8 PEM private keys (unencrypted —
the local backend does not decrypt encrypted PEMs; protect the key
file with filesystem-level encryption like LUKS or `age`, or use
the `aws-kms` backend so the key never leaves the HSM).

## Verify

`gtb sign` produces what `gpg --verify` consumes — and what the
in-tool verifier (`TrustSet.VerifyManifestSignature`, now in the
`gitlab.com/phpboyscout/go/signing/verify` module that gtb consumes)
checks during a self-update.

```sh
# Import the published public key into a clean keyring + verify.
TMPGNUPG=$(mktemp -d)
gpg --homedir "$TMPGNUPG" --import release.asc
gpg --homedir "$TMPGNUPG" --verify checksums.txt.sig checksums.txt
# Expect: "Good signature from <UID>" with the published fingerprint.
rm -rf "$TMPGNUPG"
```

## CI integration: GitLab + AWS OIDC

The setup is split into two layers:

1. **Infra (one-time, Terraform)**. The `terraform-aws-signing-kms`
   module already provisions:
   - The OIDC identity provider for `https://gitlab.com` in your AWS
     account.
   - An IAM signer role with `kms:Sign` + `kms:GetPublicKey` on the
     release key only.
   - A trust policy pinning the role to
     `project_path:phpboyscout/go-tool-base:ref_type:tag:ref:v*` —
     only **tag pipelines** on the specific project may assume it.
     MR/branch pipelines and non-`v*` tags fail the OIDC subject filter.

   The module's `signing_kms_signer_role_arn` output is the role ARN
   you pass to CI as `AWS_ROLE_ARN`.

2. **Pipeline (in this repo's `.gitlab-ci.yml`)**. In the goreleaser
   job, declare `id_tokens` and do the assume-role in `before_script`:

   ```yaml
   goreleaser:
     id_tokens:
       AWS_WEB_IDENTITY_TOKEN:
         aud: https://gitlab.com
     variables:
       AWS_ROLE_ARN: arn:aws:iam::<account>:role/gtb-release-signing-v1-signer
     before_script:
       - |
         creds=$(aws sts assume-role-with-web-identity \
           --role-arn "$AWS_ROLE_ARN" \
           --role-session-name "gtb-release-${CI_COMMIT_TAG}" \
           --web-identity-token "$AWS_WEB_IDENTITY_TOKEN" \
           --query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
           --output text)
         export AWS_ACCESS_KEY_ID=$(echo "$creds" | awk '{print $1}')
         export AWS_SECRET_ACCESS_KEY=$(echo "$creds" | awk '{print $2}')
         export AWS_SESSION_TOKEN=$(echo "$creds" | awk '{print $3}')
   ```

   Goreleaser's `signs` block then invokes
   `scripts/sign-release.sh checksums.txt checksums.txt.sig`
   which is a thin wrapper around `gtb sign --backend aws-kms`. The
   AWS env vars set by the `before_script` are picked up by the
   AWS SDK's default credential chain — `gtb sign` itself knows
   nothing about OIDC, which keeps it portable across CI platforms.

## Cross-tool compatibility

`gtb sign` output verifies under:

- **`gpg --verify`** (any modern OpenPGP implementation accepting v4
  RSA-PKCS1v15 signatures)
- **the in-tool verifier** (`gitlab.com/phpboyscout/go/signing/verify`, consumed by gtb) during `gtb update`
- **`openpgp.CheckArmoredDetachedSignature` from
  `ProtonMail/go-crypto`** (the library used by both)

The unit test suite in the `signing/openpgpkey` module exercises the
`go-crypto` path; the integration test gated by `INT_TEST_SIGN=1`
additionally invokes `gpg --verify` to catch any drift between our
framing and the reference implementation.

## What if my signer doesn't match the public key

`gtb sign` reads the public half from `--public-key` and compares
the RSA modulus + exponent to what the backend returns. If they
disagree, the command errors out with:

```
signer's RSA public half does not match the public key block — wrong key?
```

Before producing a signature. This is the safety check that
catches "I copied the wrong KMS alias into the script" before it
becomes an unverifiable `.sig` file in production.

If you intend to rotate the signing key, do it in this order:

1. Mint a new key via `gtb keys mint --backend aws-kms ...`,
   producing `release-v2.asc`.
2. Sign the rotation manifest with the **old** key (using the old
   `release.asc` as `--public-key`).
3. Publish `release-v2.asc` via WKD alongside the old key for a
   transition period.
4. Embed both keys in `internal/trustkeys/keys/`.
5. Cut the next release; it now signs with v2.

See the Phase 2 prep doc for the full rotation runbook.

## Related

- [Spec: gtb sign](../development/specs/2026-06-09-sign-command.md)
  — design decisions, RFC details, threat model.
- [`openpgpkey`](../explanation/components/openpgpkey.md)
  — the `DetachSign` library function the CLI wraps (now the standalone
  `gitlab.com/phpboyscout/go/signing/openpgpkey` module).
- [How-to: publish via WKD](publish-wkd.md) — the matching
  trust-anchor publication step.
- [How-to: add a signing backend](add-signing-backend.md) — for
  consumers who want GCP KMS, Vault, YubiKey, etc.
