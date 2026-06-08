---
title: Add a signing backend
description: Implement and register a third-party backend for `gtb keys mint` so consumers of your fork can sign against GCP KMS, HashiCorp Vault, YubiKey, or any other key store.
date: 2026-06-08
tags: [how-to, keys, backend, signing, extension]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add a signing backend

The standard `gtb` binary ships with two backends: `aws-kms` (AWS
KMS) and `local` (PEM file on disk). If you need to sign against
something else — GCP KMS, Azure Key Vault, HashiCorp Vault Transit,
a YubiKey — you implement a `signing.Backend`, register it from
your own `main` package, and `gtb keys mint --backend <name>` picks
it up.

This how-to walks through the contract and shows a minimal example
(a hypothetical `gcp-kms` backend).

## The Backend interface

```go
// pkg/signing/backend.go
type Backend interface {
    Name() string
    RegisterFlags(fs *pflag.FlagSet)
    NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
}
```

Three methods:

- **`Name()`** — the identifier the user types after `--backend`.
  Lowercase, kebab-case, must be unique across the process. Duplicate
  registration panics at `init()` time (fail-fast).
- **`RegisterFlags(fs)`** — your chance to declare backend-specific
  flags (region, endpoint, keyring path, etc.). Called by `gtb keys
  mint` before flag parsing, so the flags surface in `--help`.
- **`NewSigner(ctx, keyID)`** — given the user's `--key-id`,
  return a `crypto.Signer` whose `Public()` is an `*rsa.PublicKey`
  and whose `Sign()` makes the remote signing call. **`Public()`
  must return RSA in v0.1** — Ed25519 minting goes through
  `gtb keys generate`, not through the backend registry.

## Worked example: a `gcp-kms` backend

The full surface is ~80 lines of Go. Below is the shape; replace the
GCP SDK calls with the real ones for your provider.

### Step 1: implement the signer

```go
// pkg/yourorg/signing/gcp/signer.go
package gcp

import (
    "context"
    "crypto"
    "crypto/rsa"
    "crypto/x509"
    "io"

    kms "cloud.google.com/go/kms/apiv1"
    kmspb "cloud.google.com/go/kms/apiv1/kmspb"
    "github.com/cockroachdb/errors"
)

type gcpSigner struct {
    ctx    context.Context
    client *kms.KeyManagementClient
    name   string  // full key version resource path
    pub    *rsa.PublicKey
}

func newSigner(ctx context.Context, client *kms.KeyManagementClient, name string) (*gcpSigner, error) {
    out, err := client.GetPublicKey(ctx, &kmspb.GetPublicKeyRequest{Name: name})
    if err != nil {
        return nil, errors.Wrap(err, "gcp GetPublicKey")
    }

    block, _ := pem.Decode([]byte(out.GetPem()))
    if block == nil {
        return nil, errors.New("gcp returned non-PEM public key")
    }

    parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
    if err != nil {
        return nil, errors.Wrap(err, "parsing GCP public key DER")
    }

    rsaPub, ok := parsed.(*rsa.PublicKey)
    if !ok {
        return nil, errors.Newf("GCP key is %T; only RSA is supported", parsed)
    }

    return &gcpSigner{ctx: ctx, client: client, name: name, pub: rsaPub}, nil
}

func (s *gcpSigner) Public() crypto.PublicKey { return s.pub }

func (s *gcpSigner) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
    // Map opts.HashFunc() to GCP's CryptoKeyVersion_RSA_SIGN_PKCS1_*
    // ... details depend on the GCP SDK shape.
}
```

The implementation pattern matches `pkg/signing/kms` (AWS) and
`pkg/signing/local` (PEM file). Look at them as references when in
doubt.

### Step 2: implement the Backend

```go
// pkg/yourorg/signing/gcp/gcp.go
package gcp

import (
    "context"
    "crypto"

    kms "cloud.google.com/go/kms/apiv1"
    "github.com/spf13/pflag"

    "gitlab.com/phpboyscout/go-tool-base/pkg/signing"
)

type backend struct {
    // backend-specific flag-bound state goes here
    projectID string
}

func (b *backend) Name() string { return "gcp-kms" }

func (b *backend) RegisterFlags(fs *pflag.FlagSet) {
    fs.StringVar(&b.projectID, "gcp-project-id", "",
        "GCP project ID hosting the KMS key. Defaults to the SDK's resolved project.")
}

func (b *backend) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
    // ... resolve the full key resource name (using b.projectID +
    //     keyID), construct the GCP client, hand off to newSigner.
}

func init() {
    signing.Register(&backend{})
}
```

### Step 3: blank-import it from your binary's main

```go
// cmd/your-cli/main.go
package main

import (
    "gitlab.com/phpboyscout/go-tool-base/internal/cmd/root"

    // Activate the backends you want. Standard gtb ships aws-kms +
    // local; your tool can ship anything.
    _ "gitlab.com/yourorg/yourtool/pkg/signing/gcp"

    // ... rest of your main()
)
```

That's it. `your-cli keys mint --backend gcp-kms --key-id projects/p/locations/l/keyRings/k/cryptoKeys/c/cryptoKeyVersions/1 ...`
now works.

### Step 4 (optional): tests

Mirror the test pattern from `pkg/signing/kms/kms_test.go`:

- Define an interface over the SDK methods you call (e.g.
  `gcpClient { GetPublicKey, AsymmetricSign }`).
- The test supplies a fake; the production code uses the real GCP
  client.

This avoids depending on the GCP SDK's own mock framework and keeps
tests fast.

## Behaviour the registry enforces for you

Once you've called `signing.Register(&backend{})`, you get:

- **Discoverability.** `gtb keys mint --help` lists your backend's
  name alongside the built-in ones.
- **Flag wiring.** The flags you declared in `RegisterFlags` show
  up in `--help` and get parsed alongside the generic flags.
- **Error handling.** A user invoking
  `gtb keys mint --backend gcp-kms` against a binary that doesn't
  blank-import your package gets a clear
  `ErrUnknownBackend: "gcp-kms" (available: aws-kms, local)`.

## Common pitfalls

- **Don't expose your private-key material.** If your backend needs
  the secret bytes to operate (e.g. for an algorithm `crypto.Signer`
  can't represent), that's a sign the backend belongs at a lower
  level than this registry. Open an issue first.
- **Don't shell out.** The framework's promise is "no external
  tool dependencies". Stay native to Go.
- **RSA-only in v0.1.** The minting path requires RSA. If your
  backend can only produce Ed25519, route through
  `gtb keys generate` instead (which has its own non-`Backend`
  Ed25519 path).
- **Use a stable name.** Once consumers script against
  `--backend gcp-kms`, renaming it breaks them. Add aliases, never
  rename.

## Distribution

Your backend can live anywhere — it's a regular Go module that
imports `pkg/signing`. Common patterns:

- **Within your `yourtool` repo.** The simplest path; ship it as
  `internal/signing/gcp` if it's tool-specific or `pkg/signing/gcp`
  if other consumers might want it.
- **As a separate published package.** If you intend other tools to
  pick it up via blank-import.

The framework's `signing.Register` is the only integration point;
nothing else is needed.

## See also

- [`gtb keys mint`](mint-signing-key.md) — the user-facing surface
  your backend plugs into.
- [`pkg/signing`](../components/signing.md) — the registry API.
- [`pkg/signing/kms`](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/pkg/signing/kms/kms.go)
  and
  [`pkg/signing/local`](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/pkg/signing/local/local.go)
  — production examples.
