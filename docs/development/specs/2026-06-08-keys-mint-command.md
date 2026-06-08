---
title: "`gtb keys mint` — pluggable OpenPGP-from-HSM minter"
description: "Add `gtb keys mint` (mint an armored OpenPGP public key from an existing signer) and `gtb keys generate` (generate a fresh keypair locally + mint it), backed by a pluggable backend registry. Consumers can ship AWS KMS, GCP KMS, HashiCorp Vault, YubiKey, or any other backend without modifying the framework. Both commands keep operations inside the `gtb` binary — no shell-out to `gpg` or external tools required. Surfaces `openpgpkey` from `internal/` to `pkg/` and adds a public `pkg/signing` backend registry. Commands live in `internal/cmd/keys/` so they ship with the `gtb` binary only — scaffolded downstream tools do not inherit them."
status: APPROVED
date: 2026-06-08
tags:
  - specification
  - signing
  - openpgp
  - kms
  - keys
  - cli
  - backend-registry
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# `gtb keys mint` — pluggable OpenPGP-from-HSM minter

Authors
:   Matt Cockayne

Date
:   2026-06-08

Status
:   APPROVED

## Summary

The Phase 2 self-update signature verification work
(`2026-04-02-remote-update-checksum-verification.md`) requires that the
release signing key live in an HSM/KMS and that an ASCII-armored
OpenPGP public key be (a) embedded in the binary's trust set and
(b) published via WKD. Producing that armored key from an opaque
HSM-held key requires a single round of imperative code — fetch the
public half, call Sign once, frame the result in OpenPGP packets.

This work is currently being prototyped as a one-off Go program in
`phpboyscout/infra/scripts/mint-signing-key/`. Promoting it to a
first-class `gtb` subcommand with a pluggable backend interface (a)
generalises the recipe so any downstream consumer can mint a key
against their preferred HSM, (b) gives the feature a documented,
tested, semver-versioned home, and (c) seeds a public-facing tutorial
demonstrating end-to-end HSM-rooted release signing.

The minter ships as a `gtb`-only command (`internal/cmd/keys/`),
**not** as a feature flag scaffolded into downstream tools. Tool
authors mint their signing key with `gtb`; their tool's own users
never see a `mytool keys mint` command, because minting is a
release-engineering concern of the tool author, not a runtime
concern of their tool's users.

Two backends ship in the standard `gtb` binary: **`aws-kms`** (the
production HSM-rooted path) and **`local`** (a PEM-encoded RSA private
key on disk — for testing, blog tutorials, and self-hosted setups
that don't have a cloud KMS). Additional backends (GCP Cloud KMS,
Azure Key Vault, Vault Transit, YubiKey) can be implemented by anyone
consuming `pkg/signing` and blank-imported into a tool's `main`
package.

Alongside `mint`, a companion **`gtb keys generate`** command produces
fresh keypairs entirely inside the `gtb` process — no `gpg` install
required, no shell-out. Used during onboarding to generate the
rotation-authority key (Ed25519, sign-only, written to disk so the
operator can move the private half to offline storage) and, for the
tutorial path, the local RSA signing key that pairs with the `local`
backend. The same `pkg/openpgpkey` library that wraps an HSM-held key
also wraps these locally-generated keys, so every armored OpenPGP
file the framework produces has a single, audited code path.

This design keeps every step of the trust-chain bootstrap inside the
`gtb` binary. A consumer who has just installed `gtb` can run three
commands and have a complete signing chain ready to embed:

```sh
gtb keys generate --algorithm ed25519 --name "Tool Rotation Authority" \
    --email rotation@example.com --output rotation.asc
gtb keys generate --algorithm rsa --rsa-bits 4096 --name "Tool Release" \
    --email release@example.com --output signing.pem
gtb keys mint --backend local --key-id signing.pem \
    --name "Tool Release" --email release@example.com --output release.asc
```

Production tools substitute the second step for a Terraform-applied
KMS key + `gtb keys mint --backend aws-kms`. Same first and third
commands.

## Background

`go-tool-base`'s remote-update verification stack
(`pkg/setup/signing.go` and friends, currently on the un-merged
[MR !9 / `feat/phase2-signing`][mr9]) requires a trust set of OpenPGP
public keys. The intended production source for the primary signing
key is **AWS KMS** with an RSA-4096 `SIGN_VERIFY` key
(see `docs/development/phase2-signing-prep.md`). AWS KMS exposes only
`GetPublicKey` and `Sign`; the OpenPGP packet framing needed by the
verifier (and by every other OpenPGP consumer, including GnuPG and
WKD) is **not** something KMS produces directly.

