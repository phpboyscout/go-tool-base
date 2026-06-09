---
title: pkg/openpgpkey
description: OpenPGP packet assembly from a `crypto.Signer` — the primitive that wraps an HSM/KMS-held RSA key as an ASCII-armored OpenPGP public key.
date: 2026-06-08
tags: [components, openpgp, signing, kms, rsa]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `pkg/openpgpkey`

API stability: **Beta** (per
[`docs/about/api-stability.md`](../about/api-stability.md) and spec
[D4](../development/specs/2026-06-08-keys-mint-command.md)).

## What it does

Mints an ASCII-armored OpenPGP public key from any `crypto.Signer`
whose `Public()` returns `*rsa.PublicKey`. The OpenPGP self-signature
is produced by calling `signer.Sign(...)` exactly once — so an opaque
HSM-backed signer (AWS KMS, GCP KMS, YubiKey) works without the
private key ever leaving the HSM.

Used by:

- `internal/cmd/keys/mint.go` (production) and
- `internal/cmd/keys/generate.go` (RSA path) inside the `gtb` binary.

Anyone with a `crypto.Signer` that produces RSA signatures can call
this package directly to produce an embeddable / WKD-publishable
`.asc`.

## Public API

```go
// ArmoredPublicKey is the one-call form. Returns the armored bytes
// in memory; callers persist them however they like.
func ArmoredPublicKey(signer crypto.Signer, name, email string, creationTime time.Time) ([]byte, error)

// WriteArmoredPublicKey streams directly to an io.Writer. Useful for
// writing to a file or network without an intermediate buffer.
func WriteArmoredPublicKey(w io.Writer, signer crypto.Signer, name, email string, creationTime time.Time) error

// Entity returns the underlying *openpgp.Entity if you need both the
// public and private halves. Used by `gtb keys generate` to write
// the two halves to separate files.
func Entity(signer crypto.Signer, name, email string, creationTime time.Time) (*openpgp.Entity, error)

// Sentinel for non-RSA signers. Use errors.Is to check.
var ErrUnsupportedKeyType
```

### What `signer` must provide

- `signer.Public()` returns `*rsa.PublicKey`. Anything else returns
  `ErrUnsupportedKeyType`.
- `signer.Sign(rand, digest, opts)` produces a PKCS#1 v1.5 RSA
  signature over `digest`. `opts.HashFunc()` is one of SHA-256,
  SHA-384, or SHA-512 (whichever the underlying OpenPGP packet
  framing chooses — that's an internal go-crypto detail; your signer
  must support all three to be robust).

### Why RSA only

AWS KMS — the production HSM in scope for v0.1 — only exposes RSA
for asymmetric `SIGN_VERIFY` keys. Ed25519 minting flows through
`internal/cmd/keys/generate.go` instead, which uses go-crypto's own
`openpgp.NewEntity(...)` with `PubKeyAlgoEdDSA` to produce v4 EdDSA
(algorithm 22) keys that GnuPG 2.4 imports cleanly.

A consumer with a Backend producing Ed25519 signers via a route that
*does* land in mainstream gpg would be a reason to add Ed25519 here
additively. None exists in v0.1.

## Example: wrap a local RSA key

```go
priv, _ := rsa.GenerateKey(rand.Reader, 4096)

armored, err := openpgpkey.ArmoredPublicKey(priv,
    "MyTool Release",
    "release@mytool.example",
    time.Now().UTC(),
)
if err != nil {
    log.Fatal(err)
}

os.WriteFile("release.asc", armored, 0o644)
```

## Example: wrap a KMS-backed signer

```go
// signer.Public() returns *rsa.PublicKey; signer.Sign() calls
// kms.Sign() under the hood. See pkg/signing/kms for the concrete
// implementation.
signer, _ := kms.NewSigner(ctx, "eu-west-2", "alias/release-signing-v1")

armored, err := openpgpkey.ArmoredPublicKey(signer, "...", "...", time.Now().UTC())
```

