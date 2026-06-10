---
title: "`gtb sign` — detached OpenPGP signing via a pluggable backend"
description: "Add `gtb sign`, a top-level CLI that produces ASCII-armored OpenPGP detached signatures over arbitrary files using a `crypto.Signer` from the existing `pkg/signing` backend registry (AWS KMS, local PEM, etc.). Library side: `pkg/openpgpkey.DetachSign`. Pairs with the verifier in `pkg/setup` (MR !9) to close the release-binary signing chain without requiring `gpg` in production CI."
status: IMPLEMENTED
date: 2026-06-09
tags:
  - specification
  - signing
  - openpgp
  - kms
  - cli
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# `gtb sign` — detached OpenPGP signing via a pluggable backend

Authors
:   Matt Cockayne

Date
:   2026-06-09

Status
:   IMPLEMENTED (shipped in MR !35, 2026-06-09; live since v0.12.2)

## Summary

Add a new top-level `gtb sign` command and an underlying
`pkg/openpgpkey.DetachSign` library function. The command takes a
file, a signing backend (`aws-kms` / `local` / future
GCP/Vault/YubiKey), and a key identifier; produces an
ASCII-armored OpenPGP detached signature compatible with the
verifier in `pkg/setup/signing.go` (MR !9) and with `gpg --verify`.

```sh
gtb sign \
    --backend aws-kms \
    --kms-region eu-west-2 \
    --key-id alias/gtb-release-signing-v1 \
    --name "PHP Boy Scout Release" \
    --email release@phpboyscout.uk \
    --output checksums.txt.sig \
    checksums.txt
```

Replaces `scripts/sign-release.sh`'s `gpg --detach-sign` call in MR
!9. The signing operation never sees the private key — only the
backend's remote `Sign` round-trip does.

## Motivation

Phase 2 needs CI to mint a detached OpenPGP signature over
`checksums.txt` on every tag pipeline. The held-MR-!9 script uses
`gpg --local-user $GTB_SIGNING_KEY` which assumes a gpg-resolvable
key — workable only if you expose the KMS-held key through a
PKCS#11 provider / scdaemon, a moving-parts setup we explicitly
want to avoid (same reason `gtb keys mint` exists instead of
shelling out to gpg).

A native Go signer using the same `pkg/signing` registry closes
the loop: one consistent path for minting keys, publishing them
via WKD, and producing the detached signatures the verifier
expects. `gpg` is no longer in the release pipeline.

## Design decisions

### D1 — Command placement (top-level)

`gtb sign` is a top-level verb, not a subcommand under
`gtb keys`. The `keys` group is for **key lifecycle** (mint,
generate, wkd); `sign` is for **using** a key to operate on a file.
Conventional with `cosign sign`, `signify -S`, `minisign -S`,
`signtool sign`.

Implementation lives in a new `internal/cmd/sign/` package (gtb-only;
not scaffoldable to downstream tools — same boundary as `keys/`).

### D2 — Library-first split

Per the library-first principle, the signing primitive lives in
`pkg/openpgpkey/sign.go` (Beta tier) as `DetachSign`. Shape:

```go
// DetachSign computes an ASCII-armored OpenPGP detached signature
// over data using signer. publicKey is the armored OpenPGP public-key
// block that identifies the signer; its fingerprint becomes the
// signer-fingerprint embedded in the signature. signer must produce
// signatures verifiable against publicKey's public half (we check
// this at call time and refuse to proceed if they disagree).
//
// data is read in full (the signature is over the whole content;
// no streaming).
//
// sigCreationTime is independent of the key's creation time embedded
// in publicKey; it stamps the signature itself.
func DetachSign(signer crypto.Signer, publicKey []byte, data io.Reader, sigCreationTime time.Time) ([]byte, error)
```

Uses the same `crypto.Signer` interface and OpenPGP packet framing
as `ArmoredPublicKey`. An opaque KMS-backed signer works without
the private key ever leaving the HSM.

**API revision note**: an earlier draft took
`(signer, name, email, data, creationTime)` and rebuilt the entity
inside DetachSign — but that conflated two distinct timestamps (the
key's creation time and the signature's creation time). A test run
against a freshly-minted key surfaced the bug: the signature named a
phantom-key fingerprint derived from the sig-time `--created` rather
than the actual key's published fingerprint, and gpg rejected the sig
with "No public key". The revised API takes the armored public key as
the identity source; the signer must produce signatures verifiable
against it (checked via RSA n/e comparison before signing).

### D3 — Algorithms (RSA only in v0.1)

Same constraint as `ArmoredPublicKey`: `signer.Public()` must
return `*rsa.PublicKey`. Returns `ErrUnsupportedKeyType` otherwise.
AWS KMS asymmetric `SIGN_VERIFY` keys are RSA-only; Ed25519
signing via `gtb sign` is additive and can be added when a
backend that supplies Ed25519 signers (other than the
self-generated local key) appears.

### D4 — Reproducibility: `--created` flag (signature timestamp)

The CLI exposes `--created <rfc3339>` (default: now). Pinning this
produces byte-identical signatures across re-runs over the same
content (with the same signing key) — useful for SLSA-style
attestation and for diffing two signed builds.

`--created` is the **signature's** creation time, not the key's. The
key's creation time is whatever was set when the key was minted
(`gtb keys mint --created`) and is now baked into the `--public-key`
file. Mixing the two was a bug surfaced during implementation — see
the API revision note under D2.