The existing `internal/openpgpkey/openpgpkey.go` on MR !9 implements
the OpenPGP framing as a `func ArmoredPublicKey(signer crypto.Signer,
name, email string, creationTime time.Time) ([]byte, error)` —
agnostic of *where* the signing key lives. It is gated by Go's
`internal/` rule so only code under `go-tool-base/` itself can import
it.

For the work to be reusable by downstream consumers (and to support
the framework-shaped `gtb keys mint` command this spec introduces),
the package needs to move to `pkg/openpgpkey/`. A new `pkg/signing/`
package on top of it provides a registry of backends — each backend
knowing how to talk to its specific HSM/KMS surface and producing a
`crypto.Signer` that `openpgpkey.ArmoredPublicKey` can use.

[mr9]: https://gitlab.com/phpboyscout/go-tool-base/-/merge_requests/9

## Goals

- Provide a framework-supported way for tool authors to mint an
  OpenPGP public key from a remotely-held HSM signing key, with no
  exposure of the private half.
- Provide a framework-supported way to **generate** a fresh keypair
  locally (Ed25519 or RSA), with no external dependency on `gpg`,
  `openssl`, or any other tool. Targets two onboarding flows:
  - The rotation-authority key (Ed25519, sign-only).
  - A local RSA signing key for the tutorial / no-cloud-KMS path.
- Ship AWS KMS as a first-class backend (the prep-doc-recommended
  production path).
- Ship a `local` backend (PEM-encoded RSA private key on disk) so
  the feature is **demonstrable without cloud access** — important
  for the blog tutorial and for tools that don't have HSM access.
- Keep every operation **inside the `gtb` binary** — no shell-out to
  `gpg`, no off-tool key extraction. The framework distributes a
  tool that handles all of the cryptographic operations natively.
- Allow third-party backends (GCP KMS, Azure Key Vault, Vault
  Transit, YubiKey, custom) to be added by anyone consuming
  `pkg/signing`, registered via a blank-import in the tool's `main`,
  with no upstream change to `gtb` itself.
- Keep the commands discoverable: `gtb keys mint --help` and
  `gtb keys generate --help` work out-of-the-box on every standard
  `gtb` install.
- Surface the OpenPGP packet-assembly primitives as a public,
  reusable `pkg/openpgpkey` API supporting both RSA and Ed25519.

## Non-Goals

- **Generating HSM-held keys.** KMS keys are provisioned by Terraform
  (`terraform-aws-signing-kms` for AWS). `gtb keys generate` is
  explicitly for *local* keys — its private half is written to disk
  and the operator decides where it lives next.
- **Signature verification**. That's Phase 2 of the
  remote-update-checksum-verification spec, already on MR !9.
- **WKD publishing**. The minter emits a `.asc` file; pushing it to
  Cloudflare Pages or any other static host is operator-driven and
  out of scope.
- **Key rotation orchestration**. Rotate by minting a v2 key against
  a new HSM key resource; that's the same command run with different
  inputs.
- **Importing existing armored keys**. Out of scope — this is a
  *mint* command, not an *import* command.
- **Surfacing the command on scaffolded downstream tools**.
  Deliberately confined to `gtb` (Decision D3 below).

## Design Decisions

### D1 — Command shape: `gtb keys {mint,generate}`

The command lives under a `gtb keys` group rather than a flat
`gtb mint-signing-key` so the namespace can grow (future:
`gtb keys verify`, `gtb keys fingerprint`, `gtb keys import-wkd`).

Initial subcommand set: **`mint`** and **`generate`** (see D11).
`mint` wraps an existing signer in OpenPGP framing; `generate`
creates a fresh keypair locally and emits both halves. Other
subcommands are out-of-scope for this spec and added separately if
needed.

### D2 — Backend selection: explicit `--backend` always required

Every `gtb keys mint` invocation declares the backend explicitly. No
implicit default. Rationale:

- Invocations are self-documenting (a recipe in CI, a tutorial
  command, a blog snippet — all unambiguous).
- Adding a new backend to a future `gtb` release never silently
  changes what existing scripts do.
- The 12-character cost of `--backend aws-kms` is cheap.

The flag's accepted values are exactly the names of the backends
registered at process start (via blank-imports in `main`). Missing or
unknown backends fail with a clear error listing what *is* available.

### D3 — `internal/cmd/keys/` placement, not `pkg/cmd/keys/`

The minter is a `gtb`-only command — it is **not** surfaced through
the framework's command-feature system to downstream tools.
Rationale:

- A scaffolded `mytool` whose users build (say) a CLI for managing
  customer databases has no reason to expose `mytool keys mint`. It
  would be confusing noise in `--help`.
