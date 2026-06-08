---
title: "`gtb keys mint` — pluggable OpenPGP-from-HSM minter"
description: "Add a `gtb keys mint` subcommand that produces an ASCII-armored OpenPGP public key from a remotely-held signing key, with a pluggable backend interface so consumers can ship AWS KMS, GCP KMS, HashiCorp Vault, YubiKey, or local GPG without modifying the framework. Surfaces openpgpkey from internal/ to pkg/ and adds a public `pkg/signing` backend registry. The command lives in `internal/cmd/keys/` so it ships with the `gtb` binary only — scaffolded downstream tools do not inherit it."
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

The two backends shipped in the standard `gtb` binary are
**`aws-kms`** (the primary production backend) and **`gpg`** (local
GPG on disk — useful for testing, blog tutorials, and offline
generation paths). Additional backends (GCP Cloud KMS, Azure Key
Vault, Vault Transit, YubiKey) can be implemented by anyone consuming
`pkg/signing` and blank-imported into a tool's `main` package.

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
- Ship AWS KMS as a first-class backend (the prep-doc-recommended
  production path).
- Ship a local GPG backend so the feature is **demonstrable without
  cloud access** — important for the blog tutorial and for tools that
  pick a local-GPG signing path.
- Allow third-party backends (GCP KMS, Azure Key Vault, Vault
  Transit, YubiKey, custom) to be added by anyone consuming
  `pkg/signing`, registered via a blank-import in the tool's `main`,
  with no upstream change to `gtb` itself.
- Keep the minter discoverable: `gtb keys mint --help` works
  out-of-the-box on every standard `gtb` install.
- Surface the OpenPGP packet-assembly primitives as a public,
  reusable `pkg/openpgpkey` API.

## Non-Goals

- **Key generation**. KMS keys are provisioned by Terraform
  (`terraform-aws-signing-kms` for AWS); local keys by `gpg
  --quick-generate-key`. This spec covers transforming an existing
  key's public half into OpenPGP form.
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

### D1 — Command shape: `gtb keys mint`

The command lives under a `gtb keys` group rather than a flat
`gtb mint-signing-key` so the namespace can grow (future:
`gtb keys verify`, `gtb keys fingerprint`, `gtb keys import-wkd`).

Initial subcommand set: `mint` only. Other subcommands are
out-of-scope for this spec and added separately if needed.

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
| AWS KMS | `pkg/signing/kms` | `aws-kms` | RSA `SIGN_VERIFY` keys. `--region`, default `eu-west-2`. Uses AWS SDK v2 default credential chain. |
| Local GPG | `pkg/signing/gpg` | `gpg` | Reads from the user's `~/.gnupg/` (or `--gnupg-home` for isolated keyrings, see Q2). RSA keys only. |

Both are blank-imported in `cmd/gtb/main.go`. Downstream tools that
want a different mix opt in by blank-importing only the backends they
need. A regulated downstream tool can avoid linking the AWS SDK
entirely by omitting `pkg/signing/kms`.

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

### D9 — Algorithm support: RSA only at v0.1

OpenPGP minting requires the signer's public half to be a known key
type. v0.1 supports RSA only because:

- AWS KMS asymmetric `SIGN_VERIFY` keys are RSA (Ed25519 not exposed).
- `go-crypto/openpgp` v4 entity assembly for ECDSA is more complex and
  not needed for v0.1's customer set.

A v0.2 with ECDSA support is straightforward to add; this spec
explicitly tracks RSA-only as a constraint, not a permanent design
choice.

### D10 — Spec-first; tests-first within implementation

This spec is approved before any HCL or Go code lands. The
implementation MR carries:

- Unit tests for `pkg/openpgpkey` (existing tests on MR !9 are
  carried forward; one path tests RSA-2048 minting against a stub
  signer; one path covers the unsupported-key-type error).
- Unit tests for `pkg/signing` registry (Register/Get/Names,
  concurrency, ErrUnknownBackend).
- Unit tests for `pkg/signing/kms` (mocked AWS SDK; happy path +
  KMS-returns-non-RSA error path).
- Unit tests for `pkg/signing/gpg` (table-driven; some paths gated
  by `gpg` being on PATH).
- BDD smoke test (Godog) for `gtb keys mint --backend gpg ...` (uses
  a test keyring fixture; verifies the resulting armored output is
  parseable by `gpg --show-key`).

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

### New: `pkg/signing/gpg`

A `Backend` implementation that wraps a local GnuPG installation.
Registers as `gpg`. Reads from `$GNUPGHOME` (or `--gnupg-home` flag),
shells out to `gpg` for the signing operation. Public surface:
blank-import to register.

### New (internal): `internal/cmd/keys/`

The `gtb keys` cobra command. Not part of the framework API; tool
authors cannot enable it on their own binary.

### Modified: `cmd/gtb/main.go`

Adds blank-imports of `pkg/signing/kms` and `pkg/signing/gpg`.

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

### `pkg/signing/gpg/gpg.go`