For deterministic output, go-crypto's default `NonDeterministicSignaturesViaNotation`
(a random-salt subpacket added as a fault-injection defence) is
explicitly disabled in DetachSign. The defence protects against
hardware-fault attacks on the *signing* machine; in our setup the
signer is AWS KMS, which has its own HSM-level fault protections.

### D5 — One file at a time

The CLI signs one input file per invocation:

```sh
gtb sign --output checksums.txt.sig checksums.txt
```

This matches goreleaser's `signs` block contract (`signs[*].args`
is called once per artifact with `${artifact}` / `${signature}`)
and keeps error handling simple — one input, one output, one exit
code reflecting that pair.

If `--output` is omitted, defaults to `<input>.sig`.

### D6 — AWS auth outside `gtb sign`

The CLI does **not** know about OIDC. CI is responsible for
exporting `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` /
`AWS_SESSION_TOKEN` (typically via `aws sts
assume-role-with-web-identity` in a `before_script`) before
invoking `gtb sign`. The signing-kms backend uses the standard
AWS SDK credential chain, which picks those up automatically.

This keeps `gtb sign` provider-agnostic — the same command works
with GitHub Actions OIDC, BitBucket Pipelines OIDC, or a
long-lived access key in a local terminal. Tightly coupling
`gtb sign` to a specific OIDC flow would require maintaining a
matrix of CI-platform integrations the framework doesn't need to
own.

### D7 — Output format

Single output: armored OpenPGP detached signature
(`-----BEGIN PGP SIGNATURE-----` … `-----END PGP SIGNATURE-----`).
That's what `TrustSet.VerifyManifestSignature` (MR !9) consumes
via `openpgp.CheckArmoredDetachedSignature`. No optional binary
form in v0.1.

### D8 — Validation

- `--backend` must be a registered backend name (same lookup as
  `gtb keys mint`).
- `--key-id` and `--public-key` required. `--public-key` is the
  armored OpenPGP public-key file the signer's RSA key must match
  (n + e comparison); mismatch returns
  `"signer's RSA public half does not match the public key block"`.
- Input file must exist and be readable.
- `--output` defaults to `<input>.sig`; refuses to equal the
  input path (would clobber the artifact).
- `--created` parses as RFC 3339; default is `time.Now().UTC()`
  rounded to whole seconds (OpenPGP signature packets store
  uint32 seconds since epoch, so sub-second precision is lost
  anyway — rounding makes the timestamp explicit and test
  fixtures predictable).
- Successful sign prints the public-key fingerprint at INFO so
  operators can cross-check against the expected key without a
  follow-up `gpg --verify`.

## Verification plan

1. **Unit**: `pkg/openpgpkey/sign_test.go` covers concrete-RSA and
   opaque-signer (KMS simulation) paths, fingerprint pinning when
   `creationTime` is constant, golden test that the produced
   signature is accepted by go-crypto's
   `openpgp.CheckArmoredDetachedSignature` against the matching
   public key (closes the loop within the same package).
2. **CLI**: `internal/cmd/sign/sign_test.go` covers happy path,
   flag validation, input/output equality refusal, missing-file
   error, fingerprint logging.
3. **Cross-tool**: integration test (gated `INT_TEST_SIGN=1`)
   that uses `gpg --verify` on the produced .sig against the
   matching public key (the recipient's expected behaviour). Catches
   any drift between our framing and the OpenPGP standard.
4. **End-to-end**: locally, against the real Phase 2 KMS key, sign
   a 10 KB random file and verify with both go-crypto AND `gpg
   --verify`. Documented in the eventual `docs/how-to/sign-releases.md`.

## Out of scope

- Inline (`--clearsign`) and message-embedded signatures. Goreleaser
  only wants detached.
- Signing entire directories or git refs. Per-artifact only.
- Verification: `gtb verify` is a natural counterpart but the
  existing `gpg --verify` is fine for operator-facing checks, and
  the in-binary verifier (`pkg/setup`) already covers the
  self-update path. Add when there's a concrete asker.

## Resolutions (open questions confirmed with user 2026-06-09)

1. **Command location** — top-level `gtb sign` (not `gtb keys
   sign`). Matches conventional signing-tool UX.
2. **`--created` flag** — present, defaults to now, allows pinning.
3. **Multi-file** — strict one-at-a-time, matches goreleaser's
   per-artifact invocation contract.
4. **OIDC location** — outside the tool. CI assumes the role in a
   `before_script` and exports `AWS_*` env vars; gtb-sign reads
   them via the SDK's standard credential chain.

## Related

- [keys-mint spec](2026-06-08-keys-mint-command.md) — establishes the
  `pkg/signing` backend registry that `gtb sign` reuses.
- [keys-wkd spec](2026-06-09-keys-wkd-command.md) — parallel
  pattern: library function + thin CLI wrapper.
- [Phase 2 signing prep doc](../phase2-signing-prep.md) — Gate 1
  selected AWS KMS as the signing-key store; this spec is the
  consumer.
- [MR !9](https://gitlab.com/phpboyscout/go-tool-base/-/merge_requests/9)
  — the verifier side; this signer's output is what its
  `TrustSet.VerifyManifestSignature` consumes.