- The minting operation belongs to the *release engineering* concern
  of the tool author, not the *runtime* concern of the tool's users.
  Tool authors already have `gtb` installed.
- Mirrors the existing pattern: `internal/cmd/generate/`,
  `internal/cmd/regenerate/`, `internal/cmd/remove/` are all
  `gtb`-only scaffolding commands, not features inherited by
  scaffolded tools.

Tool authors who want to expose mint-like operations from their own
binary can do so by importing `pkg/signing` and `pkg/openpgpkey`
directly — the building blocks are public.

### D4 — Move `openpgpkey` from `internal/` to `pkg/`

`pkg/openpgpkey/` replaces `internal/openpgpkey/` (currently on
MR !9). The API surface is unchanged:

```go
func ArmoredPublicKey(signer crypto.Signer, name, email string, creationTime time.Time) ([]byte, error)
```

API stability tier: **Beta**. The function shape is small and unlikely
to change; the only churn risk is around what `crypto.SignerOpts`
shapes downstream KMS/HSM signers pass through (PKCS#1 v1.5 today,
possibly PSS for non-OpenPGP envelopes later). Promote to Stable in
v1.0 alongside the rest of the framework.

MR !9 rebases to import from the new path and to delete its
`internal/` copy. Single-commit rebase delete.

### D5 — `pkg/signing` registry shape

`pkg/signing` is a thin registry over `Backend` implementations. The
interface is intentionally minimal:

```go
// Backend constructs a crypto.Signer for an HSM-held key.
type Backend interface {
    // Name uniquely identifies this backend at the CLI. The user passes
    // it via --backend.
    Name() string

    // RegisterFlags declares any backend-specific CLI flags (region,
    // endpoint, keyring path, …). Called by `gtb keys mint` before
    // flag parsing.
    RegisterFlags(fs *pflag.FlagSet)

    // NewSigner returns a crypto.Signer wrapping the remote key. The
    // backend interprets keyID's format (AWS alias, GCP resource name,
    // Vault path, GPG uid, …). The returned signer.Public() must be a
    // *rsa.PublicKey — OpenPGP minting currently requires RSA.
    NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
}

// Register adds a backend to the global registry. Called from each
// backend package's init() so blank-importing the package activates
// the backend.
func Register(b Backend)

// Get returns the registered backend with name `name`, or
// ErrUnknownBackend listing what's available.
func Get(name string) (Backend, error)

// Names returns the registered backend names, sorted. Used by
// `gtb keys mint --help` to enumerate options.
func Names() []string
```

The registry uses a `sync.RWMutex`-guarded `map[string]Backend`,
written from `init()` and read from `gtb keys mint`'s `RunE`.

Open question (Q1 below): should the interface include
capability-discovery methods (e.g. `SupportsRSA() bool`,
`ListAvailableKeys()`)? Defer to v0.2 unless a concrete need surfaces.

### D6 — Ship two backends in the standard `gtb` binary

| Backend | Package | Registers as | Notes |
|---|---|---|---|
| AWS KMS | `pkg/signing/kms` | `aws-kms` | RSA `SIGN_VERIFY` keys. `--kms-region`, default `eu-west-2`. Uses AWS SDK v2 default credential chain. |
| Local PEM | `pkg/signing/local` | `local` | Reads a PEM-encoded RSA private key from disk. `--key-id` is the file path. Optional `--local-passphrase-env <var>` reads a passphrase from the named environment variable for PKCS#8-encrypted PEMs. |

The `local` backend exists to make the framework usable without an
HSM — for blog tutorials, integration tests, and self-hosted setups
that don't have a cloud KMS. It pairs naturally with
`gtb keys generate --algorithm rsa --rsa-bits 4096 --output signing.pem`
(see D11), giving a complete fresh-key-to-armored-OpenPGP flow with
no external dependencies.