Shells out to `gpg --export-secret-keys` to fetch the public half +
agent-mediated `gpg --output ... --sign --detach-sign` for the
signing operation. The `Backend` wraps this in a `crypto.Signer` whose
`Sign()` shells out per call. Acceptable performance because the
mint operation invokes `Sign` exactly once.

A `--gnupg-home` flag lets the user point at an isolated keyring
(useful for the offline-workstation pattern documented in
`docs/development/phase2-signing-prep.md`).

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
| `pkg/signing/kms` | ≥80% | Mocked KMS client; happy path + non-RSA error path. |
| `pkg/signing/gpg` | ≥75% | Some paths gated by `gpg` on PATH; covered by an integration test fixture if available. |
| `internal/cmd/keys` | ≥85% | Flag parsing, error path for `--backend` unknown, happy path with a fake backend. |

### Integration (`INT_TEST=1`)

- `gtb keys mint --backend gpg --gnupg-home <fixture> --key-id <fixture-uid> --name x --email x@example.com --output /tmp/out.asc` → exits 0, output parses as a valid OpenPGP entity.

### BDD (Godog)

One scenario in `features/keys-mint.feature`:

```gherkin
Scenario: Mint an OpenPGP key from a local GPG backend
  Given a local GnuPG keyring containing an RSA signing key
  When I run "gtb keys mint --backend gpg ..."
  Then the command exits with status 0
  And the output file is a valid ASCII-armored OpenPGP public key
  And the fingerprint matches the key in the keyring
```

## Documentation

- `docs/components/openpgpkey.md` — package-level reference for the
  new `pkg/openpgpkey`.
- `docs/components/signing.md` — `pkg/signing` registry, plus the two
  ship-in-the-box backends.
- `docs/concepts/release-binary-signing.md` — narrative concept doc
  introducing the HSM-rooted signing chain for a tool author who has
  never set one up before. Companion to (but distinct from) the prep
  doc in MR !9.
- `docs/how-to/mint-signing-key.md` — task-oriented recipes for each
  ship-in-the-box backend (`aws-kms`, `gpg`). Designed to be
  excerpted into a blog tutorial.
- `docs/how-to/add-signing-backend.md` — guide for downstream
  consumers who want to add a new backend (GCP, Azure, Vault, etc.).
  Includes the `init()`+`signing.Register()` pattern, the required
  interface methods, and a worked example.

## Rollout

1. **This MR** lands the `pkg/openpgpkey` move, `pkg/signing` +
   backends, `internal/cmd/keys/mint`, tests, and docs. Releaser-
   pleaser picks up the `feat:` commits and proposes a minor bump
   (v0.x → v0.y).

2. **After v0.y publishes**: run
   `gtb keys mint --backend aws-kms --key-id alias/gtb-release-signing-v1 --name "GTB Release" --email release@phpboyscout.uk --output /tmp/release.asc`
   to produce the actual `release.asc` for the Phase 2 rollout. The
   trust-policy widening recipe (currently in the prep doc) applies.

3. **MR !9 rebases** to:
   - Drop `internal/openpgpkey/`.
   - Update its imports to `pkg/openpgpkey/`.
   - Embed `release.asc` + `rotation-authority.asc` under
     `internal/trustkeys/keys/`.

4. **Phase 2 rollout continues** per the prior plan (WKD endpoint →
   N+1 release → CI wiring → N+2 signed release → N+3 flip).

## Resolutions

The originally-listed open questions were resolved during spec review
(2026-06-08):

1. **No capability-discovery on `Backend` in v0.1.** Backends either
   produce an RSA signer or fail; `Capabilities()` / `SupportedAlgorithms()`
   add ceremony without consumers. Revisit when a second algorithm
   (e.g. ECDSA) lands.
2. **The `gpg` backend exposes a `--gnupg-home` flag** in addition to
   honouring `$GNUPGHOME`. Mirrors the offline-workstation pattern
   from `phase2-signing-prep.md` and lets a single shell session
   target multiple isolated keyrings.
3. **No `--output -` stdout support in v0.1.** Default to a file path;
   passing `-` errors. Avoids the log-interleaving footgun. Revisit
   only if a real user asks.
4. **Compile-time backend gating is a tested design property.** A CI
   smoke test compiles a minimal `gtb`-shaped binary that blank-imports
   only the `gpg` backend (not `kms`) and asserts that
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
   transcription step in operator runbooks.

## Related

- [Phase 2 prep doc][prep] — operational steps for AWS KMS provisioning and WKD endpoint.
- [MR !9 / Phase 2 signature verification][mr9] — the consumer of the minted `release.asc`.
- [`terraform-aws-signing-kms`][mod] — the Terraform module that provisions the KMS key + signer role this command consumes.

[prep]: ../phase2-signing-prep.md
[mr9]: https://gitlab.com/phpboyscout/go-tool-base/-/merge_requests/9
[mod]: https://gitlab.com/phpboyscout/terraform-aws-signing-kms
