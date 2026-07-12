---
title: Release-binary signing
description: How gtb-derived tools establish a cryptographic chain of trust between you, the release pipeline, and the people running your CLI — without anyone holding a private key on their laptop.
date: 2026-06-08
tags: [concepts, signing, openpgp, kms, rotation, trust]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Release-binary signing

When someone runs `mytool update`, they're trusting that the binary
they're about to install came from you and hasn't been tampered with.
That trust has to be **cryptographic** — not "we hope the registry
wasn't compromised", but "the binary verifies against a public key we
already shipped you".

`gtb` provides the building blocks for that chain without anyone
holding a private key on their laptop and without shelling out to
`gpg`. Three commands cover the whole flow:

```sh
gtb keys generate ...     # create new keypairs (rotation-authority, dev signing keys)
gtb keys mint   ...       # turn an HSM-held key into an OpenPGP-armored public file
```

Plus a Terraform module (`terraform-aws-signing-kms`) that provisions
the production signing key in AWS KMS so the private half lives
nowhere but the HSM.

This concept doc walks the trust chain end-to-end. You'll see how the
two key types fit together, what each component protects against, and
where the boundary lives between "framework" and "operational
setup".

## The two keys

A complete signing setup uses **two** keys, not one. They have
different lifetimes, different storage, and different threat models.

### 1. The signing key

The key that actually signs every release. Used on every `vX.Y.Z`
tag — once per release.

- **Algorithm**: RSA-4096 (AWS KMS doesn't expose Ed25519 for
  asymmetric signing, so RSA is the production choice).
- **Where it lives**: AWS KMS in production; an on-disk PEM file
  for tutorials / no-KMS setups.
- **Who can use it**: a tightly-scoped IAM role assumable only by
  tag-pipeline OIDC tokens (production) or anyone with read access
  to the PEM file (tutorial).
- **Created by**: `gtb keys generate --algorithm rsa ...` (tutorial)
  or Terraform via `terraform-aws-signing-kms` (production). The
  `gtb keys mint --backend X` command turns either source into the
  armored OpenPGP public-key file you ship.
- **Rotation cadence**: as often as your threat model requires.
  Usually every 1–3 years, plus emergency rotation on compromise.

### 2. The rotation-authority key

The "break glass" key. Never used in normal operation. Used only when
the signing key is lost or compromised, to authorise the next
signing key without breaking the chain of trust for existing
installs.

- **Algorithm**: Ed25519 (smaller paperkey output, simpler key
  management).
- **Where it lives**: offline. Two backups: an encrypted USB stick
  and a printed `paperkey` recovery sheet, both in a home safe.
- **Who can use it**: you, physically, on a trusted offline
  workstation.
- **Created by**: `gtb keys generate --algorithm ed25519 ...` — the
  command produces both halves locally and asks you to move the
  private half to offline storage immediately.
- **Rotation cadence**: typically never. If you ever have to use it,
  you've already had a bad day.

Both public halves are embedded in your tool's binary
(`internal/trustkeys/keys/`) and published via WKD at a domain you
control. The verifier on the end-user side requires both keys in its
trust set to accept a signature — but in normal operation it only
ever sees signatures from the signing key.

## How verification works at install time

When `mytool update` runs, it does this (sketch):

1. **Download the release artefacts**: the binary plus
   `checksums.txt` plus `checksums.txt.sig`.
2. **Load the embedded trust set**: parse the OpenPGP keys baked
   into the running binary at `internal/trustkeys/keys/`. Both the
   signing key and the rotation-authority key are present.
3. **Fetch the WKD-served copy** of the signing key from your
   domain. Verify it matches the embedded copy by fingerprint —
   this is the **cross-check** that catches a compromised release
   registry (the embedded copy can't be tampered with after the
   binary shipped, and the WKD copy lives on a server administered
   independently from the release registry).
4. **Verify `checksums.txt.sig`** against the cross-checked signing
   key.
5. **Verify the binary** against the hash in `checksums.txt`.

If any step fails, the update is refused with an actionable error.
Phase 2 of the
[`remote-update-checksum-verification`](../../development/specs/2026-04-02-remote-update-checksum-verification.md)
spec implements this flow.

## The trust boundary

Every component above is one of:

- **Yours to set up once**: AWS account, KMS key, IAM role, WKD
  domain, offline rotation-authority key.
- **Done by `gtb` automatically**: generate keypairs, mint armored
  public files, embed via `go:embed`, verify on update.

The line between them is exactly where the framework stops and the
operator starts. `gtb` doesn't try to provision your AWS account or
host your WKD endpoint — those are decisions tied to your
infrastructure. But it does cover every cryptographic operation
between "I have a KMS key" and "the verifier passed", with no
external dependencies on `gpg`, `openssl`, or any other tool.