A `gpg` backend was considered and rejected. Implementing
`crypto.Signer.Sign(digest, opts)` against gpg would require either
extracting the secret key from the keyring (defeating the "private
key never leaves the keyring" claim that was its only advantage) or
manually computing the OpenPGP binding-signature bytes outside of
go-crypto (fragile against go-crypto framing changes). Neither
trade-off pays off when `gtb keys generate` covers the same use case
natively. See Resolution 8.

Both built-in backends are blank-imported in `cmd/gtb/main.go`.
Downstream tools that want a different mix opt in by blank-importing
only the backends they need. A regulated downstream tool can avoid
linking the AWS SDK entirely by omitting `pkg/signing/kms`.

Provider plug-ins for **GCP KMS**, **Azure Key Vault**, **HashiCorp
Vault Transit**, and **YubiKey** are explicitly out of scope here but
the registry pattern accommodates them; community contributions are
welcome as separate packages.

### D7 — Output: armored `.asc` to a file, no stdout default

The minter writes the armored public key to a `--output` path,
defaulting to `release.asc` in the current directory. Reasoning:

- The output is a multi-line ASCII-armored block; piping it via stdout
  would interleave with `gtb`'s own log lines unless we route all
  logging to stderr. We do route logging to stderr (per the framework
  convention) — but a default-to-stdout invocation invites accidental
  redirection mistakes that produce mixed-output files.
- Defaulting to a file makes the "save it somewhere durable" intent
  explicit.

Q3 below covers whether to support `--output -` (write to stdout) as
an opt-in.

### D8 — Reproducibility: `--created` flag pins creation time

OpenPGP keys carry a `creationTime` value that is folded into the
key's fingerprint. To re-mint the *same key* (same KMS material, same
UID, same fingerprint) — e.g. to rebuild the armored file after
losing it — the `--created` flag accepts an RFC 3339 timestamp.
Default: `time.Now().UTC()`.

This is a footgun (passing the wrong timestamp changes the
fingerprint), but the alternative — making the creation time
non-reproducible — breaks the "two armored copies, identical
fingerprint, embedded + WKD-served" cross-check that the Phase 2
verifier relies on.

### D9 — Algorithm support: RSA + Ed25519

`pkg/openpgpkey` supports two algorithms in v0.1:

- **RSA** — used by `mint` (HSM-held signing keys; AWS KMS only
  exposes RSA for asymmetric `SIGN_VERIFY`) and by `generate` when
  the operator picks `--algorithm rsa --rsa-bits 4096` (the
  tutorial / local-signing path).
- **Ed25519** — used by `generate` when the operator picks
  `--algorithm ed25519` (the rotation-authority key path, and any
  future signing-key path that uses a backend supporting Ed25519).

ECDSA support is *not* added in v0.1 — no concrete consumer has
asked for it. The interface admits it additively when a need arises.

The minting code branches on the `signer.Public()` type to choose
between `packet.NewRSAPublicKey` and `packet.NewEdDSAPublicKey`; the
rest of the OpenPGP packet assembly path is identical.

### D10 — Spec-first; tests-first within implementation

This spec is approved before any Go code lands. The implementation
MR carries:

- Unit tests for `pkg/openpgpkey` (RSA + Ed25519 paths; opaque
  signer simulating KMS; non-supported-key-type error; deterministic
  fingerprint round-trip; failing-writer error propagation).
- Unit tests for `pkg/signing` registry (Register/Get/Names,
  concurrency, ErrUnknownBackend, empty-registry hint).
- Unit tests for `pkg/signing/kms` (mocked AWS SDK; happy path +
  KMS-returns-non-RSA error path + per-hash algorithm mapping).
- Unit tests for `pkg/signing/local` (PEM parsing; PKCS#8-encrypted
  PEM with `--local-passphrase-env`; non-RSA-in-PEM rejection).
- Unit tests for `internal/cmd/keys/{mint,generate}` (flag wiring,
  --backend unknown, --algorithm unknown, --output path errors,
  fingerprint logged at INFO).
- BDD smoke test (Godog) covering the three-command tutorial chain:
  generate Ed25519 rotation → generate RSA local → mint local.

### D11 — `gtb keys generate` command (companion to `mint`)

`generate` produces a fresh keypair entirely inside the `gtb`
process and writes both halves to disk. It exists to remove the
external-tool dependency (`gpg`, `openssl`, etc.) from every
consumer's onboarding flow, and to make the framework genuinely
self-sufficient for trust-chain bootstrap.

Flags:

