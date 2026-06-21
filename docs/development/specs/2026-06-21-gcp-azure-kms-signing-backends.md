---
title: "GCP KMS & Azure Key Vault signing backends (item C1)"
description: "Add two cloud-HSM signing backends — `gcp-kms` (Cloud KMS) and `azure-kv` (Azure Key Vault) — alongside the existing `aws-kms` and `local` backends, each as a blank-importable subpackage that mirrors `pkg/signing/kms`. The work uses only the sanctioned `signing.Backend` registry extension point; it introduces no vendored secrets-provider registry. Dead-code-elimination is preserved so regulated downstreams that omit a cloud SDK link none of it."
date: 2026-06-21
status: DRAFT
tags:
  - specification
  - signing
  - openpgp
  - kms
  - gcp
  - azure
  - backend-registry
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# GCP KMS & Azure Key Vault signing backends (item C1)

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   21 June 2026

Status
:   DRAFT

---

## Summary

GTB ships two production-grade signing backends today —
[`pkg/signing/kms`](../../../pkg/signing/kms/kms.go) (`aws-kms`,
RSA SIGN_VERIFY keys held in AWS KMS) and
[`pkg/signing/local`](../../../pkg/signing/local/local.go) (`local`,
an on-disk PEM key for the tutorial / no-cloud path) — plus the
`signing.Backend` registry in [`pkg/signing`](../../../pkg/signing/backend.go)
that both `gtb keys mint` and `gtb sign` dispatch against by name.

This spec adds two more cloud-HSM backends as **sibling subpackages**
under `pkg/signing/`:

| Name | Package | SDK | Held key |
|------|---------|-----|----------|
| `gcp-kms`  | `pkg/signing/gcpkms`  | `cloud.google.com/go/kms/apiv1`                       | RSA `RSA_SIGN_PKCS1_*` CryptoKeyVersion |
| `azure-kv` | `pkg/signing/azurekv` | `github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys` | RSA key, `RS256`/`RS384`/`RS512` |

Each backend is **blank-import-activated** exactly like `aws-kms`:

```go
import _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/gcpkms"
import _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/azurekv"
```

Once imported, they appear in `signing.Names()` and are usable as
`--backend gcp-kms` / `--backend azure-kv` for both `gtb keys mint`
and `gtb sign` with **zero changes to those commands** — that is the
whole point of the registry.

No new public types are introduced on `pkg/signing`. The
`Backend` interface (`Name`, `RegisterFlags`, `NewSigner`) is
unchanged; this is purely additive, following the "the only known
evolution path is additive" note in the package doc.

## Motivation

1. **Multi-cloud release signing.** The `aws-kms` backend rooted GTB's
   own Phase 2 self-update signing chain (live since v0.12.2). Tool
   authors building on GTB whose release engineering lives on GCP or
   Azure currently have to write and maintain their own backend (the
   [add-a-signing-backend how-to](../../how-to/add-signing-backend.md)
   shows the recipe, and already uses a hypothetical `gcp-kms` as its
   worked example). Promoting the two most-requested cloud HSMs to
   first-class, tested, semver-versioned subpackages removes that
   per-consumer cost and gives the how-to two real references instead
   of one.
2. **Same recipe, three clouds.** AWS, GCP, and Azure all expose an
   opaque asymmetric RSA key with a "fetch public half" + "sign a
   digest remotely" pair. The minting recipe (`GetPublicKey` → one
   `Sign` → OpenPGP framing) is identical; only the SDK call shapes
   differ. Implementing both alongside `aws-kms` keeps the three in
   lockstep and exercises the registry's multi-backend wiring with
   more than one cloud SDK present.
3. **Validates the extensibility claim.** Shipping a second and third
   backend through the registry is the strongest possible evidence
   that the registry is the right extension point — and confirms the
   blank-import dead-code-elimination story holds with heavyweight
   cloud SDKs, not just the AWS one.

## Non-goals

- **No secrets-provider registry.** See "Relationship to the rejected
  secrets-provider registry" below. This spec touches only the
  *signing* `Backend` registry, which is already accepted.
