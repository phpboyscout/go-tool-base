---
title: "openpgpkey"
description: "OpenPGP packet assembly from a `crypto.Signer`: the primitive that wraps an HSM/KMS-held RSA key as an ASCII-armored OpenPGP public key. Now a standalone module."
date: 2026-06-08
tags: [components, openpgp, signing, kms, rsa]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `openpgpkey`

!!! info "Extracted into the signing module"
    This package was extracted from go-tool-base. It now lives in the
    standalone, independently-versioned **signing** module at
    **`gitlab.com/phpboyscout/go/signing/openpgpkey`** (v0.1.0).
    go-tool-base consumes it as an ordinary dependency.

    - **API reference:** [pkg.go.dev/gitlab.com/phpboyscout/go/signing/openpgpkey](https://pkg.go.dev/gitlab.com/phpboyscout/go/signing/openpgpkey)
    - **Module documentation:** [signing.go.phpboyscout.uk](https://signing.go.phpboyscout.uk)

    The `gtb` CLI behaviour is unchanged, only the Go import path moved.
    The change is relevant to anyone writing Go against the package.

## How `gtb` uses it

Mints an ASCII-armored OpenPGP public key from any `crypto.Signer` whose
`Public()` returns `*rsa.PublicKey`, so an opaque HSM-backed signer (AWS KMS,
GCP KMS, YubiKey) works without the private key ever leaving the HSM. Inside
the `gtb` binary it backs `gtb keys mint`, `gtb keys generate` (RSA path),
`gtb keys wkd` (Web Key Directory tree generation), and the `DetachSign` half
of `gtb sign` (the per-release `checksums.txt` → `checksums.txt.sig` step).
The full API reference, algorithm/version details, reproducibility notes, and
worked examples live in the
[signing module documentation](https://signing.go.phpboyscout.uk) and on
[pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/signing/openpgpkey).

For the operator-facing recipes, see:

- [How-to: mint a signing key](../../how-to/mint-signing-key.md)
- [How-to: sign release artefacts](../../how-to/sign-releases.md)
- [How-to: publish via WKD](../../how-to/publish-wkd.md)

## Related

- [Release-binary signing concept](../concepts/release-binary-signing.md): the big-picture story
- [`signing`](signing.md): the backend registry that drives
  `gtb keys mint`