| Flag | Required | Default | Purpose |
|---|---|---|---|
| `--algorithm` | yes | — | `ed25519` or `rsa` |
| `--rsa-bits` | RSA only | `4096` | `2048` / `3072` / `4096` |
| `--name` | yes | — | OpenPGP UID real-name |
| `--email` | yes | — | OpenPGP UID email |
| `--output` | no | `<role>.asc` | Path for the armored OpenPGP public half |
| `--private-output` | no | derived from `--output` | Path for the private half (armored OpenPGP for Ed25519; PEM for RSA) |
| `--passphrase-env` | no | none | Reads a passphrase from the named env var to encrypt the private half (PKCS#8 for RSA PEM, OpenPGP s2k for Ed25519 armored) |
| `--created` | no | now | RFC3339 timestamp baked into the OpenPGP entity |

Two canonical invocations:

```sh
# Rotation-authority key (operator moves the private half to offline
# storage after running):
gtb keys generate \
    --algorithm ed25519 \
    --name "Tool Rotation Authority" \
    --email rotation@example.com \
    --output rotation-authority.asc \
    --passphrase-env GTB_ROTATION_PASS

# Local signing key (tutorial path):
gtb keys generate \
    --algorithm rsa --rsa-bits 4096 \
    --name "Tool Release" \
    --email release@example.com \
    --output signing.asc \
    --private-output signing.pem
```

The Ed25519 private half is written as an armored OpenPGP secret-key
packet (compatible with `gpg --import` for inspection / rotation). The
RSA private half is written as PKCS#8 PEM (compatible with
`openssl rsa -in signing.pem ...` and consumable by the `local`
backend).

A successful run logs the fingerprint at INFO (per Resolution 7),
along with a reminder to move the private-half file to offline
storage.

### D12 — Ed25519 support in `pkg/openpgpkey`

`pkg/openpgpkey` accepts `crypto.Signer` instances whose `Public()`
returns either `*rsa.PublicKey` or `ed25519.PublicKey`. Both flow
through the same `entity()` assembly path; only the public-key packet
constructor differs (`packet.NewRSAPublicKey` vs.
`packet.NewEdDSAPublicKey`).

This is purely additive — the existing RSA-only API surface stays
backward-compatible. Other key types still return the existing
unsupported-type error.

## Public API Changes

### New: `pkg/openpgpkey`

```go
// ArmoredPublicKey constructs a self-signed OpenPGP entity around the
// signer's public half and returns its ASCII-armored encoding.
//
// signer must satisfy crypto.Signer with a *rsa.PublicKey from
// Public(); other key types return an error.
//
// creationTime is folded into the resulting key's fingerprint —
// passing inconsistent values across re-mints produces different
// fingerprints for the same KMS material. For new keys use
// time.Now().UTC(); to re-derive an existing key pass its original
// creation time.
func ArmoredPublicKey(signer crypto.Signer, name, email string, creationTime time.Time) ([]byte, error)
```

API stability: Beta (per D4).

### New: `pkg/signing`

```go
type Backend interface {
    Name() string
    RegisterFlags(fs *pflag.FlagSet)
    NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
}

func Register(b Backend)
func Get(name string) (Backend, error)
func Names() []string

var ErrUnknownBackend = errors.New("unknown signing backend")
```

API stability: Beta.

### New: `pkg/signing/kms`

A `Backend` implementation that wraps AWS KMS. Registers as
`aws-kms`. Public surface:

```go
// (blank-import only — no public types)
import _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/kms"
```

If a tool needs to construct the KMS signer programmatically without
going through the `--backend` flag (rare), an `NewSigner` constructor
is exposed:

```go
// NewSigner returns a crypto.Signer backed by the given KMS key. Used
// directly by callers that don't want the global registry; the
// `gtb keys mint` CLI uses the registry path.
func NewSigner(ctx context.Context, client *kms.Client, keyID string) (crypto.Signer, error)
```

### New: `pkg/signing/local`

A `Backend` implementation that reads a PEM-encoded RSA private key
from disk. Registers as `local`. Public surface: blank-import to
register, plus a `NewSigner(path string) (crypto.Signer, error)`
constructor for callers that want to bypass the registry.

The backend supports unencrypted PKCS#1 / PKCS#8 PEMs, plus PKCS#8
PEMs encrypted with a passphrase read from an environment variable
named by `--local-passphrase-env`. Non-RSA keys in the PEM file are
rejected with a clear error.

### New (internal): `internal/cmd/keys/`

The `gtb keys` cobra command group, with two subcommands:

  - `mint` — wraps an existing signer (KMS or local PEM) in OpenPGP
    framing and writes the armored public half. Driven by D1.
  - `generate` — generates a fresh keypair in-process and writes both
    halves to disk. Driven by D11.

Not part of the framework API; tool authors cannot enable these on
their own binary.

### Modified: `cmd/gtb/main.go`

Adds blank-imports of `pkg/signing/kms` and `pkg/signing/local`.

### Removed: `internal/openpgpkey/` (on MR !9, after this lands)

Replaced by `pkg/openpgpkey/`. MR !9 rebases.

## Internal Implementation

### `pkg/openpgpkey`

Verbatim port of `internal/openpgpkey/openpgpkey.go` from MR !9, with
no API changes. Existing tests carry forward.

### `pkg/signing/registry.go`

```go
var (
    mu       sync.RWMutex
    backends = map[string]Backend{}
)
```

