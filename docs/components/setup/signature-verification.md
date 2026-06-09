---
title: "Signature Verification — Trust Anchors & Key Resolvers"
description: "pkg/setup signing primitives for Phase 2 self-update signature verification: an immutable TrustSet with a minimum-strength policy, a detached-signature verifier, and a pluggable KeyResolver chain (embedded, WKD, composite cross-check) that diffuses the signing trust anchor away from the VCS."
date: 2026-05-21
tags: [component, security, signing, self-update, openpgp, wkd]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Signature Verification — Trust Anchors & Key Resolvers

Phase 1 of secure self-update verifies a downloaded binary against the release's `checksums.txt` manifest (see [Remote Checksum Verification](index.md#remote-checksum-verification-phase-1)). That defends against a corrupted or truncated download, but it does **not** defend against an attacker who can publish a *replacement* `checksums.txt` — anyone who compromises the VCS platform can swap both the binary and the manifest.

Phase 2 closes that gap: the release pipeline signs `checksums.txt` with an OpenPGP key, and the updater verifies that detached signature against a **trust set** of vetted public keys before the manifest is ever parsed. This page documents the cryptographic primitives that make up the trust layer.

!!! note "Status"
    The verification path is wired end-to-end in `pkg/setup`: the `TrustSet` primitive, the `KeyResolver` chain, the strength policy, the `update.*` config keys, and the verify-before-parse gate inside `SelfUpdater.Update()` are all implemented and tested. What remains are **operational**, not code: producing GTB's own signed releases (the `scripts/sign-release.sh` signing step and the `.goreleaser.yaml` signs block) and standing up the WKD endpoint with GTB's published key. Those are tracked in the [remote-update-checksum-verification spec](../../development/specs/2026-04-02-remote-update-checksum-verification.md) and `docs/development/phase2-signing-prep.md`. A downstream tool can use the full path today by supplying its own keys.

## Threat Model

A signature is only as trustworthy as the key used to check it. If the public key travels through the same channel as the binary — baked into source on the VCS — then a single VCS compromise lets an attacker replace the binary, the manifest, the signature, *and* the key. The signature would still "verify", against the attacker's own key.

The defence is to **diffuse the trust anchor**: publish the public key at an independent service whose compromise is uncorrelated with a VCS compromise, and require the two to agree. GTB publishes its release key via [Web Key Directory (WKD)](https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/) under a domain it controls, and cross-checks the embedded key against the WKD-served key on every update.

| Attacker capability | Outcome |
|---------------------|---------|
| Controls VCS only | Can replace binaries and the embedded key. The WKD cross-check detects the mismatch → update aborts. |
| Controls WKD endpoint only (DNS hijack, TLS MITM) | Cannot replace binaries. Cross-check fails → update aborts with a clear alarm. |
| Controls **both** VCS and WKD endpoint | Full compromise. Now requires breaching two independent systems within the same detection window. |

The objective is not invulnerability but **cost**: raising the attacker's bar from "breach one system" to "breach two independent systems at once". Simultaneous compromise of both sources remains unsolved at this layer and is deferred to a future transparency-log phase.

## TrustSet

