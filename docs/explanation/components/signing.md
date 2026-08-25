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
    - **Module documentation:** [signing.phpboyscout.uk](https://signing.phpboyscout.uk)

    The `gtb` CLI behaviour is unchanged, only the Go import paths moved.

    The `sign` / `keys` **commands** that drive this registry now live in the
    shareable [`go/signing-cli`](https://signing-cli.go.phpboyscout.uk) module,
    which go-tool-base and the standalone `sigillum` CLI both attach.

## What it does

A tiny registry. Each backend (AWS KMS, local PEM file, GCP KMS,
HashiCorp Vault, …) implements one interface and registers itself
from its package's `init()`. Downstream binaries opt-in by
**blank-importing** the backend package. `gtb keys mint --backend
<name>` then resolves the registered backend, invokes its
`NewSigner`, and hands the resulting `crypto.Signer` to
[`openpgpkey`](openpgpkey.md) for OpenPGP packet assembly.

This is the same activate-by-side-effect pattern used by
`net/http/pprof`, `image/*` decoders, and the framework's own
`go/credentials/keychain`.

## The contract is CLI-agnostic

As part of the extraction the `Backend` contract was narrowed to two
methods, it no longer references any CLI types:

```go
// gitlab.com/phpboyscout/go/signing
type Backend interface {
    Name() string
    NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
}
```

`RegisterFlags` is **no longer part of the `Backend` contract**. A
backend that needs CLI flags (e.g. aws-kms's `--kms-region`) implements
an *optional* interface that the CLI front-end type-asserts for; a
backend with no flags simply omits it. The only seams are stdlib:
`crypto.Signer` for keys and `*slog.Logger` for logging. This keeps the
module free of any dependency on Cobra/pflag or go-tool-base.

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

## Reference

The full registry API (`Register`, `Get`, `Names`, `ErrUnknownBackend`,
`ResetForTesting`), error-handling shapes, the compile-time backend
opt-out story, concurrency guarantees, and the backend test pattern now
live in the
[signing module documentation](https://signing.phpboyscout.uk) and on
[pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/signing).

## Adding a new backend

See [How-to: add a signing backend](../../how-to/add-signing-backend.md).

## Related

- [Release-binary signing concept](../concepts/release-binary-signing.md)
- [`openpgpkey`](openpgpkey.md): the consumer that turns a
  `crypto.Signer` into an OpenPGP packet.
</content>