Standard registry pattern. `Register` panics on duplicate name (cheap
fail-fast for an `init()`-time error). `Get` is read-locked and
returns `ErrUnknownBackend` with the available names listed.

### `pkg/signing/kms/kms.go`

Wraps the existing `kmsSigner` from
`phpboyscout/infra/scripts/mint-signing-key/kmssigner.go` plus a
`Backend` implementation:

```go
type backend struct {
    region string
    client *kms.Client  // lazy-initialised on first NewSigner
}

func (b *backend) Name() string { return "aws-kms" }

func (b *backend) RegisterFlags(fs *pflag.FlagSet) {
    fs.StringVar(&b.region, "kms-region", "eu-west-2", "AWS region the KMS key lives in")
}

func (b *backend) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
    // load AWS config with region, lazy-init kms.Client, return kmsSigner.
}

func init() { signing.Register(&backend{}) }
```

### `pkg/signing/local/local.go`

Reads a PEM-encoded RSA private key from the path supplied as
`--key-id`. Supports:

- Unencrypted PKCS#1 PEM (`-----BEGIN RSA PRIVATE KEY-----`).
- Unencrypted PKCS#8 PEM (`-----BEGIN PRIVATE KEY-----`).
- PKCS#8-encrypted PEM (`-----BEGIN ENCRYPTED PRIVATE KEY-----`),
  with the passphrase read from `--local-passphrase-env <name>`'s
  named environment variable. Refused if the env var is unset.

The resulting `crypto.Signer` is a `*rsa.PrivateKey`, so
`Public().(*rsa.PublicKey)` flows through to `pkg/openpgpkey`
without any wrapping.

Non-RSA keys in the PEM file (Ed25519, ECDSA, etc.) return
`ErrUnsupportedKeyType`; this preserves the v0.1 design that the
`local` backend is for the tutorial / no-KMS path which uses RSA.

### `internal/cmd/keys/generate.go`

```go
func NewCmdKeysGenerate(p *props.Props) *setup.Command {
    // Flags: --algorithm, --rsa-bits, --name, --email, --output,
    //        --private-output, --passphrase-env, --created.
    //
    // RunE:
    //   1. Validate flags.
    //   2. Generate keypair:
    //      - "ed25519": ed25519.GenerateKey(rand.Reader)
    //      - "rsa":     rsa.GenerateKey(rand.Reader, var.rsaBits)
    //   3. signer = key   (both *rsa.PrivateKey and ed25519.PrivateKey
    //                      implement crypto.Signer)
    //   4. armored, err := openpgpkey.ArmoredPublicKey(signer, name,
    //                                                  email, created)
    //   5. Write armored to --output.
    //   6. Write private half to --private-output:
    //      - Ed25519 → armored OpenPGP secret-key packet (optionally
    //        s2k-encrypted with --passphrase-env)
    //      - RSA     → PKCS#8 PEM (optionally PKCS#8-encrypted with
    //        --passphrase-env)
    //   7. Log fingerprint at INFO; remind operator to move private
    //      half to offline storage.
}
```

Wiring matches `internal/cmd/keys/mint.go` — registered into
`internal/cmd/root/root.go` alongside `generate`, `regenerate`,
`remove`, and the new `keys` command group.

### `internal/cmd/keys/mint.go`

```go
func NewCmdKeysMint(p *props.Props) *setup.Command {
    var (
        backendName string
        keyID       string
        name        string
        email       string
        output      string
        createdRaw  string
    )

    cmd := &cobra.Command{
        Use:   "mint",
        Short: "Mint an ASCII-armored OpenPGP public key from an HSM/KMS-held signing key",
        Long:  `…`,  // includes the available-backends listing from signing.Names()
        RunE: func(cmd *cobra.Command, _ []string) error {
            b, err := signing.Get(backendName)
            if err != nil { return err }
            // ... resolve creation time, call b.NewSigner, call openpgpkey.ArmoredPublicKey, write output
        },
    }

    cmd.Flags().StringVar(&backendName, "backend", "", "Signing backend name (required). Available: "+strings.Join(signing.Names(), ", "))
    cmd.Flags().StringVar(&keyID, "key-id", "", "Backend-specific key identifier (required)")
    cmd.Flags().StringVar(&name, "name", "", "OpenPGP user-id real name (required)")
    cmd.Flags().StringVar(&email, "email", "", "OpenPGP user-id email (required)")
    cmd.Flags().StringVar(&output, "output", "release.asc", "Output file path")
    cmd.Flags().StringVar(&createdRaw, "created", "", "RFC3339 creation time; default is now")
    _ = cmd.MarkFlagRequired("backend")
    _ = cmd.MarkFlagRequired("key-id")
    _ = cmd.MarkFlagRequired("name")
    _ = cmd.MarkFlagRequired("email")

    // Backends register their own flags.
    for _, name := range signing.Names() {
        b, _ := signing.Get(name)
        b.RegisterFlags(cmd.Flags())
    }

    return setup.Wrap("", cmd)
}
```

