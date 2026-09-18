---
title: signing
description: Backend registry that lets `gtb keys mint` (and downstream tools) target arbitrary HSM/KMS/keyring back-ends through a single `Backend` interface. Now a standalone module.
date: 2026-06-08
tags: [components, signing, kms, registry, backend]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `signing`

!!! info "Extracted into the signing module"
    The `Backend` interface and the registry were extracted from
    go-tool-base. They now live in the standalone, independently-versioned
    **signing** module at **`gitlab.com/phpboyscout/go/signing`** (v0.1.0).
    go-tool-base consumes it as an ordinary dependency.

    - **API reference:** [pkg.go.dev/gitlab.com/phpboyscout/go/signing](https://pkg.go.dev/gitlab.com/phpboyscout/go/signing)
    - **Module documentation:** [signing.go.phpboyscout.uk](https://signing.go.phpboyscout.uk)

    The `gtb` CLI behaviour is unchanged, only the Go import paths moved.

    The `sign` / `keys` **commands** that drive this registry now live in the
    shareable [`go/signing-cli`](https://signing-cli.go.phpboyscout.uk) module,
    which go-tool-base and the standalone `sigillum` CLI both attach.

A tiny registry: each backend (AWS KMS, local PEM file, GCP KMS, HashiCorp
Vault, …) implements a two-method `Backend` interface (`Name`, `NewSigner`)
and registers itself from its package's `init()`. Downstream binaries opt in
by **blank-importing** the backend package; `gtb keys mint --backend <name>`
resolves the registered backend, invokes its `NewSigner`, and hands the
resulting `crypto.Signer` to [`openpgpkey`](openpgpkey.md) for OpenPGP packet
assembly. The full `Backend` contract, the registry API, and the
compile-time opt-out story are on the
[module documentation](https://signing.go.phpboyscout.uk).

## Built-in backends

The standard `gtb` binary blank-imports both built-in backends:

- **`aws-kms`**: a **separate module**,
  **`gitlab.com/phpboyscout/go/signing-aws-kms`** (package `awskms`).
  Wraps an AWS KMS asymmetric RSA-4096 `SIGN_VERIFY` key. Kept in its
  own module so a regulated downstream that omits the blank import keeps
  the AWS SDK out of the linked binary (linker dead-code elimination).
- **`local`**: **`gitlab.com/phpboyscout/go/signing/local`**. Loads an
  RSA private key from an unencrypted PKCS#1 or PKCS#8 PEM file. Intended
  for the onboarding tutorial, local development, and the
  rotation-authority signing path, not production CI.

## Adding a new backend

See [How-to: add a signing backend](../../how-to/add-signing-backend.md).

## Related

- [Release-binary signing concept](../concepts/release-binary-signing.md)
- [`openpgpkey`](openpgpkey.md): the consumer that turns a
  `crypto.Signer` into an OpenPGP packet.