- **No Ed25519 / ECDSA minting.** The OpenPGP minting path
  (`pkg/openpgpkey`) requires `signer.Public()` to be an
  `*rsa.PublicKey` (see
  [`pkg/openpgpkey/openpgpkey.go`](../../../pkg/openpgpkey/openpgpkey.go)
  line ~55 and `sign.go` line ~50). Both new backends are therefore
  **RSA-only**, mirroring `aws-kms`. ECDSA/Ed25519 support is a
  separate, cross-cutting change to `pkg/openpgpkey` and is explicitly
  out of scope (recorded as a follow-up in
  [Open questions](#open-questions)).
- **No credential-mode plumbing.** Like `aws-kms`, both backends defer
  entirely to their SDK's ambient credential chain (Application Default
  Credentials for GCP; `DefaultAzureCredential` for Azure). They do
  **not** read GTB's `pkg/credentials` config. Credential resolution is
  a caller/CI concern, consistent with the `Backend` contract note:
  "Backends do not own credential resolution."
- **No change to `gtb keys mint` / `gtb sign` source.** Both commands
  already enumerate `signing.Names()` and call `RegisterFlags` on every
  registered backend. Adding backends to the binary is a one-line
  blank-import in `cmd/gtb/signing.go`.

## Relationship to the rejected secrets-provider registry

The [feature-decisions log](../feature-decisions.md)
records a **rejected** proposal: a bundled `SecretsProvider` *plugin
registry with vendor implementations* (OS keychain, HashiCorp Vault,
AWS SSM, 1Password). The rejection rationale: secrets management is
deployment-specific, and shipping one vendor adapter "opens a rabbit
hole of vendor-specific adapters" that belong in the *tool*, not the
*framework*. The log's own summary: *"`Backend` is the extension
point; `SecretsProvider` as originally proposed (a plugin registry
with vendor implementations) is still rejected."*

**This work does not conflict with that rejection, for three reasons:**

1. **Different registry, different purpose.** The rejected item was
   about *reading arbitrary secrets at runtime* for config composition.
   This is about the `signing.Backend` registry — a *release-engineering*
   surface for producing OpenPGP signatures from an HSM-held key. The
   signing registry was **accepted and shipped** (the
   [`gtb keys mint` spec](2026-06-08-keys-mint-command.md) and
   the [`gtb sign` spec](2026-06-09-sign-command.md)), with GCP
   KMS / Azure / Vault / YubiKey called out from day one as the
   intended extension targets.
2. **No private-key material crosses the boundary.** A
   `SecretsProvider` hands raw secret bytes to the caller. A
   `signing.Backend` never exposes private-key material — it returns an
   opaque `crypto.Signer` whose `Sign` is a remote round-trip. That is
   the explicit "don't expose your private-key material" pitfall in the
   how-to, and it is the structural reason adding cloud backends here is
   *not* the rejected pattern.
3. **The framework already endorses adding exactly these.** The
   add-a-signing-backend how-to uses `gcp-kms` as its worked example and
   names GCP KMS / Azure Key Vault as first-class candidates. Promoting
   two of them from "you could write this" to "we ship and test this"
   is consistent with the accepted design, not a reversal of the
   rejected one.

There is **no conflict.** The distinction to preserve in review: *new
signing backends via the accepted `signing.Backend` registry are
sanctioned; a vendor-implementation secrets registry is not.*

## Background: the pattern to mirror (`pkg/signing/kms`)

The AWS backend is the template. Each new backend reproduces its exact
structure:

- **`<name>.go`** — package doc explaining the blank-import activation,
  the backend-specific CLI flag(s), and credential resolution; the
  `backend` struct implementing `Name`/`RegisterFlags`/`NewSigner`; the
  exported `NewSigner(ctx, …)` constructor that builds the real cloud
  client and delegates to the unexported `newSigner`; and an `init()`
  that calls `signing.Register(&backend{…})`.
- **`signer.go`** — a small `<vendor>Client` **interface** capturing
  only the SDK methods used (so tests supply a fake without the SDK's
  own mock framework), the `<vendor>Signer` adapting the remote key to
  `crypto.Signer`, and `newSigner(ctx, client, keyID)` which fetches the
  public half, parses it to `*rsa.PublicKey` (rejecting non-RSA), and
  returns the signer. `Sign` maps `opts.HashFunc()` to the vendor's
  RSASSA-PKCS1-v1_5 algorithm, **rejects PSS** (`*rsa.PSSOptions`)
  rather than silently downgrading, and **rejects unsupported hashes**.
- **`<name>_test.go`** — table-driven, `t.Parallel()`, fake client; the
  same eight scenarios `kms_test.go` covers (registration, flag parse,
  RSA happy path, non-RSA rejection, GetPublicKey error, malformed DER,
  nil opts, PSS rejection, unsupported-hash, hash→algorithm mapping,
  vendor-Sign error wrapping).

The crucial split — `newSigner` (testable via a fake to 100%) vs
`NewSigner` (the thin cloud-config wrapper that needs real cloud creds)
— is what lets the inner logic hit the coverage bar while the outer
wrapper is excluded. See
[Test coverage & `.coverage-policy.yaml`](#test-coverage--coverage-policyyaml).

### Sentinel-error parity

Each backend exports `errors.Is`-able sentinels matching the AWS set,
named per-vendor:

- `ErrUnsupported<Vendor>KeyType` — public half is not RSA.
- `ErrUnsupportedHashFunc` — caller requested a hash the vendor's RSA
  Sign does not map to.
- `ErrPSSUnsupported` — caller requested RSASSA-PSS; refused, not
  downgraded.

## SDK confirmation: signer surfaces & algorithm support

Confirmed against the current Go SDKs and provider docs (June 2026).

### GCP Cloud KMS — `cloud.google.com/go/kms/apiv1`

- **Client:** `*kms.KeyManagementClient` (from
  `kms "cloud.google.com/go/kms/apiv1"`), protos in
  `kmspb "cloud.google.com/go/kms/apiv1/kmspb"`.
- **Public half:** `GetPublicKey(ctx, *kmspb.GetPublicKeyRequest{Name:
  <version-resource-name>})` returns a `*kmspb.PublicKey` whose `Pem`
  field is a **PEM-encoded SubjectPublicKeyInfo**. Decode the PEM block,
  then `x509.ParsePKIXPublicKey` → assert `*rsa.PublicKey`. (Note: GCP
  returns PEM, where AWS returns raw DER — the only meaningful divergence
  from the AWS adapter.)
- **Sign:** `AsymmetricSign(ctx, *kmspb.AsymmetricSignRequest{Name,
  Digest})`. The request carries a **`Digest` oneof** with `Sha256` /
  `Sha384` / `Sha512` fields; the digest's hash **must match the key
  version's configured algorithm**. Response `*kmspb.AsymmetricSignResponse`
  has a `Signature []byte` plus a `SignatureCrc32C` integrity checksum
  the backend SHOULD verify.
- **Algorithms for OpenPGP detached signing (RSA, this backend):**
  `RSA_SIGN_PKCS1_2048_SHA256`, `RSA_SIGN_PKCS1_3072_SHA256`,
  `RSA_SIGN_PKCS1_4096_SHA256`, `…_4096_SHA512`. **GCP keys are
  algorithm-pinned**: a CryptoKeyVersion is created for one specific
  `RSA_SIGN_PKCS1_<size>_<hash>` and `AsymmetricSign` rejects a digest
  whose hash differs. The backend therefore reads `opts.HashFunc()`,
  populates the matching `Digest` oneof field (SHA-256/384/512), and
  lets GCP enforce the match — surfacing a wrapped error if the
  operator's `--created`/hash choice and the key's pinned algorithm
  disagree. GCP also offers `RSA_SIGN_PSS_*` and EC (`EC_SIGN_P256_SHA256`,
  …) algorithms, but PSS and EC are **out of scope** (PSS rejected; EC
  needs the non-RSA minting path).
- **`crypto.Signer` shape:** no first-party `crypto.Signer` is provided
  by the SDK; the adapter is the standard ~15-line wrapper (a
  well-known community pattern, e.g. `salrashid123/kms_golang_signer`),
  identical in shape to the AWS `kmsSigner`.

### Azure Key Vault — `azure-sdk-for-go/sdk/security/keyvault/azkeys`

- **Client:** `*azkeys.Client` constructed with a vault URL and an
  `azcore.TokenCredential` (typically `DefaultAzureCredential` from
  `azidentity`).
- **Public half:** `GetKey(ctx, name, version, nil)` →
  `GetKeyResponse{KeyBundle{Key *JSONWebKey}}`. The JWK carries the RSA
  modulus/exponent (`N`, `E`) for `KeyType` `RSA`/`RSA-HSM`; the backend
  reconstructs an `*rsa.PublicKey` from those big-endian byte fields
  (no DER/PEM step — JWK is the native form here).
- **Sign:** `Sign(ctx, name, version, azkeys.SignParameters{Algorithm:
  <alg>, Value: <digest>}, nil)` → `SignResponse{KeyOperationResult{Result
  []byte}}`. `Value` is the **pre-hashed digest**; `Result` is the **raw
  signature**.
- **Algorithms for OpenPGP detached signing (RSA, this backend):**
  `azkeys.SignatureAlgorithmRS256` / `RS384` / `RS512` (RSASSA-PKCS1-v1_5,
  the OpenPGP-compatible scheme). The backend maps `opts.HashFunc()`
  (SHA-256/384/512) to these. **Unlike GCP, an Azure RSA key is not
  algorithm-pinned** — the same key signs with RS256/384/512 — so the
  hash→algorithm mapping is purely caller-driven, exactly like the AWS
  adapter. Azure also supports `PS256/384/512` (PSS — rejected here) and
  `ES256/384/512`/`ES256K` (EC — out of scope, non-RSA path).
- **ECDSA raw-signature caveat (documented, not hit here):** Azure
  returns EC signatures as **raw `R‖S`**, not ASN.1/DER — relevant only
  if EC support is added later via the non-RSA minting path. RSA
  (RS256/384/512) returns the standard PKCS#1 v1.5 signature with no
  re-encoding needed, so this RSA-only backend is unaffected. Recorded
  here so the future EC follow-up does not rediscover it.

### Cross-backend summary

| Concern | AWS (`aws-kms`) | GCP (`gcp-kms`) | Azure (`azure-kv`) |
|---|---|---|---|
| Public-half format | DER (SPKI) | **PEM** (SPKI) | **JWK** (N/E) |
| Sign input | digest | digest (in `Digest` oneof) | digest (`Value`) |
| Sign output | raw PKCS#1 v1.5 sig | raw sig (+CRC32C) | raw sig |
| RSA PKCS#1 v1.5 | ✓ (this backend) | ✓ `RSA_SIGN_PKCS1_*` | ✓ `RS256/384/512` |
| Key algorithm-pinned? | no | **yes** (one hash per version) | no |
| PSS | rejected | rejected | rejected |
| EC / Ed25519 | n/a (KMS no Ed25519) | out of scope | out of scope |

All three converge on the same `crypto.Signer` contract
`pkg/openpgpkey` consumes: `Public() → *rsa.PublicKey`, `Sign(digest,
PKCS1v15)`.

## Design decisions

### D1 — Package layout & naming

New subpackages `pkg/signing/gcpkms` (name `gcp-kms`) and
`pkg/signing/azurekv` (name `azure-kv`). Package directory names avoid
hyphens (Go convention); the registry **name** is kebab-case to match
the existing `aws-kms`. CLI flags are vendor-namespaced to avoid
collisions when multiple backends are compiled in (cobra binds every
registered backend's flags onto one flag set, so names must be globally
unique across backends):

- `gcp-kms`: `--gcp-key-version` is **not** added separately — the GCP
  full resource name *is* the `--key-id` (e.g.
  `projects/p/locations/l/keyRings/r/cryptoKeys/c/cryptoKeyVersions/1`).
  Optional flag `--gcp-credentials-file` (path to a service-account JSON;
  default empty → Application Default Credentials).
- `azure-kv`: `--key-id` is the **full key identifier URL**
  (`https://<vault>.vault.azure.net/keys/<name>/<version>`), from which
  the vault URL, key name, and version are parsed. No extra required
  flag; optional `--azure-tenant-id` reserved for future explicit
  tenant pinning (default empty → `DefaultAzureCredential`).

> **Open question OQ1** — confirm the `--key-id` semantics above
> (full GCP resource name; full Azure key URL) versus splitting vault
> host into its own flag. Leaning to "key-id is the whole identifier"
> for symmetry with `aws-kms` accepting a full ARN.

### D2 — RSA-only, enforced at `newSigner`

Both backends reject a non-RSA public half with
`ErrUnsupported<Vendor>KeyType`, exactly as `aws-kms` does. This keeps
the backends honest against the `pkg/openpgpkey` RSA precondition and
fails early with a clear message rather than deep in OpenPGP framing.

### D3 — PSS refusal, hash mapping (parity with `aws-kms`)

`Sign` refuses `*rsa.PSSOptions` with `ErrPSSUnsupported` (no silent
scheme downgrade — a contract violation in an exported `crypto.Signer`),
and maps only SHA-256/384/512 to the vendor's PKCS#1 v1.5 algorithm,
returning `ErrUnsupportedHashFunc` otherwise. For `gcp-kms` the mapping
additionally selects the `Digest` oneof field; GCP enforces the
key-algorithm match server-side and the backend wraps any mismatch.

### D4 — Credential resolution deferred to the SDK chain

No `pkg/credentials` involvement. GCP uses Application Default
Credentials (`GOOGLE_APPLICATION_CREDENTIALS` / metadata server /
`gcloud` ADC); Azure uses `DefaultAzureCredential` (env →
workload-identity → managed-identity → Azure CLI). Each package doc
documents the chain and the CI/OIDC story (GCP Workload Identity
Federation; Azure OIDC federated credentials), mirroring how the
`aws-kms` doc covers IAM Roles Anywhere / web-identity. This keeps the
backends free of GTB-specific credential config and consistent with the
`Backend` contract.

### D5 — Dead-code elimination preserved (the load-bearing constraint)

The cloud SDKs are **heavy** (the GCP KMS client pulls gRPC + a large
genproto closure; the Azure SDK pulls `azcore`/`azidentity`). A
regulated or size-sensitive downstream must be able to link **none** of
a SDK it does not use. The blank-import activation pattern already
guarantees this: a backend's `init()` only runs — and its SDK is only in
the link closure — if some package blank-imports it. The structural
rules that keep this true:

1. **No `internal/cmd/*` package may import a cloud-SDK backend
   directly.** `keys` and `sign` reference only `pkg/signing`
   (registry), never `pkg/signing/gcpkms` etc. (verified: current `keys`
   and `sign` import only `pkg/signing`).
2. **The only on/off switch is `cmd/gtb/signing.go`**, mirroring the
   keychain switch in `cmd/gtb/keychain.go`. The shipped `gtb` binary
   blank-imports all four backends there; a downstream tool's `main`
   imports only what it wants.
3. **Compile-time + link-time smoke fixtures** prove the omission
   actually elides the SDK — see [Smoke fixtures](#smoke-fixtures).

This is the same dead-code-elimination story as the keychain
blank-import (`cmd/gtb/keychain.go`: "linker dead-code elimination then
keeps go-keyring, godbus, and wincred out of the artefact") and the
existing AWS slice (`cmd/gtb-no-aws-smoke`).

### Smoke fixtures

The repo already ships `cmd/gtb-no-aws-smoke` — a `main` that
blank-imports only `pkg/signing/local`, with a unit test asserting
`signing.Names()` returns exactly `{"local"}` (proving no other backend
leaks in transitively). We extend the proof to the new SDKs:

- **Reuse, don't multiply.** Rather than one `no-<vendor>` binary per
  cloud, generalise to a single `cmd/gtb-min-signing-smoke` fixture
  (local-only) whose unit test asserts `signing.Names() == {"local"}`,
  i.e. **none** of `aws-kms`/`gcp-kms`/`azure-kv` leak in. This subsumes
  the existing `gtb-no-aws-smoke` intent and covers all three cloud
  SDKs at once. *(OQ2: keep the existing `gtb-no-aws-smoke` name/binary
  and add the broader assertion, or rename? Leaning to keeping the
  existing binary and broadening its assertion to avoid churn in any CI
  job that references it.)*
- **Link-time arm.** The `gtb-no-aws-smoke` package doc describes a CI
  `go list -deps` / symbol check asserting no `github.com/aws/*` lands
  in the closure, but **no such CI job currently exists in
  `.gitlab-ci.yml` or `scripts/`** (confirmed — only `apidiff.sh`,
  `coverage-policy.sh`, `sign-release.sh` are present). This spec adds a
  `scripts/signing-deps-smoke.sh` that runs
  `go list -deps ./cmd/gtb-min-signing-smoke` and greps the closure for
  `github.com/aws/`, `cloud.google.com/go/kms`, and
  `github.com/Azure/azure-sdk-for-go`, failing if any appear. Wired as a
  non-blocking advisory CI job initially (consistent with `apidiff` /
  `coverage-policy` being advisory), promotable to blocking later.

> **Open question OQ2/OQ3** — (a) reuse vs rename the smoke binary;
> (b) advisory vs blocking for the new deps-smoke job from day one.

### D6 — `go.mod` weight & the standard binary

The standard `gtb` binary will link all three cloud SDKs (it
blank-imports them in `cmd/gtb/signing.go`), growing the default binary
and `go.sum`. This is acceptable for the reference binary — it is the
"batteries included" artefact — and is precisely why the elision story
(D5) matters for downstreams. *(OQ4: do we want a documented
`cmd/gtb-aws-only` / minimal reference build, or is the how-to guidance
"omit the import in your own main" sufficient? Leaning to sufficient —
downstreams build their own `main`; we should not multiply reference
binaries.)*

## Public API

No change to `pkg/signing`. Two new packages, each exporting:

```go
// pkg/signing/gcpkms
func NewSigner(ctx context.Context, keyID, credentialsFile string) (crypto.Signer, error)
var ErrUnsupportedGCPKeyType, ErrUnsupportedHashFunc, ErrPSSUnsupported error

// pkg/signing/azurekv
func NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
var ErrUnsupportedAzureKeyType, ErrUnsupportedHashFunc, ErrPSSUnsupported error
```

Both register a `backend` in `init()`. The exported `NewSigner`
mirrors `kms.NewSigner` — for integration tests and tool authors wiring
signing into their own command structure rather than `gtb keys mint`.

## Test coverage & `.coverage-policy.yaml`

`pkg/signing/kms` is in the `excluded` list of
[`.coverage-policy.yaml`](../../../.coverage-policy.yaml) with the
rationale:

> *"AWS KMS adapter; inner `newSigner` is 100% via a fake, the
> `NewSigner` AWS-config wrapper is untestable without real AWS."*

The new packages get the **same treatment** — the inner
`newSigner`/`Sign`/algorithm-mapping logic is covered to 100% via a
fake `<vendor>Client` (the `signer.go` interface is the seam), and only
the thin `NewSigner` real-client constructor is uncovered. Two new
`excluded` entries:

```yaml
  - { pkg: pkg/signing/gcpkms, reason: "GCP Cloud KMS adapter; inner newSigner is 100% via a fake, the NewSigner ADC-config wrapper is untestable without real GCP" }
  - { pkg: pkg/signing/azurekv, reason: "Azure Key Vault adapter; inner newSigner is 100% via a fake, the NewSigner DefaultAzureCredential wrapper is untestable without real Azure" }
```

The coverage policy requires a rationale per exclusion and that this
file stay in sync with the coverage-gap spec's Bucket A table — both
will be updated.

### Unit tests (hermetic, fake client)

Mirror `kms_test.go` per backend: registration; `RegisterFlags`
parse; RSA happy path (assert modulus matches the fetched public key,
assert the correct vendor algorithm flows through on SHA-256/384/512);
non-RSA rejection; GetPublicKey/GetKey error; malformed public-half
(bad PEM for GCP, malformed JWK for Azure); nil opts; PSS rejection
(assert the remote Sign is **not** called); unsupported-hash; vendor
Sign-error wrapping. GCP adds one case: digest-hash vs key-algorithm
mismatch surfaces a wrapped error.

### Integration tests (gated, desktop/CI-cred)

Per the project's env-var gating
([`internal/testutil`](../../../internal/testutil/integration.go)),
add `*_integration_test.go` gated by `INT_TEST_GCP=1` / `INT_TEST_AZURE=1`
(and `INT_TEST=1` for all). These require real cloud credentials and a
provisioned RSA signing key, so they are **deferred** like the existing
VCS/chat-live integration tests — registered in the integration-test
inventory and in the deferred-integration follow-up, not run on the
homelab.

### E2E / BDD

Extend `features/cli/sign.feature` and `features/cli/keys.feature`
**only** with `signing.Names()`-style assertions that the new backends
register and surface in `--help` (no live cloud calls in BDD). The
existing `internal/cmd/sign` / `internal/cmd/keys` packages are
E2E-covered exclusions in the coverage policy and need no source change.

## Documentation impact

- **`docs/components/signing.md`** — add `gcp-kms` and `azure-kv` to the
  built-in backend table and to the "Compile-time backend opt-out"
  section (the elision story now covers three SDKs).
- **`docs/how-to/add-signing-backend.md`** — the worked example already
  uses a hypothetical `gcp-kms`; update it to reference the now-real
  `pkg/signing/gcpkms` and `pkg/signing/azurekv` as production examples
  alongside `kms`/`local`, and correct the GCP `Sign` sketch to populate
  the `Digest` oneof.
- **`docs/concepts/release-binary-signing.md`** — note multi-cloud
  rooting options.
- Package docs in each new package (the doc comment is the primary
  reference for the CLI flag + credential chain), mirroring the
  `kms.go` package doc.

## Migration notes

Additive — no breaking change. Per the pre-1.0 policy this ships as a
**minor** bump (`feat(signing):`). No `docs/migration/` entry needed (no
public-API break). The standard binary gains the two SDKs in its link
closure; downstreams already on a custom `main` are unaffected unless
they opt in.

## Implementation plan (TDD)

1. `pkg/signing/azurekv` first (simplest: JWK public half, no
   algorithm-pinning, mapping identical to AWS). `signer.go` + fake →
   tests green → `azurekv.go` (backend + `init` + flags) → registration
   test.
2. `pkg/signing/gcpkms` (adds PEM decode + `Digest` oneof + the
   key-algorithm-match case). Same test-first order.
3. Blank-import both in `cmd/gtb/signing.go`.
4. Generalise `cmd/gtb-no-aws-smoke` (or add `cmd/gtb-min-signing-smoke`)
   + `scripts/signing-deps-smoke.sh`; wire the advisory CI job.
5. Two `.coverage-policy.yaml` exclusions with rationale.
6. Docs (components/signing, add-signing-backend how-to, concepts).
7. `/gtb-verify` (tests, race, lint, mocks); `go list -deps` smoke;
   `just snapshot` sanity on binary size delta.

## Open questions

- **OQ1 — `--key-id` semantics.** Confirm `--key-id` is the *full*
  identifier for both (GCP CryptoKeyVersion resource name; Azure key
  URL), with vault/project derived from it, vs splitting into separate
  flags. (Drafted as: full identifier, for ARN-symmetry with `aws-kms`.)
- **OQ2 — Smoke fixture.** Keep `cmd/gtb-no-aws-smoke` and broaden its
  assertion to "no aws/gcp/azure", or introduce a fresh
  `cmd/gtb-min-signing-smoke`? (Drafted as: keep + broaden.)
- **OQ3 — Deps-smoke CI job.** Advisory (`allow_failure: true`, like
  `apidiff`) or blocking from day one? (Drafted as: advisory, since
  there is no link-time check today at all.)
- **OQ4 — Reference binary weight.** Accept the standard `gtb` linking
  all three SDKs, or document a minimal reference build? (Drafted as:
  accept; downstreams build their own `main`.)
- **OQ5 — Algorithm validation depth (GCP).** Should `gcp-kms`
  pre-fetch the key version's algorithm and validate the hash *before*
  calling Sign (clearer error), or let GCP reject server-side and wrap?
  (Drafted as: let GCP reject + wrap, to keep one round-trip per mint
  like the AWS path.)
- **OQ6 — EC/Ed25519 follow-up.** Track a separate spec to extend
  `pkg/openpgpkey` to accept ECDSA/EdDSA `crypto.Signer`s, unlocking
  `EC_SIGN_*` (GCP) and `ES*` (Azure) — note the Azure raw-`R‖S` →
  ASN.1 re-encoding requirement captured above.

## Resolutions (open questions confirmed with user 2026-06-21)

- **OQ1 — `--key-id` semantics** — RESOLVED: **full identifier, single flag** for
  both backends (GCP CryptoKeyVersion resource name; Azure key URL), vault/project
  derived from it — symmetric with the `aws-kms` ARN model.
- **OQ2 — Smoke fixture** — RESOLVED: **keep and broaden `cmd/gtb-no-aws-smoke`**
  to assert "no aws/gcp/azure symbols". One fixture across all cloud SDKs. (Now
  framed as guarding the *downstream* elision property — see OQ4 — not `gtb`'s own
  distribution.)
- **OQ3 — Deps-smoke CI job** — RESOLVED: **advisory** (`allow_failure: true`, like
  `apidiff`/`coverage-policy`); there is no link-time check today, so start
  advisory and tighten later.
- **OQ4 — Reference-binary weight** — RESOLVED: **the `gtb` binary links all three
  cloud backends; accept the extra baggage.** Users must never switch binaries to
  switch signing backend — one `gtb` serves aws/gcp/azure. This differs from the
  keychain precedent (which gates a *local* capability). **Rejected:** splitting
  backends into separate submodules (B — fragments the binary/module story) and a
  download-at-init plugin (C — re-opens a rejected decision, breaks the
  CGO-off/FIPS static build, and puts downloaded code on the highest-trust signing
  path). The blank-import registry mechanism still allows a regulated downstream to
  build a leaner `main`, but GTB pursues **no** minimal reference build and does not
  treat elision as a goal for `gtb` itself.
- **OQ5 — GCP algorithm validation** — RESOLVED: **let GCP reject server-side and
  wrap** the error; no pre-fetch of the key algorithm — keeps one round-trip per
  mint, matching the AWS path.
- **OQ6 — EC/Ed25519** — RESOLVED: **separate follow-up spec.** This spec stays
  RSA-only (current `pkg/openpgpkey` constraint). Track extending `openpgpkey` to
  accept ECDSA/EdDSA `crypto.Signer`s — unlocking GCP `EC_SIGN_*` and Azure `ES*`
  (incl. the Azure raw-`R‖S` → ASN.1 re-encoding) — as its own spec.

## References

- [`pkg/signing/backend.go`](../../../pkg/signing/backend.go),
  [`registry.go`](../../../pkg/signing/registry.go) — the registry.
- [`pkg/signing/kms/kms.go`](../../../pkg/signing/kms/kms.go),
  [`signer.go`](../../../pkg/signing/kms/signer.go),
  [`kms_test.go`](../../../pkg/signing/kms/kms_test.go) — the pattern.
- [`pkg/signing/local/local.go`](../../../pkg/signing/local/local.go).
- [`internal/cmd/keys/mint.go`](../../../internal/cmd/keys/mint.go),
  [`internal/cmd/sign/sign.go`](../../../internal/cmd/sign/sign.go) —
  the consumers (unchanged).
- [`cmd/gtb/signing.go`](../../../cmd/gtb/signing.go),
  [`cmd/gtb/keychain.go`](../../../cmd/gtb/keychain.go),
  [`cmd/gtb-no-aws-smoke/`](../../../cmd/gtb-no-aws-smoke/) — the
  on/off switch and the elision smoke.
- [`.coverage-policy.yaml`](../../../.coverage-policy.yaml).
- Specs: [`2026-06-08-keys-mint-command.md`](2026-06-08-keys-mint-command.md),
  [`2026-06-09-sign-command.md`](2026-06-09-sign-command.md).
- How-to: [`add-signing-backend.md`](../../how-to/add-signing-backend.md).
- Decisions: [`feature-decisions.md`](../feature-decisions.md)
  (rejected secrets-provider registry).
- SDKs: `cloud.google.com/go/kms/apiv1`;
  `github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys`.
</content>
</invoke>