Exactly **one** `kms.Sign` round-trip happens during the mint: the
self-signature on the binding between the public-key packet and the
User ID.

## Reproducibility

`creationTime` is folded into the OpenPGP fingerprint. Two mints of
the same key one second apart produce different fingerprints. To
re-derive an existing key after losing the `.asc` file, pin
`creationTime` to the original value.

## Algorithm + version details (for the curious)

- **OpenPGP packet version**: v4. RFC 4880 native, accepted by every
  modern OpenPGP implementation.
- **RSA public-key packet**: produced via
  `packet.NewRSAPublicKey(creationTime, *rsa.PublicKey)`.
- **Self-signature**: positive cert (`SignatureTypePositiveCert`),
  produced by `Entity.AddUserId` which routes through
  `signer.Sign(...)`.
- **Hash**: chosen by go-crypto/openpgp at sign time; typically
  SHA-256 for RSA-2048/3072 and SHA-384/512 for larger keys.

## Web Key Directory (WKD) tree generation

The package also produces a complete WKD layout per
[draft-koch-openpgp-webkey-service §3.1][wkd], paired with
`pkg/setup/signing_wkd.go`'s client-side `WKDResolver`. Use this to
publish public keys at `openpgpkey.<yourdomain>` so the verifier can
cross-check the embedded key against an externally-served copy on
each `gtb update`.

```go
// One bucket per email — keys are concatenated under hu/<hash> in
// lexicographic fingerprint order for reproducible output.
type Entry struct {
    Email string
    Keys  [][]byte // armored *or* binary; auto-detected
}

type Method string
const (
    MethodAdvanced Method = "advanced" // default — dedicated openpgpkey.<domain> subdomain
    MethodDirect   Method = "direct"   // root-domain layout (no openpgpkey/<domain>/ level)
)

type Options struct {
    Method            Method
    SubmissionAddress string // optional; if non-empty, written to the domain root for WKS-aware clients
}

func WriteWKDTree(outDir, domain string, opts Options, entries ...Entry) ([]string, error)
func WKDHash(email string) (string, error) // exposed for callers that just need the hash
```

Output for the standard PHP Boy Scout setup:

```
out/.well-known/openpgpkey/phpboyscout.uk/
├── policy                                       (empty; required by RFC)
├── submission-address                           (optional; configured via Options.SubmissionAddress)
└── hu/y84sdmnksfqswe7fxf5mzjg53tbdz8f5          (binary concatenated public keys for release@)
```

`WKDHash` and the underlying `zbase32Encode` are validated against
the reference implementation: a unit test feeds `joe.user@example.org`
and compares to the output of `gpg-wks-client --print-wkd-hash`. Any
drift from RFC compliance fails CI.

### Why a native generator

The reference tool, `gpg-wks-client`, is a thin shell wrapper over the
same packet routines `pkg/openpgpkey` already uses. Bringing
generation in-process means a downstream tool can publish WKD without
requiring a `gpg` install — the same property `gtb keys generate`
provides on the key-minting side.

For the operator-facing recipe (Cloudflare Pages Direct Upload), see
[How-to: publish via WKD](../how-to/publish-wkd.md).

[wkd]: https://datatracker.ietf.org/doc/html/draft-koch-openpgp-webkey-service-15

## Related

- [Release-binary signing concept](../concepts/release-binary-signing.md)
  — the big-picture story.
- [`pkg/signing`](signing.md) — the backend registry that drives
  `gtb keys mint`.
- [Spec D12](../development/specs/2026-06-08-keys-mint-command.md) —
  the (revised) RSA-only design decision and why Ed25519 minting
  lives in `internal/cmd/keys/`.
- [Spec 2026-06-09-keys-wkd-command](../development/specs/2026-06-09-keys-wkd-command.md)
  — the WKD generator design (D1–D8) and resolved open questions.
