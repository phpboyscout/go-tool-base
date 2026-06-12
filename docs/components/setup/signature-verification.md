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

!!! success "Status — Phase 2 shipped"
    Both the verification primitives and the production wiring are live:

    - **v0.12.2** (2026-06-09) — first signed release. `checksums.txt.sig` attached to every release going forward; signature produced by an AWS-KMS-held RSA-4096 key via OIDC-federated CI.
    - **v0.13.0** (2026-06-09) — `setup.DefaultRequireSignature = true`. Every update now refuses to install an unsigned release.
    - **v0.13.1** (2026-06-10) — wired `setup.DefaultExternalKeyEmail = "release@phpboyscout.uk"` so the resolver chain becomes `CompositeResolver{Embedded, WKD}` by default. Before this, the verifier silently degraded to embedded-only — see [Interpreting verifier log output](#interpreting-verifier-log-output) below.

    Downstream tools using `pkg/setup` get the same wiring out of the box by setting `setup.DefaultExternalKeyEmail` in their own `main()` (or by passing `update.external_key_email` via config). The [phase2-signing-prep doc](../../development/phase2-signing-prep.md) and the [remote-update-checksum-verification spec](../../development/specs/2026-04-02-remote-update-checksum-verification.md) cover the rollout history end-to-end.

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

Every key entering a trust set — embedded or fetched — is checked against a short, explicit accept-list. The check covers the primary key **and every signing-capable subkey**, because signature verification resolves issuers including subkeys; the RSA floor is also enforced at verification time (`MinRSABits`) as defense-in-depth.

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

#### URL derivation — the only configurable input is the email

URL derivation follows [draft-koch-openpgp-webkey-service §3.1][wkd-draft]. Given a release email `release@example.org`, the resolver computes:

| Component | Source |
|-----------|--------|
| Local part | Everything before the `@` (`release`) |
| Domain | Everything after the `@`, lowercased (`example.org`) |
| Hash | SHA-1 of the lower-cased local part, encoded in z-base-32 |
| **Advanced URL** | `https://openpgpkey.<domain>/.well-known/openpgpkey/<domain>/hu/<hash>?l=<localpart>` |
| **Direct URL** (fallback on 404) | `https://<domain>/.well-known/openpgpkey/hu/<hash>?l=<localpart>` |

The `openpgpkey.` subdomain prefix is **not configurable** — it is part of the WKD wire format. Every WKD client (GnuPG, our `WKDResolver`, Sequoia, etc.) hardcodes this prefix when constructing the advanced URL, so a WKD-publishing domain serves the key under that fixed pattern.

In practice: **the only thing a tool author aligns across the framework, the DNS, and the hosting account is the release email.** Setting `setup.DefaultExternalKeyEmail` (or `update.external_key_email` via config) is sufficient — the verifier derives the URLs, the operator stands up the matching `openpgpkey.<domain>` endpoint, the keys flow.

[wkd-draft]: https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/

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

## Interpreting verifier log output

Every `gtb update` emits two structured log lines that tell you exactly which trust anchors were consulted. They're the primary diagnostic both for operators and for support conversations.

### `update signature verification configured`

Emitted once at the start of an update attempt, **before** any network I/O for signature material. The `resolver` field names the concrete `KeyResolver` that will be asked to produce the trust set.

```
INFO update signature verification configured resolver=<name>
```

| `resolver=<name>` value | Meaning | Trust-anchor count |
|--------------------------|---------|--------------------|
| `embedded` | Only the keys baked into the binary at build time. WKD is **not** consulted. Happens when `update.key_source` is `embedded`, OR when `key_source=both` but `update.external_key_email` is empty. | 1 |
| `wkd:<host>` | Only the keys served from that WKD endpoint. The embedded set is **not** consulted. Happens when `update.key_source` is `external`, OR when `key_source=both` but no keys were embedded into the binary. | 1 |
| `composite[embedded,wkd:<host>]` | **The intended Phase 2 default.** Both the embedded set AND the WKD-served set are fetched. Their fingerprints must agree, or the update aborts with `ErrKeyResolverMismatch`. | 2 |
| `composite[wkd:<host>,…]` (multiple WKD entries) | A tool author wired more than one external anchor. All listed entries are fetched in parallel and must agree. | ≥2 |

### `signature verified`

Emitted after the detached signature has been verified against the resolved trust set — i.e. after a successful signature check.

```
INFO signature verified resolver=<name>
```

The `resolver` value matches the one logged at `configured` (verification runs against the trust set the resolver produced). Seeing this line means:

1. `update.require_signature` was enabled (or the release happened to ship a sig anyway).
2. The trust set resolved successfully — including, for `composite[…]`, that all configured anchors **agreed on the same fingerprint set**.
3. The OpenPGP signature over `checksums.txt` validated against at least one key in that trust set.

### Failure-side log lines

Mismatch, missing signature, weak key, or signature-doesn't-validate all log at `Warn` or `Error` with the matching sentinel from the [Sentinel Errors](#sentinel-errors) table. Notable failure shapes:

```
WARN composite resolver failed (RequireAll=false, continuing) resolver=wkd:openpgpkey.example.com err=...
```

A child resolver in a `composite[…]` chain failed (typically a transient network error or a 404 from the WKD endpoint), but `RequireAll` was `false` (the default), so the surviving resolver's trust set was used and the update continued. Re-run with `--debug` and check the WKD endpoint if you see this consistently.

```
ERROR ErrKeyResolverMismatch: composite: resolvers returned divergent fingerprint sets
```

**Active-tampering signal.** Two trust anchors disagreed on which key is currently valid. The update is aborted; investigate which anchor is wrong before retrying. This is the highest-priority signal the verifier emits.

### Customer-facing summary

For tickets where a user asks "is my update actually being verified?", read the `resolver=` value:

- `resolver=composite[embedded,wkd:openpgpkey.phpboyscout.uk]` — full two-of-three trust-anchor independence. Both `internal/trustkeys/keys/*.asc` (embedded) and the live key served from Cloudflare Pages were consulted and agreed.
- `resolver=embedded` — single-anchor verification. The embedded key alone was authoritative. **Cryptographically sound, but lower defence-in-depth.** This was the state of v0.13.0 binaries between 2026-06-09 (Phase 2 ship) and 2026-06-10 (the `DefaultExternalKeyEmail` fix in v0.13.1). Tell the customer: their next `gtb update` (from v0.13.1+) will upgrade to the composite resolver automatically.
- `resolver=wkd:…` — single-anchor verification via WKD only. The customer's tool was built without an embedded key; the externally-served key is the sole trust anchor.

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