Wired into `internal/cmd/root/root.go` alongside `generate`,
`regenerate`, `remove`.

## Testing

### Unit

| Package | Coverage target | Notes |
|---|---|---|
| `pkg/openpgpkey` | ≥90% | Stub signer, RSA-2048 fixture, parse output with go-crypto reader to confirm round-trip. |
| `pkg/signing` | ≥95% | Registry concurrency (Register from multiple goroutines should be deterministic via init ordering). |
| `pkg/signing/kms` | ≥80% | Mocked KMS client; happy path + non-RSA error path + per-hash SigningAlgorithmSpec mapping. |
| `pkg/signing/local` | ≥90% | PKCS#1 / PKCS#8 / encrypted PEM parsing; missing-env-var rejection; non-RSA-in-PEM rejection. No external dependency on `openssl`. |
| `internal/cmd/keys` | ≥85% | Flag parsing, error path for `--backend` unknown, `--algorithm` unknown, `--output` collision, happy path for `mint` with a fake backend, happy path for `generate` end-to-end. |

### Integration (`INT_TEST=1`)

End-to-end smoke that exercises the three-command tutorial chain:

```sh
gtb keys generate --algorithm ed25519 --name "TR" --email tr@example.com --output /tmp/rotation.asc
gtb keys generate --algorithm rsa --rsa-bits 4096 --name "TS" --email ts@example.com --output /tmp/signing.asc --private-output /tmp/signing.pem
gtb keys mint --backend local --key-id /tmp/signing.pem --name "TS" --email ts@example.com --output /tmp/release.asc
```

Each command exits 0; each output is a valid OpenPGP entity; the
`mint` output and the `generate` output for the RSA key have identical
fingerprints (proves the `local` backend round-trips faithfully).

### BDD (Godog)

`features/keys.feature` covers the two commands:

```gherkin
Scenario: Generate an Ed25519 rotation-authority key
  When I run "gtb keys generate --algorithm ed25519 --name 'TR' --email tr@example.com --output /tmp/rotation.asc"
  Then the command exits with status 0
  And /tmp/rotation.asc parses as a valid OpenPGP public key
  And the key algorithm is Ed25519
  And the fingerprint is logged at INFO

Scenario: Mint via the local backend chains with generate
  Given an RSA private key produced by "gtb keys generate --algorithm rsa --rsa-bits 4096 ..."
  When I run "gtb keys mint --backend local --key-id /tmp/signing.pem ..."
  Then the resulting --output fingerprint matches the fingerprint logged by the previous generate command
```

## Documentation

- `docs/components/openpgpkey.md` — package-level reference for
  `pkg/openpgpkey` (RSA + Ed25519).
- `docs/components/signing.md` — `pkg/signing` registry, plus the two
  ship-in-the-box backends (`aws-kms`, `local`).
- `docs/concepts/release-binary-signing.md` — narrative concept doc
  introducing the HSM-rooted signing chain for a tool author who has
  never set one up before. Walks through the `gtb keys generate` →
  `gtb keys mint` flow, contrasts local vs. KMS for the signing key,
  introduces the rotation-authority concept.
- `docs/how-to/generate-rotation-key.md` — recipe for
  `gtb keys generate --algorithm ed25519`, the offline-storage
  pattern for the private half, and where to embed the public half.
- `docs/how-to/mint-signing-key.md` — task-oriented recipes for each
  ship-in-the-box backend (`aws-kms`, `local`). Designed to be
  excerpted into a blog tutorial.
- `docs/how-to/add-signing-backend.md` — guide for downstream
  consumers who want to add a new backend (GCP, Azure, Vault, etc.).
  Includes the `init()`+`signing.Register()` pattern, the required
  interface methods, and a worked example.

## Rollout

1. **This MR** lands the `pkg/openpgpkey` move (with Ed25519 support),
   `pkg/signing` registry + the `aws-kms` and `local` backends,
   `internal/cmd/keys/{mint,generate}`, tests, and docs. Releaser-
   pleaser picks up the `feat:` commits and proposes a minor bump
   (v0.x → v0.y).

