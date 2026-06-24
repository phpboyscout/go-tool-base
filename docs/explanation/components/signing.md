---
title: pkg/signing
description: Backend registry that lets `gtb keys mint` (and downstream tools) target arbitrary HSM/KMS/keyring back-ends through a single `Backend` interface.
date: 2026-06-08
tags: [components, signing, kms, registry, backend]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `pkg/signing`

API stability: **Beta** (per
[`docs/about/api-stability.md`](../../reference/api-stability.md) and spec
[D6](../../development/specs/2026-06-08-keys-mint-command.md)).

## What it does

A tiny registry. Each backend (AWS KMS, local PEM file, GCP KMS,
HashiCorp Vault, …) implements one interface and registers itself
from its package's `init()`. Downstream binaries opt-in by
**blank-importing** the backend package. `gtb keys mint --backend
<name>` then resolves the registered backend, invokes its
`NewSigner`, and hands the resulting `crypto.Signer` to
`pkg/openpgpkey` for OpenPGP packet assembly.

This is the same activate-by-side-effect pattern used by
`net/http/pprof`, `image/*` decoders, and the framework's own
`pkg/credentials/keychain`.

## Public API

```go
// Backend constructs a crypto.Signer for an HSM/KMS-held signing key.
type Backend interface {
    Name() string
    RegisterFlags(fs *pflag.FlagSet)
    NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
}

// Register, Get, Names — the registry mutators / accessors.
func Register(b Backend)                  // panics on duplicate / nil / empty name
func Get(name string) (Backend, error)    // returns ErrUnknownBackend with a "available: ..." list
func Names() []string                     // sorted

var ErrUnknownBackend                     // sentinel

// ResetForTesting clears the registry. Tests-only — production callers
// must not invoke. The "ForTesting" suffix is the Go-standard signal.
func ResetForTesting()
```

## How registration works

Each backend lives in its own package. A typical layout:

```go
// pkg/signing/yourbackend/yourbackend.go
package yourbackend

import "gitlab.com/phpboyscout/go-tool-base/pkg/signing"

type backend struct{ /* flag-bound state */ }

func (b *backend) Name() string                                                      { return "your-name" }
func (b *backend) RegisterFlags(fs *pflag.FlagSet)                                   { /* ... */ }
func (b *backend) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) { /* ... */ }

func init() {
    signing.Register(&backend{})
}
```

A consumer blank-imports the backend to activate it:

```go
// cmd/your-cli/main.go
import (
    _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/kms"   // aws-kms
    _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/local" // local
)
```

The standard `gtb` binary blank-imports both built-in backends in
`internal/cmd/root/root.go`. A regulated downstream that doesn't
want the AWS SDK in its dependency tree omits the `kms` import; the
linker dead-code-strips the entire AWS SDK and the binary stays
slim. The framework verifies this property via a no-AWS smoke
binary (`cmd/gtb-no-aws-smoke`) — see
[Compile-time backend opt-out](#compile-time-backend-opt-out).

## Built-in backends

### `aws-kms` (`pkg/signing/kms`)

Wraps an AWS KMS asymmetric `SIGN_VERIFY` key (RSA-4096).
Requires `kms:GetPublicKey` and `kms:Sign` on the target key. The
backend resolves AWS credentials through the standard SDK chain
(env / shared config / EC2 IMDS / web-identity / OIDC). Maps the
hash digest length to the matching `RSASSA_PKCS1_V15_SHA_*`
signing algorithm.

The signer implements **only** `RSASSA-PKCS1-v1_5`. If a caller
requests `RSASSA-PSS` by passing `*rsa.PSSOptions` to `Sign`, the
signer returns `ErrPSSUnsupported` rather than silently downgrading to
PKCS#1 v1.5 — a silent scheme swap behind an exported `crypto.Signer`
is a contract violation, so the signer refuses loudly instead.

Flags:

- `--kms-region` (default `eu-west-2`)

### `local` (`pkg/signing/local`)

Loads an RSA private key from a PEM file on disk. Supports
unencrypted PKCS#1 (`-----BEGIN RSA PRIVATE KEY-----`) and PKCS#8
(`-----BEGIN PRIVATE KEY-----`). **Refuses encrypted PEMs** in
v0.1 — the standard library doesn't expose a PKCS#8 decryption
function and shelling out is out of scope. Use filesystem-level
encryption (LUKS, FileVault, age) until v0.2 adds in-tool key
encryption.

The `local` backend is intended for the onboarding tutorial,
local development, and the rotation-authority key signing path. It
is **not** intended for production CI: production runs through
`aws-kms` (or another HSM-backed backend).

Flags: none. The key path comes from the generic `--key-id` flag,
which the `local` backend interprets as a filesystem path.

## Error handling

`Get` wraps `ErrUnknownBackend` with the requested name and the
list of registered backends:

```
ErrUnknownBackend: "gcp-kms" (available: aws-kms, local)
```

When **no** backends are linked into the binary (regulated build
that imports neither `kms` nor `local`), the wrap calls that out
explicitly:

```
ErrUnknownBackend: "aws-kms" (no backends are registered — this binary was built without any signing backends compiled in)
```

Callers check membership via `errors.Is(err, signing.ErrUnknownBackend)`.

## Compile-time backend opt-out

Because backends register via blank-import, omitting the import is
all it takes to drop a backend from a build:

```go
// Standard gtb: ships both
import (
    _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/kms"
    _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/local"
)

// Regulated build: ships only local — AWS SDK is not linked
import (
    _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/local"
)
```

The repository's `cmd/gtb-no-aws-smoke/main.go` is a compile-time
fixture that imports `gtb` minus the `kms` backend. CI confirms it
builds (i.e. nothing in `internal/cmd/keys/` accidentally creates
a hard dependency on the AWS SDK) and that `Names()` returns
`["local"]` at runtime.

## Concurrency

`Register` is safe to call from multiple `init()` functions (the
registry holds a `sync.RWMutex`). `Get`, `Names` are concurrent
readers. `ResetForTesting` should only be called from a test in
sole control of the process — it's a clean-slate operation.

## Testing your backend

The framework's signing backends use a fake-client interface
pattern for unit tests so the SDK doesn't need a network. Pattern:

1. Define an interface over the SDK methods your backend calls.
2. Have your `newSigner` accept that interface (not the concrete
   SDK client).
3. Production constructs the real SDK client; tests construct a
   fake.

See `pkg/signing/kms/kms_test.go` and
`pkg/signing/local/local_test.go` for the canonical examples.

For end-to-end tests that exercise the registry itself, call
`signing.ResetForTesting()` in `t.Cleanup` to keep registrations
from leaking across tests.

## Adding a new backend

See [How-to: add a signing backend](../../how-to/add-signing-backend.md).

## Related

- [Release-binary signing concept](../concepts/release-binary-signing.md)
- [`pkg/openpgpkey`](openpgpkey.md) — the consumer that turns a
  `crypto.Signer` into an OpenPGP packet.
- [Spec D6 + Resolution 8](../../development/specs/2026-06-08-keys-mint-command.md)
  — why `aws-kms` + `local` instead of the originally-proposed
  `gpg` backend.
