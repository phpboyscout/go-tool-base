---
title: Add a signing backend
description: Implement and register a third-party backend for `sigillum keys mint` / `sigillum sign` so a build of sigillum, or your own signer, can sign against GCP KMS, HashiCorp Vault, YubiKey, or any other key store.
date: 2026-06-08
tags: [how-to, keys, backend, signing, extension]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add a signing backend

The standard `sigillum` binary links two backends: `aws-kms` and
`local` (PEM file on disk). The `gtb` binary links none, because it no
longer ships the `sign` and `keys` commands. If you need to sign against
something else (GCP KMS, Azure Key Vault, HashiCorp Vault Transit, a
YubiKey) you implement a `signing.Backend` and register it from the
`main` package of the binary that signs. In sigillum that is a blank
import in `cmd/sigillum/signing.go`, so a build with your backend is a
fork or local build of [sigillum](https://gitlab.com/phpboyscout/sigillum)
with one more import, and `sigillum keys mint --backend <name>` /
`sigillum sign --backend <name>` then picks it up.

A generated release job signs with `sigillum`, which links only `aws-kms` and
`local`. To release with your own backend, point the `signs:` block in
`.goreleaser.yaml` at the binary that registers it, install that binary in the
release job, and list both files in `.gtb/ignore`.

!!! info "Canonical guide lives in the signing module"
    The backend registry was extracted from go-tool-base into the
    standalone **signing** module (`gitlab.com/phpboyscout/go/signing`,
    v0.1.0). The canonical, in-depth "Implement a custom backend"
    how-to: plus the trust model, threat model, and per-backend
    guides. Now lives in the
    [signing module documentation](https://signing.go.phpboyscout.uk),
    with the API on
    [pkg.go.dev/gitlab.com/phpboyscout/go/signing](https://pkg.go.dev/gitlab.com/phpboyscout/go/signing).

    This page keeps the essentials: the contract, how to register, and
    how to activate a backend in a sigillum build or your own signer.

## The Backend contract

The contract is **CLI-agnostic**, two methods, both backed by stdlib
seams (`crypto.Signer`, `context.Context`):

```go
// gitlab.com/phpboyscout/go/signing
type Backend interface {
    Name() string
    NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
}
```

- **`Name()`**: the identifier the user types after `--backend`.
  Lowercase, kebab-case, must be unique across the process. Duplicate
  registration panics at `init()` time (fail-fast).
- **`NewSigner(ctx, keyID)`**: given the user's `--key-id`, return a
  `crypto.Signer` whose `Public()` is an `*rsa.PublicKey` and whose
  `Sign()` makes the remote signing call. **`Public()` must return RSA
  in v0.1**, Ed25519 minting goes through `sigillum keys generate`, not
  through the backend registry.

Flags are not part of the `Backend` contract. A backend that needs
settings (e.g. aws-kms's `--kms-region`) describes them, and sigillum
turns each described setting into a flag; a backend with none stays free
of any CLI dependency. See the signing module's how-to for how a backend
describes its settings.

## Register from `init()`

```go
// yourtool/signing/gcp/gcp.go
package gcp

import (
    "context"
    "crypto"

    "gitlab.com/phpboyscout/go/signing"
)

type backend struct{ /* optional flag-bound state */ }

func (b *backend) Name() string { return "gcp-kms" }

func (b *backend) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
    // resolve the key, construct the SDK client, return a crypto.Signer
}

func init() {
    signing.Register(&backend{})
}
```

The implementation pattern matches the two reference backends:

- **`gitlab.com/phpboyscout/go/signing-aws-kms`** (package `awskms`): a
  separate module wrapping AWS KMS, and the example of a backend that
  contributes a CLI flag (`--kms-region`) through its settings.
- **`gitlab.com/phpboyscout/go/signing/local`**: the on-disk PEM backend,
  the example of a flag-less backend.

## Activate it in your binary

Backends register by side effect, so a blank import is all it takes:

```go
// cmd/sigillum/signing.go (or your own signer's main package)
import (
    // Activate the backends you want. Standard sigillum ships aws-kms +
    // local; your tool can ship anything.
    _ "gitlab.com/yourorg/yourtool/signing/gcp"
)
```

That's it. `sigillum keys mint --backend gcp-kms --key-id <id> ...`
(run from your build) now works, and omitting a backend's import drops it (and its SDK) from
the linked binary entirely.

## See also

- [signing module documentation](https://signing.go.phpboyscout.uk):
  the canonical "Implement a custom backend" guide, trust model, and
  per-backend reference.
- [`sigillum keys mint`](mint-signing-key.md): the user-facing surface your
  backend plugs into.
- [`signing` component](../explanation/components/signing.md): the
  registry overview.
- [`gitlab.com/phpboyscout/go/signing-aws-kms`](https://pkg.go.dev/gitlab.com/phpboyscout/go/signing-aws-kms)
  and
  [`gitlab.com/phpboyscout/go/signing/local`](https://pkg.go.dev/gitlab.com/phpboyscout/go/signing/local),
  production example backends.