A `TrustSet` is an immutable collection of public keys that can validate an update signature. It is constructed by a [`KeyResolver`](#keyresolver) per update attempt.

```go
type TrustSet struct { /* ... */ }

func LoadTrustSet(armoredKeys ...[]byte) (*TrustSet, error)

func (t *TrustSet) Fingerprints() []string
func (t *TrustSet) VerifyManifestSignature(manifest, signature []byte) error
```

- **`LoadTrustSet`** parses one or more ASCII-armored public-key blobs and enforces the [minimum-strength policy](#minimum-strength-policy) at construction time. Any weak key in the input aborts the load, so a weak key never enters a trust set even transiently.
- **`Fingerprints`** returns the 40-character uppercase hex fingerprint of every key, sorted ascending — so two trust sets can be compared for equality by their fingerprint slices (this is what [`CompositeResolver`](#compositeresolver) uses to cross-check).
- **`VerifyManifestSignature`** verifies an ASCII-armored detached signature over the manifest using any key in the set. It returns `nil` on the first key that validates, and `ErrSignatureInvalid` for an empty, malformed, or non-validating signature. The failure path deliberately does **not** name the keys tried, so a caller that logs only the sentinel does not leak which key rejected the signature.

### Minimum-Strength Policy

Every key entering a trust set — embedded or fetched — is checked against a short, explicit accept-list:

| Algorithm | Decision |
|-----------|----------|
| Ed25519 (legacy `EdDSA` and modern `Ed25519` packet forms) | **Accepted** |
| RSA ≥ 3072 bits | **Accepted** |
| RSA < 3072 bits | Rejected (`ErrWeakKey`) |
| DSA, ElGamal, ECDH, ECDSA, X25519, X448, Ed448, RSA-encrypt-only | Rejected (`ErrWeakKey`) |
| Any unknown / future algorithm | Rejected (`ErrWeakKey`) — fails closed |

The policy fails closed: an algorithm a future `go-crypto` release might add is rejected by the `default` branch rather than slipping through. A weak **embedded** key surfaces at binary startup (the binary refuses to start); a weak **WKD** key fails the individual update with `ErrWeakKey`. This is fail-loud at whichever layer introduced the weak key.

## KeyResolver

`KeyResolver` decouples *where the trust anchor comes from* from *how a signature is verified against it*. The updater depends on the interface; the concrete chain is wired per tool.

```go
type KeyResolver interface {
    // Name returns a short identifier for logs and diagnostics
    // (e.g. "embedded", "wkd:openpgpkey.example.com", "composite[...]").
    Name() string

    // Resolve returns the trust set for the current update attempt.
    // Resolve may perform I/O on every call.
    Resolve(ctx context.Context) (*TrustSet, error)
}
```

Three implementations ship in Phase 2.

### EmbeddedResolver

Keys baked into the binary via `//go:embed`. No I/O; always available; preserves offline and air-gapped update paths.

```go
func NewEmbeddedResolver(armoredKeys ...[]byte) KeyResolver
```

Keys are parsed and strength-checked **at construction**, so a weak, malformed, or empty input **panics** in `NewEmbeddedResolver`. This is intentional: a broken embedded key is a build-time defect and must surface when the binary starts, not at the first update attempt. Tool authors typically call this from an internal `trustkeys` package at init, embedding their public keys:

```go
//go:embed keys/*.asc
var keyFS embed.FS

func Resolver() setup.KeyResolver {
    primary, _ := keyFS.ReadFile("keys/release.asc")
    return setup.NewEmbeddedResolver(primary)
}
```

### WKDResolver

Fetches a public key from a [Web Key Directory](https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/) URL derived from a release email. This is the independent, externally-administered trust anchor.

```go
type WKDResolverConfig struct {
    Email      string       // e.g. "release@phpboyscout.uk"
    HTTPClient *http.Client // wire pkg/http.NewClient
    URLOverride string      // tests only
}

func NewWKDResolver(cfg WKDResolverConfig) (KeyResolver, error)

// URL derivation, exported for tooling:
func WKDURLs(email string) (advanced, direct, advancedHost string, err error)
```

URL derivation follows the WKD draft §3.1: the SHA-1 of the lower-cased local part, encoded in Z-Base-32, plugged into an advanced URL (`https://openpgpkey.<domain>/.well-known/openpgpkey/<domain>/hu/<hash>?l=<local>`) and a direct fallback (`https://<domain>/.well-known/openpgpkey/hu/<hash>?l=<local>`).

!!! info "SHA-1 here is a directory lookup hash, not a security mechanism"
    The WKD wire format mandates SHA-1 to locate the key file. It is **not** used for integrity — signature verification runs on Ed25519/RSA via go-crypto. The `gosec` G401/G505 findings on this single use are exempted by path in `.golangci.yaml` for exactly this reason.

`Resolve` behaviour:

- Tries the **advanced** URL first; falls back to the **direct** URL only on HTTP 404. Any other failure (network, non-200, TLS, oversize, weak key) returns to the caller without falling through.
- Requires a hardened `*http.Client` — wire [`pkg/http.NewClient`](../http.md) so TLS 1.2+, certificate validation, the request timeout, and the HTTPS-downgrade redirect policy are all enforced. Non-HTTPS targets are refused outright.
- Caps the response body at `MaxWKDResponseSize` (64 KiB, accommodates multiple keys per identity) → `ErrWKDResponseTooLarge`.
- Parses the binary OpenPGP wire format and runs the same [strength policy](#minimum-strength-policy) as the embedded path.
- Surfaces network and HTTP failures as `ErrKeyResolverUnavailable`.

### CompositeResolver

Wraps an ordered list of resolvers and requires them to **agree** on the set of key fingerprints. The production default for GTB is `CompositeResolver{EmbeddedResolver, WKDResolver}`.

```go
type CompositeResolver struct {
    Resolvers  []KeyResolver
    RequireAll bool
    Logger     logger.Logger // optional; receives Warn on fail-open fallback
}
```

- Children run **concurrently**; resolve cost is `max(child latencies)`, not the sum.
- **Fingerprint disagreement always aborts** with `ErrKeyResolverMismatch`, regardless of `RequireAll`. Tampering of a single source must never be silenced.
- `RequireAll == true` (**fail-closed**): any child failure aborts with `ErrKeyResolverUnavailable`. Recommended where WKD downtime is acceptable as a "skip this update" signal.
- `RequireAll == false` (**fail-open**): child failures are logged at Warn (when a `Logger` is set) and the composite returns the surviving children's trust set, as long as at least one succeeded. Suitable for tools that must update through transient WKD outages.

```
key_source: embedded  →  EmbeddedResolver                    (no cross-check)
key_source: external  →  WKDResolver                         (single source of truth)
key_source: both      →  CompositeResolver{Embedded, WKD}    (default; cross-checked)
```

## Sentinel Errors

Match on these with `errors.Is`; the underlying cause is wrapped for diagnostics but the sentinel is the contract.

| Error | Meaning |
|-------|---------|
| `ErrSignatureInvalid` | No key in the trust set validated the signature; also returned for an empty or malformed signature. |
| `ErrSignatureMissing` | `require_signature` is set but no signature asset was found in the release. |
| `ErrWeakKey` | A key (embedded or fetched) failed the minimum-strength policy. |
| `ErrSignatureTooLarge` | The detached-signature download exceeded `MaxSignatureSize`. |
| `ErrWKDResponseTooLarge` | A WKD response exceeded `MaxWKDResponseSize`. |
| `ErrKeyResolverUnavailable` | A resolver could not produce a trust set (network/HTTP failure, or all children failed). |
| `ErrKeyResolverMismatch` | Successful resolvers returned divergent fingerprint sets — a tampering alarm. |

## Tunable Bounds & Defaults

All exported as package variables so a downstream tool author can override them in `main()`:

| Variable | Default | Purpose |
|----------|---------|---------|
| `MaxSignatureSize` | 8 KiB | Cap on a detached-signature download (real signatures are < 1 KiB). |
| `MaxWKDResponseSize` | 64 KiB | Cap on a WKD key fetch (room for multiple keys per identity). |
| `DefaultRequireSignature` | `false` | Compile-time default for signature enforcement. Set `true` once a signed release exists and clients have received an embedded key in a prior release. |
| `DefaultKeySource` | `"both"` | `embedded` \| `external` \| `both`. |
| `DefaultRequireExternalCrosscheck` | `false` | When `true`, a WKD fetch failure aborts the update rather than silently falling back to embedded-only. |
| `DefaultExternalKeyEmail` | `""` | Email used to derive the WKD URL; set to the tool's release email. |

## Wiring into SelfUpdater

`SelfUpdater` consumes the trust layer automatically during `Update()`. A tool author supplies the keys; the framework builds the resolver from config.

### Supplying keys

```go
//go:embed keys/release.asc
var releaseKey []byte

updater, err := setup.NewUpdater(ctx, props, version, force,
    setup.WithEmbeddedKeys(releaseKey),
)
```

`WithEmbeddedKeys` hands the framework the raw armored keys; `NewUpdater` calls [`BuildKeyResolver`](#buildkeyresolver) with the resolved `update.key_source` family to produce the resolver. For full control — a custom resolver chain, a DNS resolver, or Sigstore in a later phase — build it yourself and pass `setup.WithKeyResolver(r)`, which bypasses the config-driven default entirely.

### BuildKeyResolver

```go
func BuildKeyResolver(cfg KeyResolverConfig, embeddedKeys ...[]byte) (KeyResolver, error)
```

Maps a `key_source` onto a concrete resolver:

| `key_source` | Result | Errors when |
|--------------|--------|-------------|
| `embedded` | `EmbeddedResolver(keys)` | no keys supplied |
| `external` | `WKDResolver(email)` | no `external_key_email` |
| `both` (default) | `CompositeResolver{Embedded, WKD}` when both keys and email are present; degrades to whichever single source is configured | neither keys nor email |

`RequireExternalCrosscheck` maps to `CompositeResolver.RequireAll`. Embedded keys are strength-checked here, so a weak or malformed key is returned as an error from `NewUpdater` (not a panic).

### Configuration

End users tune behaviour via the `update.*` config keys (each also settable via the tool's env prefix and resolved with the same precedence as `require_checksum`: explicit config → env var → compile-time default):

```yaml
update:
  require_signature: false            # DefaultRequireSignature
  signature_asset_name: ""            # override "checksums.txt.sig"
  key_source: both                    # embedded | external | both
  external_key_email: ""              # WKD lookup email; DefaultExternalKeyEmail
  require_external_crosscheck: false  # abort if WKD unreachable (CompositeResolver.RequireAll)
```

### Verification ordering and failure policy

During `Update()`, after the checksums manifest is downloaded but **before** it is parsed:

1. The trust set is resolved (the composite cross-check runs here — the earliest failure point).
2. The detached signature is fetched (via [`SignatureProvider`](../vcs/release.md#signatureprovider-optional-interface) when the provider opts in, else by asset-name lookup).
3. The signature is verified over the **raw manifest bytes**. Only on success is the manifest parsed for checksum comparison.

Failure handling is deliberately asymmetric:

| Condition | Outcome |
|-----------|---------|
| Signature present but does not verify | **Always fatal** (`ErrSignatureInvalid`) — a forged/corrupt signature is never accepted |
| Trust anchors disagree (`ErrKeyResolverMismatch`) | **Always fatal** — active-tampering signal |
| Signature absent, resolver unreachable, or no resolver configured | Gated by `require_signature`: fail-closed aborts, fail-open logs a warning and proceeds |

## Key Rotation

Trust sets hold multiple keys, and verification passes if **any** key validates the signature. During a rotation window the release pipeline dual-signs `checksums.txt` with both the outgoing and incoming keys, and both keys are served from WKD (and embedded). Once every supported tool version has shipped with the new key, the old key is dropped from the trust set and removed from WKD. Emergency rotation via a separate rotation-authority key is documented in the spec but deferred beyond Phase 2.

## See Also

- [Setup Package](index.md) — the surrounding self-update system and Phase 1 checksum verification.
- [Secure Releases How-To](../../how-to/secure-releases.md) — operator-facing setup story.
- [HTTP client](../http.md) — the hardened client `WKDResolver` expects.
- [remote-update-checksum-verification spec](../../development/specs/2026-04-02-remote-update-checksum-verification.md) — full design, decisions, and rollout phases.