## The three-command tutorial flow

For someone trying this without a cloud KMS — say, writing a blog
post about it or proving the chain works end-to-end before reaching
for Terraform:

```sh
# 1. Rotation-authority key. Move the private half to offline
#    storage (encrypted USB + paperkey) and forget about it.
gtb keys generate \
    --algorithm ed25519 \
    --name "MyTool Rotation Authority" \
    --email rotation@mytool.example \
    --output rotation.asc

# 2. Local signing key. The PEM private half stays on this
#    machine — for the tutorial only; production uses AWS KMS.
gtb keys generate \
    --algorithm rsa --rsa-bits 4096 \
    --name "MyTool Release" \
    --email release@mytool.example \
    --output signing.asc \
    --private-output signing.pem

# 3. Mint the OpenPGP-armored public half you'll embed + serve.
#    Re-deriving from the PEM via the `local` backend gives the
#    same shape goreleaser produces in production, so the trust
#    set is identical.
gtb keys mint \
    --backend local --key-id signing.pem \
    --name "MyTool Release" \
    --email release@mytool.example \
    --output release.asc \
    --created 2026-06-08T00:00:00Z
```

The end state: `rotation.asc` + `release.asc` are ready to drop into
`internal/trustkeys/keys/` and publish via WKD. Nothing else is
needed for verification to work.

For production, step 2 swaps out: instead of generating a local PEM,
you apply the [`terraform-aws-signing-kms`](https://gitlab.com/phpboyscout/terraform-aws-signing-kms)
module and then run:

```sh
gtb keys mint \
    --backend aws-kms --key-id alias/mytool-release-signing-v1 \
    --kms-region eu-west-2 \
    --name "MyTool Release" \
    --email release@mytool.example \
    --output release.asc
```

The private half never leaves AWS. The OIDC trust on the IAM role
ensures only tag pipelines on `mytool`'s git repository can call
`kms:Sign` — not branches, not MRs, not humans.

## Why an external trust anchor (WKD)?

The embedded trust set protects against any attacker who can change
the release registry's contents *after* the user installed the
binary. But it doesn't protect against an attacker who compromised
the registry *before* the user installed — because that attacker
could embed their own trust set in their poisoned binary.

The WKD cross-check closes that gap. The WKD endpoint is a static
file on a domain you administer **independently** from your release
registry (we recommend Cloudflare Pages on a separate account with
its own MFA factor — see the
[`phase2-signing-prep`](../../development/phase2-signing-prep.md) doc).

A successful poisoning attack now requires an attacker to compromise
both the release registry **and** your WKD-hosting account. That's a
materially higher bar.

## How the verifier finds your WKD endpoint

There is only **one** configurable input on the verifier side: the
release email (`update.external_key_email`, e.g.
`release@yourdomain.com`). Everything else — the hostname, the path,
the file location — is mechanically derived from that email per
[draft-koch-openpgp-webkey-service §3.1][wkd-draft], the protocol
spec.

[wkd-draft]: https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/

Walked through for `release@phpboyscout.uk`:

| Step | Value |
|------|-------|
| Configured | `release@phpboyscout.uk` |
| Split on `@` → local-part | `release` |
| Split on `@` → domain | `phpboyscout.uk` |
| Local-part hash (SHA-1 → z-base-32) | `y84sdmnksfqswe7fxf5mzjg53tbdz8f5` |
| **Advanced URL** (tried first) | `https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/y84sdmnksfqswe7fxf5mzjg53tbdz8f5` |
| **Direct URL** (fallback on 404) | `https://phpboyscout.uk/.well-known/openpgpkey/hu/y84sdmnksfqswe7fxf5mzjg53tbdz8f5` |

### Why specifically `openpgpkey.<domain>`?

The literal string `openpgpkey.` is **not a `gtb` convention** — it
is mandated by the WKD spec for the "advanced" URL form. WKD
clients (GnuPG, our `WKDResolver`, Sequoia, anything implementing
the protocol) all hardcode this prefix when constructing the
advanced lookup URL, because the whole point of the spec is "I have
an email, give me the key without knowing anything else about the
target domain."

A WKD-publishing domain can serve either or both URL forms:

- **Advanced** is the production default. Hosting the key material
  under a dedicated subdomain (`openpgpkey.phpboyscout.uk`) lets you
  put the WKD endpoint behind separate TLS, separate hosting, and
  ideally a separate registrar account — without disturbing the
  main site. This is what the trust-anchor independence story relies
  on (see [phase2-signing-prep][prep] for the Cloudflare Pages recipe).
- **Direct** would serve the WKD layout from the bare `phpboyscout.uk`
  domain. The resolver falls back to it on 404, so a domain that
  hosts WKD on its main site also works.

[prep]: ../../development/phase2-signing-prep.md

### Consequence: one variable to align

If you're consuming GTB as a framework and adopting this signing
chain in your own tool, **the only thing you align across the
framework, the DNS, and the hosting account is the email.** Set
`verify.DefaultExternalKeyEmail` in your tool's init (or pass it via
`update.external_key_email` config), mint your key with that email
on the UID, stand up the WKD endpoint at `openpgpkey.<yourdomain>`,
publish the key. The framework derives the URL; the verifier knows
where to look.