2. **After v0.y publishes**:
   - `gtb keys mint --backend aws-kms --key-id alias/gtb-release-signing-v1 ...`
     produces the production `release.asc` for the Phase 2 rollout.
     The trust-policy widening recipe (currently in the prep doc)
     applies.
   - The current operator's existing offline-generated rotation-
     authority key (Ed25519, fingerprint
     `42FB 010F C2EB 81C4 4F5A 9FC5 3D73 36AF 4E27 CF6D`) is reused
     as-is — its public half (`/tmp/rotation-authority.asc`) is
     already in hand. Future operators bootstrapping a new tool use
     `gtb keys generate --algorithm ed25519 ...` instead of the
     offline-gpg dance.

3. **MR !9 rebases** to:
   - Drop `internal/openpgpkey/`.
   - Update its imports to `pkg/openpgpkey/`.
   - Embed `release.asc` + `rotation-authority.asc` under
     `internal/trustkeys/keys/`.

4. **Phase 2 rollout continues** per the prior plan (WKD endpoint →
   N+1 release → CI wiring → N+2 signed release → N+3 flip).

## Resolutions

The originally-listed open questions were resolved during spec review
(2026-06-08). Resolution 8 was added during the second-round review
that introduced D11 (`gtb keys generate`) and D12 (Ed25519 support);
the original Q2 about `--gnupg-home` was rendered moot by dropping
the `gpg` backend entirely.

1. **No capability-discovery on `Backend` in v0.1.** Backends either
   produce a Signer (RSA or Ed25519) or fail; `Capabilities()` /
   `SupportedAlgorithms()` add ceremony without consumers. Revisit
   when a third algorithm (e.g. ECDSA) lands.
2. **Superseded.** The original question asked whether the `gpg`
   backend should accept a `--gnupg-home` flag. With the `gpg`
   backend removed (Resolution 8), this is moot. The `local` backend
   takes the file path directly via `--key-id`, so there is no
   equivalent flag needed.
3. **No `--output -` stdout support in v0.1.** Default to a file path;
   passing `-` errors. Avoids the log-interleaving footgun. Revisit
   only if a real user asks.
4. **Compile-time backend gating is a tested design property.** A CI
   smoke test compiles a minimal `gtb`-shaped binary that blank-imports
   only the `local` backend (not `kms`) and asserts that
   `gtb keys mint --backend aws-kms` errors with `ErrUnknownBackend`.
   No build-tag plumbing in the code — the smoke is implemented as a
   separate `cmd/gtb-no-aws/main.go` under a `_smoke` test directory
   that the CI script `go build`s. Proves the blank-import-to-activate
   pattern actually works.
5. **`pkg/openpgpkey` and `pkg/signing` are released as Beta tier** per
   `docs/about/api-stability.md`. Function signatures are stable; the
   only known evolution path is additive (ECDSA support).
6. **No `--aws-profile` flag on the `aws-kms` backend in v0.1.** AWS
   SDK default credential chain is sufficient. Users override via
   `AWS_PROFILE` or by assuming a role explicitly before invoking the
   minter. Add a flag if a real consumer asks.
7. **Successful mint prints the resulting fingerprint at INFO level**
   to stderr (e.g. `INFO  Minted OpenPGP key  fingerprint=42FB ... `).
   Removes the need for a follow-up `gpg --show-key`; one less
   transcription step in operator runbooks. The same INFO line is
   logged by `gtb keys generate` after successful keypair creation.
8. **`gpg` backend dropped; replaced by `local` (PEM file) + native
   `gtb keys generate`.** The `gpg` backend would have required either
   extracting the secret key from the keyring (defeating the security
   property that was its only advantage) or manually computing OpenPGP
   binding-signature bytes outside go-crypto (fragile against
   library changes). Neither pays off when `gtb keys generate` covers
   the same use case natively. The `local` backend (PEM-encoded RSA
   on disk) handles the no-cloud-KMS path with a simple, dependency-
   free implementation. Net result: the framework distributes a tool
   that handles all cryptographic operations internally — no external
   shell-outs, no `gpg` install required, no offline-workstation gpg
   dance for new consumers.

## Related

- [Phase 2 prep doc][prep] — operational steps for AWS KMS provisioning and WKD endpoint.
- [MR !9 / Phase 2 signature verification][mr9] — the consumer of the minted `release.asc`.
- [`terraform-aws-signing-kms`][mod] — the Terraform module that provisions the KMS key + signer role this command consumes.

[prep]: ../phase2-signing-prep.md
[mr9]: https://gitlab.com/phpboyscout/go-tool-base/-/merge_requests/9
[mod]: https://gitlab.com/phpboyscout/terraform-aws-signing-kms
