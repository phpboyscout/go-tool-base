---
title: "Sigstore/Rekor Transparency-Log Signing — additive keyless verification path (Phase 3)"
description: "Add keyless cosign signing (OIDC-derived ephemeral keys → Fulcio cert → Rekor public transparency log) at release time, plus an optional pkg/setup RekorResolver / cosign-bundle verification path, offered ALONGSIDE the existing GPG/KMS detached-signature chain — not as a replacement. Diffuses the trust anchor across a third independent system (the Rekor log) to close the simultaneous VCS+WKD compromise gap left open by Phase 2, while preserving the offline-update guarantee by making transparency-log verification strictly opt-in and network-gated."
date: 2026-06-21
status: DRAFT
tags:
  - specification
  - setup
  - security
  - update
  - signing
  - sigstore
  - cosign
  - rekor
  - transparency-log
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Sigstore/Rekor Transparency-Log Signing Specification

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   21 June 2026

Status
:   DRAFT — for human review. This spec is **additive**: it proposes a third,
    independent verification path layered onto the existing GPG/KMS detached
    signatures. It does **not** remove, weaken, or change the default behaviour
    of the Phase 2 GPG path. Several open questions remain (see
    [Open Questions](#open-questions)); resolve each before implementation.

---

## Summary

GTB's self-update integrity story today is two layers deep:

1. **Phase 1 — same-origin checksums.** `pkg/setup/checksum.go` verifies the
   downloaded binary against a GoReleaser `checksums.txt` manifest.
2. **Phase 2 — GPG/KMS detached signatures.** `pkg/setup/signing.go` verifies a
   detached ASCII-armored OpenPGP signature (`checksums.txt.sig`) over the
   manifest. The signing private key lives in **AWS KMS** (accessed via GitLab
   CI OIDC; see `.gitlab-ci.yml`), the public key is **embedded** in the binary
   (`WithEmbeddedKeys`) **and** cross-checked against a **Web Key Directory
   (WKD)** endpoint (`WKDResolver`), combined via `CompositeResolver`. The
   signature is produced by `gtb sign --backend aws-kms` (spec
   [`2026-06-09-sign-command.md`](2026-06-09-sign-command.md)) through the
   `pkg/signing` backend registry, invoked from `scripts/sign-release.sh` in the
   GoReleaser `signs:` block.

Phase 2's threat analysis (spec
[`2026-04-02-remote-update-checksum-verification.md`](2026-04-02-remote-update-checksum-verification.md)
§ "What remains unsolved") explicitly leaves one gap open:

> **Simultaneous compromise of VCS and WKD endpoint**: the attacker controls
> both sources of truth; no cross-check can detect this. Mitigation requires a
> third independent trust root (Sigstore Rekor transparency log, Phase 3).

That same spec lists "Sigstore/Rekor transparency log" as a deferred **Future
Consideration / Phase 3**, to be specified separately. **This is that spec.**

The proposal: at release time, additionally produce a **keyless cosign
signature** over `checksums.txt`. Keyless signing derives an *ephemeral*
key-pair, obtains a short-lived X.509 signing certificate from **Fulcio**
bound to the CI **OIDC identity** (the GitLab pipeline's
`project_path:…:ref_type:tag:ref:v*` claim), and records the signature +
certificate in the **Rekor public transparency log**. The resulting cosign
*bundle* (signature, certificate, and Rekor inclusion proof) is uploaded as a
release asset (`checksums.txt.cosign.bundle`). On the client, an **opt-in**
`RekorResolver` / cosign-bundle verification path verifies that bundle as a
*third independent attestation* of the manifest — its trust anchors (the
Sigstore Fulcio root and Rekor public key, plus the expected OIDC identity) are
administered by neither the VCS platform nor the project's WKD domain.

Crucially, this path is **additive and opt-in**:

- The GPG/KMS path remains the default and the only path that works **offline**
  and **air-gapped** (Rekor verification requires network reachability to the
  log — or a pre-fetched inclusion proof; see [Offline conflict](#offline-update-conflict)).
- Tools enable the Rekor path explicitly. When enabled, it composes with the
  existing GPG path under the same `KeyResolver` / cross-check machinery rather
  than displacing it.

---

## Decision-Log Note — open + explicitly deferred (confirmed, no conflict)

Cross-checked against every spec that touches this area:

| Spec | Status of Sigstore/Rekor |
|------|--------------------------|
| `2026-04-02-remote-update-checksum-verification.md` | **Deferred Phase 3 / Future Consideration.** Resolved Decision #2 keeps "Cosign keyless via Sigstore … as a viable addition for tools that need transparency-log auditability — retained as Phase 3." § "What remains unsolved" names it as the mitigation for the simultaneous VCS+WKD gap. The config block already reserves `update.sigstore_rekor_url` "(Phase 3 only)". |
| `2026-06-09-sign-command.md` | `gtb sign` is **OpenPGP-only** by design (D7: "single output: armored OpenPGP detached signature"; out-of-scope excludes other formats). The `pkg/signing` backend registry it consumes is documented as having an **additive** evolution path. Cosign signing is a *new, parallel* path, not a change to `gtb sign`. **No conflict.** |
| `2026-03-26-offline-update-support.md` | "No signature verification in Phase 1 … signature support is a future consideration"; Future Considerations explicitly names "GPG **or cosign** signature validation". The offline path (`UpdateFromFile`) reads from a local tarball with a `.sha256` sidecar and **must keep working with no network**. This spec must not make offline updates depend on Rekor. **Constraint, not conflict** — captured in [Offline conflict](#offline-update-conflict). |

**Conclusion: this work is genuinely open and explicitly deferred. There is no
conflicting decision elsewhere that this spec would override.** The one hard
constraint inherited from the offline spec — *never make the default or offline
update paths depend on transparency-log reachability* — is honoured by making
the Rekor path strictly opt-in.

---

## Goals

1. Produce a keyless cosign signature + Rekor transparency-log entry over
   `checksums.txt` at release time, **in addition to** the GPG/KMS detached
   signature, uploaded as a release asset.
2. Add an **opt-in** client verification path in `pkg/setup` that verifies the
   cosign bundle (signature, Fulcio cert chain, Rekor inclusion proof) and binds
   it to the **expected OIDC signing identity** (issuer + subject regex).
3. Integrate the new path through the **existing** `KeyResolver` /
   `CompositeResolver` abstraction so a tool can require GPG **and** Rekor to
   agree, or treat Rekor as one more independent cross-check resolver.
4. Preserve **offline-update** behaviour and the existing default exactly:
   no tool gains a network dependency it didn't have, and `UpdateFromFile`
   continues to work with no Rekor access.
5. Keep `gtb sign` (OpenPGP) untouched; expose cosign signing as a separate,
   clearly-bounded mechanism.

## Non-Goals

- **Replacing GPG/KMS signing.** The detached OpenPGP signature stays the
  default trust path. This is an *assessment of an additive path*, per the task
  framing.
- **SLSA build provenance / attestations** (Phase 5) — provenance about *how*
  the binary was built is a separate concern from *who signed the manifest*.
  A cosign *attestation* (in-toto predicate) is a natural future companion but
  is out of scope here; this spec signs the checksums manifest only.
- **Self-hosted Rekor / Fulcio.** v1 targets the Sigstore public-good instances
  (`rekor.sigstore.dev`, `fulcio.sigstore.dev`) with configurable URLs so a
  downstream tool *could* point at a private instance, but operating one is out
  of scope.
- **Signing individual binaries.** As in Phase 2, signing the *manifest* covers
  every asset through the SHA-256 hash chain. One cosign signature, not N.
- **Changing `gtb sign`'s output format** (OpenPGP detached only — D7 of the
  sign-command spec).

---

## Background: how keyless cosign differs from the Phase 2 GPG path

| Property | GPG/KMS (Phase 2, default) | Keyless cosign (Phase 3, this spec) |
|----------|----------------------------|--------------------------------------|
| Private key | Long-lived, held in AWS KMS | **Ephemeral**, generated per-signing, discarded immediately |
| Identity binding | The key's fingerprint (embedded + WKD) | The **OIDC identity** in the Fulcio cert (`issuer` + `subject`) |
| Trust anchor at verify time | Embedded public key ∧ WKD-served key | Fulcio root cert ∧ Rekor public key ∧ expected OIDC identity |
| Independent of VCS? | Yes (WKD on a separate domain) | Yes (Sigstore PKI + log, separate operators) |
| Independent of WKD? | n/a | **Yes** — this is the new third anchor |
| Public auditability | None (no log) | **Rekor append-only transparency log**, monitorable |
| Works offline? | **Yes** (embedded key) | **No by default** (needs Rekor; mitigations below) |
| Revocation/forensics | Manual key rotation (Phase 4) | Log entry is permanent, timestamped, queryable by identity |

The defining benefit of the Rekor path is **public auditability**: every
signature is an append-only, timestamped log entry tied to a verifiable OIDC
identity. A malicious release signed under a stolen-but-valid CI identity is at
least *recorded* and detectable by a monitor, where a GPG-only forgery (if the
KMS policy were breached) leaves no external trace.

---

## Trust model — what the third anchor buys

Phase 2's per-attacker-capability table extends with a row:

| Attacker capability | Phase 2 outcome | With Phase 3 Rekor cross-check (required) |
|---------------------|-----------------|-------------------------------------------|
| Controls VCS only | WKD cross-check detects → abort | Also fails: no valid Fulcio cert for expected identity / no Rekor entry → abort |
| Controls WKD only | Cross-check fails → abort | Independent of WKD; Rekor still binds the manifest |
| **Controls VCS *and* WKD** | **Undetected — Phase 2's open gap** | **Detected**: attacker still cannot mint a Fulcio cert for the real OIDC identity nor forge a Rekor inclusion proof → abort |
| Controls VCS + WKD + steals a valid CI OIDC token within its short TTL | Undetected | Signature is produced under the *real* identity but is **publicly logged in Rekor** → detectable post-hoc by a monitor; not silent |
| Controls Sigstore infra (Fulcio + Rekor) | n/a | New residual risk — but uncorrelated with a VCS/WKD breach; raises the bar to *three* independent systems |

The objective, as in Phase 2, is **cost**, not invulnerability: requiring Rekor
agreement raises the attacker's bar from "breach two independent systems" to
"breach three" (VCS + WKD + Sigstore), and converts the one previously-silent
full-compromise case into a *publicly recorded* one.

### Residual risks introduced

- **Sigstore public-good availability/trust.** The verification path now depends
  (when enabled and required) on the Sigstore root of trust (the TUF-distributed
  `trusted_root.json`) and on Rekor reachability. A Sigstore outage must **not**
  brick updates for tools that keep GPG as the primary path — hence the
  fail-open/fail-closed gating below mirrors Phase 2 exactly.
- **OIDC identity misconfiguration.** If the expected `issuer`/`subject` regex is
  too loose (e.g. any GitLab project), an attacker with *any* GitLab pipeline
  could mint a cert. The identity matcher MUST be pinned tightly (the same
  `project_path:phpboyscout/go-tool-base:ref_type:tag:ref:v*` claim already used
  by the KMS role's trust policy). Captured as a [resolved decision](#resolved-decisions).
- **New dependency surface.** cosign's Go verification libraries
  (`sigstore/sigstore-go` or `sigstore/cosign/v2/pkg/...`) pull a substantial
  dependency tree (TUF client, in-toto, protobuf-specs). See
  [Open Question Q1](#open-questions) on whether this lives behind a build tag /
  separate module to keep it out of the default `cmd/gtb` binary — mirroring the
  `pkg/credentials/keychain` blank-import isolation pattern.

---

## Offline update conflict {#offline-update-conflict}

*Analysis and resolution of the air-gapped constraint.*

The offline spec's `UpdateFromFile` path (and air-gapped tools generally) is the
hard constraint. Three facts:

1. **Rekor verification is inherently online** unless the Rekor *inclusion proof*
   and a Signed Entry Timestamp (SET) are bundled with the artefact. The modern
   cosign **bundle** format (`*.cosign.bundle`, a Sigstore protobuf bundle) *can*
   embed the signed inclusion proof, enabling **offline verification of the log
   entry** against a pinned Rekor public key — *without* contacting Rekor at
   verify time. This is the key enabler that makes an offline-tolerant Rekor path
   feasible at all.
2. Even with an embedded inclusion proof, offline verification still needs a
   **pinned trust root** (Fulcio root + Rekor public key + CT log key), which the
   binary can embed via `//go:embed` (a snapshot of Sigstore's TUF
   `trusted_root.json`) exactly as Phase 2 embeds the GPG public key.
3. The Sigstore TUF root **rotates** periodically; a stale embedded root would
   eventually reject valid signatures. Online tools refresh it via TUF; offline
   tools accept a documented staleness window and update the embedded root with
   each GTB release.

**Resolution:** two sub-modes, both opt-in:

- **`rekor` (online):** verify the bundle and, if configured, additionally
  confirm the entry's presence by querying Rekor. Default for connected tools
  that opt in.
- **`rekor-offline` (bundle-only):** verify the embedded inclusion proof and SET
  against the embedded/pinned Sigstore trust root, with **no** network call to
  Rekor. Suitable for `UpdateFromFile` and air-gapped tools that still want the
  transparency-log attestation.

The library default for **all** existing tools is **unchanged** — Rekor
verification is `off` unless a tool sets `DefaultRekorVerification` /
`update.rekor_verification`. `UpdateFromFile` gains **optional** bundle
verification (analogous to its existing optional `.sha256` sidecar check): if a
`checksums.txt.cosign.bundle` sidecar is present *and* the tool opted into
`rekor-offline`, it is verified; otherwise the existing behaviour is byte-for-byte
preserved.

---

## Design

### Release-time signing (GoReleaser)

GoReleaser has first-class cosign support via the `signs:` block with
`cmd: cosign`. The existing GPG block stays; a **second** `signs:` entry is added,
gated (like the existing one) on a CI env var so non-release / unprovisioned
builds skip it:

```yaml
# .goreleaser.yaml — ADDITIVE second signing entry (illustrative)
signs:
  - id: checksums                      # EXISTING — GPG/KMS detached sig, unchanged
    cmd: scripts/sign-release.sh
    artifacts: checksum
    signature: "${artifact}.sig"
    args: ["${artifact}", "${signature}"]
    output: true
  - id: checksums-cosign               # NEW — keyless cosign, Rekor-logged
    cmd: cosign
    artifacts: checksum
    signature: "${artifact}.cosign.bundle"
    args:
      - "sign-blob"
      - "--yes"
      - "--oidc-provider=gitlabci"     # keyless: token from GitLab CI id_tokens
      - "--bundle=${signature}"
      - "${artifact}"
    output: true
```

Notes:
- **Keyless OIDC token.** GitLab CI supplies the id_token via the same
  `id_tokens:` mechanism already used for AWS in `.gitlab-ci.yml`; a new id_token
  with `aud: sigstore` is added to the `goreleaser` job. No long-lived secret.
- The cosign binary is added to the release job image (or installed in
  `before_script`). The GPG/KMS path (`gtb sign --backend aws-kms`) is untouched,
  so a cosign or Sigstore outage does **not** block the existing signature.
- Both signature assets (`checksums.txt.sig` **and** `checksums.txt.cosign.bundle`)
  attach to the release. Existing clients ignore the new asset entirely.

### Client-time verification (`pkg/setup`)

The cleanest integration reuses the Phase 2 `KeyResolver` seam. Today a resolver
returns a `*TrustSet` of OpenPGP keys and the updater calls
`TrustSet.VerifyManifestSignatureSigner(manifest, sig)`. The Rekor path verifies
a *different* artefact (a cosign bundle) against a *different* anchor (Sigstore
trust root + OIDC identity), so it does not fit the OpenPGP `TrustSet` shape
directly. Two integration options (see [Q2](#open-questions)):

**Option A — parallel `SignatureVerifier` abstraction (recommended).** Introduce
a narrow interface that both the existing GPG path and the new cosign path
implement, and let the updater run one or more verifiers with the same
fail-open/fail-closed and cross-check policy already in `verifyManifestSignature`:

```go
// SignatureVerifier verifies an integrity attestation over the raw
// checksums manifest bytes. Implementations include the existing
// OpenPGP/TrustSet verifier and the new cosign/Rekor verifier. The
// returned identity string is recorded in the audit log (an OpenPGP
// fingerprint, or a Fulcio OIDC subject).
type SignatureVerifier interface {
    Name() string
    // Verify returns the verified signer identity on success. A
    // present-but-invalid attestation MUST return a non-nil error
    // (always fatal, mirroring the Phase 2 contract). A genuinely
    // absent attestation returns ErrAttestationMissing so the
    // updater can apply the require/fail-open policy.
    Verify(ctx context.Context, rel release.Release, manifest []byte) (identity string, err error)
}
```

The existing OpenPGP logic in `verifyManifestSignature` /
`verifyAgainstTrustSet` is refactored behind a `gpgVerifier` implementing this
interface (no behaviour change), and a new `cosignVerifier` is added. A
`CompositeVerifier` (analogous to `CompositeResolver`) can require *all*
configured verifiers to succeed (strict cross-check) or *any* (defence-in-depth
without hard-failing on one anchor's outage).

**Option B — RekorResolver under the existing KeyResolver.** Shoehorn cosign
verification into a `KeyResolver` whose `Resolve` returns a synthetic/empty
`TrustSet` after independently verifying the bundle. Rejected as the leading
option: it abuses the `TrustSet` contract (which is OpenPGP-specific) and muddies
the fingerprint-agreement cross-check in `CompositeResolver`.

### New `cosignVerifier` shape (illustrative)

```go
// CosignVerifierConfig parameterises the keyless cosign / Rekor
// verification path. All fields have safe zero-values; a tool author
// sets ExpectedIssuer + ExpectedIdentity at minimum.
type CosignVerifierConfig struct {
    // ExpectedIssuer is the OIDC issuer URL that must have minted the
    // Fulcio cert, e.g. "https://gitlab.com".
    ExpectedIssuer string

    // ExpectedIdentity is a RE2 regex (compiled via
    // regexutil.CompileBounded — pattern may originate from config)
    // matched against the cert's SAN/subject, e.g.
    // `^https://gitlab\.com/phpboyscout/go-tool-base//?.*@refs/tags/v.*$`.
    ExpectedIdentity string

    // Mode is "rekor" (online, may query the log) or "rekor-offline"
    // (verify the embedded inclusion proof only; no network).
    Mode string

    // TrustedRoot is the pinned Sigstore trust root (TUF
    // trusted_root.json snapshot). When nil, online mode fetches via
    // TUF; offline mode requires it.
    TrustedRoot []byte

    // RekorURL / FulcioURL override the public-good instances.
    RekorURL  string
    FulcioURL string

    // BundleAssetName overrides "checksums.txt.cosign.bundle".
    BundleAssetName string

    HTTPClient *http.Client // hardened pkg/http client
    Logger     logger.Logger
}
```

### Bundle retrieval

Reuse the existing `release.SignatureProvider` optional-interface plumbing and
asset-by-name lookup already in `update_signature.go` (`fetchSignature` /
`findSignatureAsset`), parameterised by the bundle filename. Bounded by a new
`MaxCosignBundleSize` (bundles are a few KiB; cap at e.g. 64 KiB). The hardened
`pkg/http` client is used for any Rekor/TUF fetch, consistent with the WKD
resolver's request hygiene (HTTPS-only, TLS 1.2+, size cap, timeout).

---

## Public API Changes (illustrative — finalise during review)

New, all additive in `pkg/setup`:

```go
// Compile-time defaults (gochecknoglobals-exempt, as the Phase 2 ones).
var DefaultRekorVerification = ""          // "", "rekor", or "rekor-offline"
var DefaultCosignExpectedIssuer = ""
var DefaultCosignExpectedIdentity = ""
var MaxCosignBundleSize int64 = 64 << 10

// SignatureVerifier + CompositeVerifier (see Design).
// cosignVerifier implementing SignatureVerifier.

// WithSignatureVerifier appends an additional verifier (e.g. cosign)
// alongside the default GPG path. Additive to the existing
// WithKeyResolver / WithEmbeddedKeys options.
func WithSignatureVerifier(v SignatureVerifier) UpdaterOption

// WithCosignVerification is a convenience option building a
// cosignVerifier from a CosignVerifierConfig + embedded trust root.
func WithCosignVerification(cfg CosignVerifierConfig) UpdaterOption

// New sentinel errors:
var ErrAttestationMissing      = errors.New("integrity attestation not found in release")
var ErrCosignBundleInvalid     = errors.New("cosign bundle verification failed")
var ErrCosignIdentityMismatch  = errors.New("cosign cert identity does not match expected issuer/subject")
var ErrRekorEntryNotFound      = errors.New("no rekor transparency-log entry for artefact")
var ErrSigstoreTrustRootStale  = errors.New("embedded sigstore trust root is stale or invalid")
```

New config keys (the `update.sigstore_rekor_url` key was already reserved for
this phase in the Phase 2 spec):

```yaml
update:
  rekor_verification: ""            # "", "rekor", "rekor-offline"
  sigstore_rekor_url: ""           # override public-good Rekor
  sigstore_fulcio_url: ""          # override public-good Fulcio
  cosign_expected_issuer: ""       # required when rekor_verification set
  cosign_expected_identity: ""     # RE2 regex; required when set
  cosign_bundle_asset_name: ""     # override default bundle filename
```

Resolution precedence mirrors `require_signature` exactly: explicit config / env
→ compile-time `Default*`. Default everywhere is **off**, so no existing tool
changes behaviour.

`gtb sign` is **not** modified. (See [Q5](#open-questions) on whether a separate
`gtb attest`/`gtb cosign-sign` command is worth adding for parity, or whether the
`cosign` binary in CI is sufficient.)

---

## Resolved Decisions

1. **Additive, never a replacement.** The GPG/KMS detached signature stays the
   default and the sole offline-capable default path. Rekor is opt-in. Confirmed
   by the task framing and the Phase 2 deferral language.
2. **Tightly-pinned OIDC identity.** The `ExpectedIdentity` regex MUST pin the
   project path and tag-ref pattern (the same constraint the KMS role's trust
   policy already enforces:
   `project_path:phpboyscout/go-tool-base:ref_type:tag:ref:v*`). A loose matcher
   is a security defect, not a convenience. The regex is compiled via
   `pkg/regexutil.CompileBounded` because it originates from config.
3. **Keyless (ephemeral key + Fulcio), not cosign with a KMS key.** The whole
   point of Phase 3 is the *independent* transparency-log anchor and the
   no-long-lived-key posture. A cosign signature backed by the *same* AWS KMS key
   would add a log entry but not a third independent trust root, so it is not the
   target. (A KMS-backed cosign variant is possible later for tools that cannot
   use OIDC, but is out of scope.)
4. **Modern Sigstore bundle format** (`*.cosign.bundle`) so the inclusion proof
   travels with the artefact and offline verification is possible. Not the legacy
   `.sig` + `.crt` + `.bundle` triple.
5. **Default off, fail-open by default when on** — identical gating to Phase 2
   `require_signature`. A present-but-invalid bundle is **always fatal**; an
   absent bundle or unreachable Rekor/TUF is gated by a `require` flag. A Sigstore
   outage must not brick GPG-primary tools.
6. **No change to `gtb sign`.** OpenPGP-only per its spec; cosign is a distinct
   mechanism invoked by the `cosign` binary in CI.

---

## Open Questions

> Per project convention, resolve each with the user before implementation.

- **Q1 — Dependency isolation.** cosign/sigstore-go pull a large dependency tree
  (TUF, in-toto, protobuf-specs). Should the cosign verifier live in a **separate
  module / blank-import subpackage** (mirroring `pkg/credentials/keychain`, kept
  out of the default `cmd/gtb` binary by dead-code elimination) so regulated /
  minimal downstreams don't link it? Or is a build tag sufficient? Or accept it
  in the main module? This materially affects binary size and the FIPS/CGO-off
  build posture.
- **Q2 — Integration seam.** Option A (`SignatureVerifier` + `CompositeVerifier`,
  refactoring the GPG path behind it) vs Option B (`RekorResolver` under the
  existing `KeyResolver`). Recommendation: A. Confirm, since A touches the Phase 2
  `verifyManifestSignature` code path (behaviour-preserving refactor).
- **Q3 — Library choice.** `sigstore/sigstore-go` (the newer, bundle-native,
  higher-level verification API) vs `sigstore/cosign/v2` packages directly. The
  former is the better fit for offline bundle verification; confirm it builds
  under `CGO_ENABLED=0` + `GOLANG_FIPS=1` (the project's release build flags).
- **Q4 — Trust-root distribution & staleness.** Embed a TUF `trusted_root.json`
  snapshot via `//go:embed` and refresh per release (offline-capable), vs require
  online TUF refresh (smaller binary, no offline). What staleness window is
  acceptable before an embedded root is treated as stale (`ErrSigstoreTrustRootStale`)?
- **Q5 — Operator-facing `gtb verify` / `gtb attest`.** The sign-command spec
  deferred `gtb verify` ("add when there's a concrete asker"). Does the Rekor path
  create that asker — i.e. should there be a `gtb verify --rekor` for humans to
  audit a downloaded release, and/or a `gtb cosign-sign` wrapper for symmetry? Or
  is the in-binary self-update verifier + the `cosign` CLI enough?
- **Q6 — Cross-check semantics.** When a tool enables *both* GPG and Rekor with
  the strict cross-check, what is the exact failure matrix? (E.g. GPG valid +
  Rekor bundle absent + `require_rekor=false` → proceed on GPG with a warning?
  GPG valid + Rekor *invalid* → always fatal?) Specify the full truth table,
  consistent with the Phase 2 three-case policy in `verifyManifestSignature`.
- **Q7 — GitLab CI cosign OIDC.** Confirm `cosign sign-blob --oidc-provider`
  works with GitLab CI id_tokens in the goreleaser component's job image, and
  whether the `aud` for the Sigstore id_token is `sigstore` (Sigstore's
  convention) — cross-check against the AWS-audience pitfall already documented
  for this project (`aud: sts.amazonaws.com`, not `https://gitlab.com`).
- **Q8 — Monitoring.** A transparency log is only valuable if someone watches it.
  Out of scope to *build* a monitor, but should the spec mandate documenting a
  `rekor-monitor` / `cosign verify` runbook so a rogue entry under the project
  identity is actually noticed? (Recommend: yes, a docs deliverable.)

---

## Testing Strategy (outline — expand after open questions resolve)

- **Unit (`pkg/setup`):** table-driven, `t.Parallel()`, `logger.NewNoop()`.
  Verify a valid bundle (golden fixture produced by a real keyless sign in a
  one-off, committed as testdata), identity-mismatch → `ErrCosignIdentityMismatch`,
  tampered manifest → `ErrCosignBundleInvalid`, absent bundle → `ErrAttestationMissing`
  gated by the require flag, stale trust root → `ErrSigstoreTrustRootStale`.
  Offline-mode test verifies the embedded inclusion proof with **no** network
  (no `httptest.Server` reachability) to prove the air-gapped guarantee.
- **CompositeVerifier matrix:** every cell of the Q6 truth table.
- **Integration (`INT_TEST_SIGSTORE=1`, desktop-gated):** a live keyless sign +
  Rekor entry + online verification round-trip. Network + a real OIDC token, so
  gated per `2026-06-20-desktop-gated-integration-tests.md` — not on the homelab
  CI.
- **E2E (Godog):** an `@cli @update` scenario where the self-update is required to
  verify a Rekor bundle and aborts on a tampered manifest, using a fixture bundle
  via the injectable release source
  (`2026-06-19-injectable-release-source.md`).
- **Coverage:** ≥90% for new `pkg/` code, per project policy.

## Documentation

- New `docs/components/` section (or extend the setup / update integrity docs)
  describing the three-anchor model and the opt-in cosign path.
- A `docs/how-to/` runbook: enabling Rekor verification in a downstream tool,
  pinning the OIDC identity, refreshing the embedded trust root, and monitoring
  the log for rogue entries (Q8).
- Update the Phase 2 spec's "Future Considerations" to point here, and flip its
  "what remains unsolved" simultaneous-VCS+WKD row to "addressed by Phase 3 when
  Rekor cross-check is required."
- Migration note in `docs/migration/` if the Option-A refactor changes any
  exported Phase 2 signature (pre-1.0 minor-bump break, per the API-stability
  policy).

## Backwards Compatibility

Fully additive. Default `update.rekor_verification` is empty → no behaviour
change for any existing tool, no new dependency in the default build path (modulo
Q1's isolation decision), and `UpdateFromFile` is unchanged unless a tool opts
into `rekor-offline` *and* a bundle sidecar is present. The Option-A refactor of
the GPG path must be behaviour-preserving and is covered by the existing Phase 2
test suite.