## What you'll see in `gtb update` logs

Every update emits two structured log lines that tell you exactly
which trust anchors were consulted:

```
INFO update signature verification configured resolver=<name>
INFO signature verified resolver=<name>
```

The `resolver=` value names the concrete `KeyResolver` chain that
produced the trust set. For a customer-facing summary:

| `resolver=…` value | What it means |
|--------------------|---------------|
| `composite[embedded,wkd:<host>]` | **The intended Phase 2 default.** Both trust anchors (the keys baked into the binary at build time AND the keys served live from your WKD endpoint) were fetched and **must agree** on the same fingerprints before the update proceeds. Two-of-three trust-anchor independence — the security property this design exists to deliver. |
| `embedded` | Single-anchor verification: only the keys baked into the binary at build time were consulted. Cryptographically sound (the signature still has to validate against a trusted key), but lower defence-in-depth than the composite default. Most commonly seen in tools where `update.external_key_email` hasn't been configured. |
| `wkd:<host>` | Single-anchor verification: only the keys served by that WKD endpoint were consulted. Useful for tools that intentionally don't embed any keys (e.g. server-side automation that trusts its DNS but doesn't ship binary releases). |

If you ever see `ErrKeyResolverMismatch` in the logs, that's an
**active-tampering signal** — two trust anchors disagreed on which
key is currently valid. Investigate which anchor is wrong before
re-running. This is the highest-priority signal the verifier emits.

The full table of log shapes and what to do about each lives in the
[Signature Verification component reference][svdocs] under
"Interpreting verifier log output".

[svdocs]: ../components/setup/signature-verification.md#interpreting-verifier-log-output

## Where `gtb` stops and `goreleaser` starts

`gtb keys` produces the trust artefacts (public keys). It does **not**
produce the signature on each release. That's `goreleaser`'s job, via
a `signs:` block in your `.goreleaser.yaml` that shells out to AWS
KMS via OIDC at tag-pipeline time.

The chain in tabular form:

| Step | Who runs it | What it produces |
|---|---|---|
| Provision KMS key + IAM role | `tofu apply` (one-off) | KMS key alias, signer role ARN |
| Mint the OpenPGP public half | `gtb keys mint` (one-off) | `release.asc` |
| Embed the public half | `go:embed all:keys` (every build) | trust set baked into binary |
| Publish the public half via WKD | manual or scripted (one-off) | armored key served from your domain |
| Sign each release manifest | `goreleaser` on tag pipeline | `checksums.txt.sig` |
| Verify on install/update | `gtb`-derived tool's self-update | trust decision |

## What's deliberately out of scope (for now)

- **Generating keys *in* an HSM via `gtb`.** KMS keys are
  provisioned by Terraform — that's a one-off infrastructure
  operation, not a release-time command.
- **Publishing to WKD via `gtb`.** WKD hosting is an externally-
  administered concern; we don't presume your DNS, your TLS
  provider, or your static-host of choice. We provide the recipe
  (Cloudflare Pages Direct Upload) in the
  [phase2-signing-prep doc](../../development/phase2-signing-prep.md).
- **Sigstore / cosign / Rekor transparency log.** Phase 3 of the
  parent spec. The architecture supports adding a Sigstore-backed
  trust path alongside OpenPGP without breaking the existing flow.

## Related

- [`gtb keys mint`](../../how-to/mint-signing-key.md) — the production
  signing key recipe.
- [`gtb keys generate-rotation`](../../how-to/generate-rotation-key.md)
  — the offline-storage flow.
- [Adding a signing backend](../../how-to/add-signing-backend.md) — for
  GCP KMS, Vault, YubiKey, etc.
- [openpgpkey](../components/openpgpkey.md) and
  [signing](../components/signing.md) — the programmatic APIs, now
  extracted into the standalone
  [signing module](https://signing.phpboyscout.uk)
  (`gitlab.com/phpboyscout/go/signing`).
- [Phase 2 spec](../../development/specs/2026-04-02-remote-update-checksum-verification.md)
  — verifier design.
