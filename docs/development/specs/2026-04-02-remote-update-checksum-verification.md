---
title: "Remote Update Integrity: Checksums + GPG Signatures"
description: "Verify remote self-update downloads against a signed checksums manifest. Phase 1 adds SHA-256 verification of checksums.txt (same-origin). Phase 2 adds GPG signature verification of the manifest, providing cryptographic provenance that survives a VCS platform compromise. Complements existing Apple notarization (macOS-only Gatekeeper approval) with cross-platform cryptographic integrity."
date: 2026-04-02
status: DRAFT
tags:
  - specification
  - setup
  - security
  - update
  - checksum
  - gpg
  - signing
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Remote Update Integrity: Checksums + GPG Signatures

Authors
:   Matt Cockayne

Date
:   02 April 2026

Status
:   DRAFT

---

## Overview

A security audit identified that the self-update mechanism in `pkg/setup` downloads release binaries from VCS providers (GitHub, GitLab, Gitea, Bitbucket, Direct) without verifying their integrity beyond HTTPS transport security. The `UpdateFromFile()` path already supports SHA-256 checksum verification via a `.sha256` sidecar file and the `VerifyChecksum()` function in `pkg/setup/checksum.go`, but the remote download path (`Update()`) performs no post-download integrity check.

This specification delivers integrity verification in two coupled phases:

**Phase 1 — Checksum verification (same-origin):**
1. Download the GoReleaser-generated `checksums.txt` file from the same release.
2. Extract the expected SHA-256 hash for the target asset from that file.
3. Verify the downloaded binary data against the expected hash before extraction.
4. Provide clear, actionable error messages on mismatch.
5. Define `update.require_checksum` and `setup.DefaultRequireChecksum` to enforce or relax verification behaviour.

**Phase 2 — GPG signature verification (cryptographic provenance):**
6. Sign `checksums.txt` with a project-controlled GPG key at release time (GoReleaser `signs` block).
7. Download the detached signature `checksums.txt.sig` alongside the manifest.
8. Verify the signature against a public key **embedded in the binary at build time** (`//go:embed`) before parsing the manifest.
9. Define `update.require_signature` and `setup.DefaultRequireSignature` with the same tool-author-opt-in pattern as Phase 1.
10. Document a key rotation plan (dual-sign during overlap; emergency rotation via a second embedded revocation-authority key).

The two phases are interdependent: GPG signing protects the integrity of `checksums.txt`, which in turn protects the integrity of every listed binary. Together they defeat the same-origin trust problem that Phase 1 alone cannot.

### Relationship to Existing Signing: Apple Notarization

GTB releases already go through Apple's notarization pipeline for macOS binaries. **Apple notarization is not a substitute for GPG signing and does not overlap with this work**:

| Property | Apple Notarization | GPG Signing |
|----------|--------------------|-------------|
| Platforms | macOS only | All platforms (linux, windows, darwin) |
| Trust anchor | Apple's notary service via Developer ID cert | Project-controlled GPG key, embedded in the binary |
| What it verifies | macOS Gatekeeper: binary is not known malware; is signed by a registered developer | Cryptographic provenance: binary chain of custody from signing key to user |
| Threat addressed | User runs malware obtained from untrusted source | VCS platform compromise, CDN poisoning, release asset tamper |
| Runtime check | Performed by macOS before execution (pre-install) | Performed by GTB before replacing the binary (in-place update) |

Both are retained. Apple notarization is Gatekeeper-facing; GPG signing is GTB-update-facing. Windows code signing (Authenticode) is out of scope for this spec — it is a separate feature that could complement this work in the future.

### Trust Model Without Signing (Phase 1 only)

Checksums hosted alongside binaries on the same VCS provider do not protect against a VCS platform compromise or a MITM attack at the TLS termination point. If an attacker can replace the binary, they can also replace `checksums.txt`. This is a fundamental limitation of same-origin integrity verification.

Same-origin checksums still provide value against:

- **Accidental corruption**: network errors, truncated downloads, CDN cache poisoning that affects only some assets.
- **Partial compromise**: an attacker who gains write access to a single release asset but not all assets (e.g., via a compromised CI job that uploads one artifact).
- **Replay/substitution attacks**: serving a legitimate older binary in place of the expected version, detectable when the checksum does not match.

They **do not** protect against:

- **Full VCS platform compromise**: attacker gains write access to all release assets and replaces both the binary and `checksums.txt`.
- **CI/CD pipeline compromise affecting the release job**: a malicious workflow can publish arbitrary binaries with matching `checksums.txt`.
- **Malicious release author**: an insider with release-publish permission.

### Trust Model With GPG Signing (Phase 2)

GPG signing of `checksums.txt` with a key held **outside the VCS provider** closes the same-origin gap. The trust model depends critically on **where the public key lives** and **where the private key lives**.

#### Residual Threat: Single-Source Public Key

If the public key used for verification is embedded in the binary AND the source tree is in the same VCS as the release assets, a full VCS compromise can poison both: the attacker replaces binaries, replaces the embedded key in source, and ships a new release that existing users' binaries still trust (because their embedded key was the real one) but that new users (installing fresh from the compromised VCS) would trust with the attacker's key. The compromise window shrinks for existing users but not for fresh installs.

**Defence: diffuse the trust anchor**. Publishing the public key at an **independent** service — one whose compromise is uncorrelated with a VCS compromise — forces the attacker to compromise two systems simultaneously. Concretely, GTB publishes its public key via **Web Key Directory (WKD)** at `https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/<z-base-32>` (the GPG standard, RFC-proposed) and GTB binaries are built to **cross-check** the embedded key against the WKD-served key on every update attempt.

Trust diffusion in practice:

| Attacker capability | Outcome |
|---------------------|---------|
| Controls VCS only | Can replace binaries and embedded key. WKD cross-check during update detects mismatch → update aborts. |
| Controls WKD endpoint only (DNS hijack, TLS MITM against the domain) | Cannot replace binaries. Cross-check fails → update aborts with a clear alarm. |
| Controls both VCS and WKD endpoint | Full compromise. This now requires breaching two independent systems during the same window. |

Coverage is not absolute. A determined adversary who compromises both systems still wins. The objective is **not** invulnerability but **cost**: raising the attacker's bar from "breach one system" to "breach two independent systems within a detection window."

#### Per-attacker-capability coverage

- **VCS platform compromise**: attacker can substitute binaries AND `checksums.txt`, but cannot produce a valid `checksums.txt.sig` without the signing key → signature verification fails. **Additionally**, if the attacker replaces the embedded public key in source, the WKD cross-check during update catches the mismatch.
- **CI/CD pipeline compromise**: depends on how the signing key is provisioned:
  - If the private key is a plain GitHub Actions secret, a compromised workflow can sign arbitrary content (equivalent to VCS compromise).
  - If the private key lives in an external KMS (AWS KMS, GCP Cloud KMS, HashiCorp Vault) and is accessed via OIDC with tightly-scoped permissions, the blast radius is smaller: an attacker needs both the workflow runner AND the KMS policy grant.
  - **Recommendation**: use an external KMS. Documented in [Key Management](#key-management) below.
- **Malicious release author**: still possible — anyone who can trigger the release workflow can sign. This is the residual threat and must be addressed operationally (branch protection, required reviews, signed commits).
- **WKD endpoint compromise in isolation**: detected by embedded-vs-WKD mismatch; update aborts.
- **DNS or TLS compromise against the WKD domain**: equivalent to WKD endpoint compromise for single-origin attacks; detected by cross-check.

#### What remains unsolved

- **Simultaneous compromise of VCS and WKD endpoint**: the attacker controls both sources of truth; no cross-check can detect this. Mitigation requires a third independent trust root (Sigstore Rekor transparency log, Phase 3).
- **Compromise of the build machine before signing**: a malicious Go compiler, dependency, or action can embed malware in the binary before it's signed. Mitigation: SLSA provenance, reproducible builds (Phase 5+).
- **First install from a poisoned VCS**: if a user's very first `curl … | bash` install happens during a VCS compromise window AND the WKD endpoint is not reachable or not cross-checked at install time, the attacker wins. **This spec's install-script updates (see [Install-Time Verification](#install-time-verification)) mitigate this by performing the WKD cross-check *before* the binary is installed, not just on subsequent updates.**
- **`go install` from a poisoned VCS**: we cannot modify the `go install` path. Documented as a known limitation with clear manual-verification instructions.

These residual threats are addressable via Phase 3 (cosign keyless with Rekor transparency log, providing an audit trail of every signed artefact tied to an OIDC identity), Phase 4 (emergency key rotation), and Phase 5 (SLSA build provenance). All are deferred to separate specs.

---

## Design Decisions

**Reuse and extend `VerifyChecksum()`**: The existing function in `pkg/setup/checksum.go` reads a sidecar file from the filesystem and compares the first hash. For remote updates, we need a new companion function that accepts multi-entry `checksums.txt` content (GoReleaser format: one `<hash>  <filename>` per line) and looks up the hash by filename. The existing single-entry `VerifyChecksum()` remains unchanged in signature but its internal hash comparison is updated to use `crypto/subtle.ConstantTimeCompare` (see "Constant-Time Comparison" below).

**Download `checksums.txt` as a release asset**: GoReleaser produces a `checksums.txt` file and attaches it to the release as an asset. The update flow already enumerates release assets to find the platform binary; the same enumeration can locate `checksums.txt`. This avoids constructing URLs manually and works uniformly across all VCS providers that expose release assets.

**Fail-open by default with a loud warning (Phase 1)**: If `checksums.txt` is not found among release assets (e.g., the release predates this feature, or the release was created manually without GoReleaser), the update proceeds with a warning. This matches the existing `UpdateFromFile()` behaviour when no `.sha256` sidecar exists. A configuration key (`update.require_checksum`) allows operators to enforce strict mode where missing checksums abort the update. **Tool authors may override the default to fail-closed at compile time** via a package-level variable `setup.DefaultRequireChecksum` — security-critical tools can ship with strict mode enabled by default while GTB itself preserves backward compatibility.

**No new dependencies**: SHA-256 is in the standard library (`crypto/sha256`, `crypto/subtle`). The `checksums.txt` format is trivially parseable with `bufio.Scanner` and `strings.Fields()`. No new modules are needed.

**`checksums.txt` filename is configurable for the Direct provider**: The Direct provider already has a `checksum_url_template` param that is declared but not implemented. This specification activates that param and also adds a `checksum_asset_name` param for VCS providers that use a non-standard checksums filename.

**Constant-time comparison**: Hash comparison uses `crypto/subtle.ConstantTimeCompare` on the hex-decoded bytes of expected vs actual hashes. This is defence-in-depth — practical timing attacks on checksum comparison of unknown binary content are infeasible, but following crypto-comparison best practice eliminates the class of concern entirely and is near-zero cost.

**Streaming hash computation**: The binary is hashed while being downloaded (or immediately after download from an in-memory buffer, if the download layer returns a buffer), using `io.MultiWriter(destination, sha256.New())`. This avoids a second pass over multi-megabyte binary data. A hard cap on download size (`MaxBinaryDownloadSize = 512 MiB`) prevents DoS via oversized asset responses.

**Bounded `checksums.txt` size**: The manifest download is capped at `MaxChecksumsSize = 1 MiB` (a GoReleaser manifest for a typical multi-OS release is ~1 KiB; 1 MiB is 1000× headroom and still prevents a hostile server from streaming an unbounded response). `io.LimitReader` enforces the bound.

**Manifest format validation**: Each line must match `^[0-9a-fA-F]{64}\s+\S+$`. Lines that don't match are treated as errors, not silently skipped, to catch malformed or truncated manifests. Empty lines and blank lines at end-of-file are permitted.

---

## Resolved Decisions

1. **Strict mode default**: `update.require_checksum` **defaults to `false` at the library level** (fail-open) for backward compatibility — existing tools with releases predating this feature must continue to update. **Tool authors can override the default at compile time** by setting `setup.DefaultRequireChecksum = true` in their `main.go`, opting their binaries into fail-closed verification from day one. The GTB binary itself will ship with `DefaultRequireChecksum = true` once the first GTB release containing checksum manifests has been produced (Phase 1 +1 release, to avoid bricking the current release). End users can always override via config.

2. **Signature verification: GPG in Phase 2** (promoted from a future phase). Rationale for choosing GPG over cosign for Phase 2:
   - **Works offline and behind corporate firewalls** — no dependency on the Sigstore public good instance or a self-hosted Rekor. Many CLI tools are used in air-gapped or restricted-network environments where `update` must still work.
   - **Self-contained trust anchor** — the public key is embedded in the binary at build time. No external service to reach at verification time.
   - **Established tooling** — GoReleaser natively supports GPG signing via its `signs` block; downstream tools already building with GoReleaser gain signing support with a small config addition.
   - **Cross-platform** — works identically on linux, darwin, windows, freebsd, with no platform-specific build variant.
   - **Complements Apple notarization cleanly** — see the [Relationship to Existing Signing](#relationship-to-existing-signing-apple-notarization) section.

   Cosign keyless via Sigstore remains a viable addition for tools that need transparency-log auditability — retained as Phase 3.

3. **GPG library**: `github.com/ProtonMail/go-crypto/openpgp`. The stdlib `golang.org/x/crypto/openpgp` is deprecated and frozen. The ProtonMail fork is pure Go, actively maintained, FIPS-ready (used by ProtonMail itself), and has no CGO dependency. It supports Ed25519, RSA, and ECDSA keys.

4. **Signing subject and format**: detached ASCII-armored signature of `checksums.txt`, named `checksums.txt.sig`. Signing the manifest (not individual binaries) means one signature protects every asset via the hash chain and requires no change to the per-binary pipeline. Detached signatures keep the manifest human-readable and auditable.

5. **Key algorithm**: **Ed25519 preferred; RSA-4096 acceptable.** Ed25519 is faster, smaller (32-byte public key, 64-byte signature), and uses modern curve arithmetic. RSA-4096 is accepted because some KMS/HSM providers do not support Ed25519. DSA and 1024-bit RSA keys are **rejected** — the verifier enforces a minimum-strength policy at load time.

6. **Key distribution: hybrid (embed + external cross-check)** — the public key is both embedded in the binary AND published at an independent external location (Web Key Directory). Verification requires the two sources to agree. Rationale:
   - A single VCS compromise is insufficient — the attacker must also compromise the independent external service within the same window.
   - Embedded-only is vulnerable to VCS poisoning of both binaries and the embedded key simultaneously, especially for fresh installs where users have no pre-established trust anchor.
   - External-only breaks offline and air-gapped use cases; first install has no trust anchor at all until the external fetch completes.
   - Embedded + external: offline continuity is preserved (embedded path still works), and online installs get the diffusion benefit automatically.
   - Concretely: the embedded key lives in `internal/version/trustkeys/` via `//go:embed`; the external key is served via **Web Key Directory (WKD)**, a GPG RFC-proposed standard that serves public keys from a well-known path under an HTTPS domain controlled by the project.

7. **External key source: Web Key Directory (WKD).** Chosen over alternatives considered:
   - **WKD**: GPG standard ([draft-koch-openpgp-webkey-service](https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/)); served from a well-known HTTPS path (`/.well-known/openpgpkey/...`); trust anchors are the project domain's DNS and TLS certificate, both administered independently from GitHub. ✅ **Chosen for Phase 2.**
   - **Keybase**: in maintenance mode since Zoom acquisition (2020); not a GPG standard; uncertain future. ❌ Rejected.
   - **HKP keyservers (keys.openpgp.org, SKS)**: support partial key uploads; trust-on-first-upload issues; no strong identity binding. ❌ Not recommended as primary.
   - **DNS-based (DANE/OPENPGPKEY)**: strong with DNSSEC; weak DNSSEC adoption in practice; client support uneven. Noted as a complementary option in docs but not implemented in Phase 2.
   - **Self-hosted HTTPS URL (custom path)**: equivalent to WKD conceptually but non-standard. ❌ Rejected in favour of the standard.

8. **KeyResolver abstraction.** The `SelfUpdater` depends on a `KeyResolver` interface that returns a `TrustSet` for verification. Three implementations ship in Phase 2:
   - `EmbeddedResolver` — returns the keys baked into the binary via `//go:embed`. Always available.
   - `WKDResolver` — fetches a public key from a WKD URL over HTTPS with certificate validation.
   - `CompositeResolver` — wraps multiple resolvers and requires them to agree (fingerprint match) on the full set of keys. The production default for GTB is `CompositeResolver{Embedded, WKD}`.
   
   Downstream tools can supply their own `KeyResolver` via a new `SelfUpdater` option, allowing e.g. a DNS-based resolver, a self-hosted HTTPS endpoint, or Sigstore Rekor in Phase 3.

9. **Cross-check failure behaviour.** Three config states:
   - `update.key_source: embedded` — use only the embedded key; WKD not consulted. For air-gapped / offline-only tools. No cross-check protection.
   - `update.key_source: external` — use only WKD-fetched keys. Embedded key is ignored. For environments that want to enforce a single source-of-truth.
   - `update.key_source: both` (default) — `CompositeResolver`. Both sources consulted; fingerprints must match or update aborts.
   - `update.require_external_crosscheck: true` (implied by `key_source: external` or `both`) — if the WKD fetch fails (network error, DNS failure, TLS failure), the update aborts rather than silently falling back to embedded-only. Default `false` on `both` for UX (offline users still get updates); tool authors who enforce cross-check in production can set `setup.DefaultRequireExternalCrosscheck = true`.

10. **Key rotation plan (dual-sign overlap)**:
    - The release workflow can sign `checksums.txt` with **multiple keys**. During a rotation period, releases are signed with both the current and the new key.
    - The trust set (both embedded and WKD) contains multiple keys. Verification passes if any key in the trust set validates the signature.
    - WKD can serve multiple keys for the same email address (OpenPGP User ID); both are fetched and added to the resolved trust set.
    - Retirement: when all supported older versions of the tool have shipped with the new key, the old key is dropped from the trust set in a subsequent release AND removed from the WKD directory.
    - Emergency rotation: a second "rotation-authority" key is embedded alongside the signing key. A release signed by the rotation-authority key AND carrying a new-key announcement (`rotate-keys.json` manifest) causes the next update to update the embedded trust set. This is documented but not implemented in Phase 2 — see [Future Considerations](#future-considerations).

11. **Private key storage**: The private key MUST NOT be a plaintext GitHub Actions secret. Acceptable storage:
   - **AWS KMS / GCP Cloud KMS / Azure Key Vault** with OIDC federation: the workflow assumes a role to sign; the key never leaves the KMS.
   - **HashiCorp Vault with Transit secrets engine**.
   - **Physical hardware token (YubiKey)**: for teams that sign releases from a specific machine.
   - **As a last resort**: a GitHub encrypted secret with `environment` protection + required reviewers. Documented but not recommended.

   The GoReleaser signing command is configurable; a thin shim (`scripts/sign-release.sh`) abstracts the provider. The default shim in the skeleton template uses GPG with a passphrase-protected key on disk (for local development/testing); CI pipelines replace it with a KMS-backed variant.

12. **`require_signature` default**: same pattern as `require_checksum` — library default `false`, `setup.DefaultRequireSignature` compile-time override. GTB itself ships with `DefaultRequireSignature = true` once the first signed release is available.

13. **Bitbucket special handling**: **Best-effort same-origin checksum and signature by name.** The Bitbucket provider's `DownloadReleaseAsset` is extended to look up `checksums.txt` and `checksums.txt.sig` by exact filename in the downloads list, matching the behaviour of other providers. If the release author did not upload them, the usual fail-open / fail-closed config applies.

14. **Constant-time comparison**: Use `crypto/subtle.ConstantTimeCompare` on the hex-decoded hash bytes (32 bytes for SHA-256). This decision is not load-bearing for security in this spec's threat model, but follows Go crypto-library convention and makes future code audits simpler (every hash comparison is constant-time). GPG signature verification itself uses the library's own constant-time primitives. Key-fingerprint comparisons between resolvers use `subtle.ConstantTimeCompare` for the same reason.

15. **Size bounds**: `MaxChecksumsSize = 1 MiB`, `MaxBinaryDownloadSize = 512 MiB`, `MaxSignatureSize = 8 KiB`, `MaxWKDResponseSize = 64 KiB` (accommodates multiple keys per identity). All configurable for downstream tools with exceptional requirements, via package-level variables. A binary over 512 MiB is almost certainly not a CLI tool; a GPG detached signature is < 1 KiB; generous ceilings protect against hostile servers without restricting legitimate use.

16. **`ChecksumProvider` as optional interface**: Kept as an optional interface on `pkg/vcs/release` rather than a required method on `Provider`. Rationale: existing third-party implementations of `Provider` would otherwise need to be updated to add a method they have no meaningful implementation for. The optional-interface pattern is standard in Go (`io.WriterTo`, `io.StringWriter`) and the update flow handles both interface-implementing and non-implementing providers cleanly. The signature asset follows the same pattern.

17. **Signature verification ordering**: `checksums.txt.sig` is verified **before** `checksums.txt` is parsed. A malformed or mis-signed manifest must never reach the parser — unsigned content cannot be trusted to be well-formed, so constrain the parse to post-verification. This is the canonical "don't parse untrusted input" discipline. The key-resolver cross-check runs **before** signature verification — an unresolved trust set is the earliest failure point.

18. **Key minimum-strength policy**: At binary startup, the embedded public keys are loaded and inspected. Keys that fail the minimum-strength policy (RSA < 3072 bits, DSA any, 1024-bit, weak curves) cause `log.Fatal` at init — the binary refuses to start rather than silently downgrading verification. WKD-fetched keys are validated against the same policy at fetch time; weak WKD keys fail the update with `ErrWeakKey`, not the binary startup. This is fail-loud at whichever layer introduces the weak key.

19. **WKD request hygiene**: The WKD fetch uses the GTB-hardened HTTP client (`pkg/http.NewClient`), which enforces HTTPS (no plaintext), TLS 1.2+, certificate validation (no `InsecureSkipVerify`), a 10-second timeout, a `User-Agent` identifying the GTB version, and a response size limit of `MaxWKDResponseSize`. Redirects are permitted only when the redirect target is also HTTPS on the same host; cross-host redirects are rejected.

---

## Public API Changes

### New Functions in `pkg/setup/checksum.go`

```go
// MaxChecksumsSize is the maximum byte length of a downloaded checksums
// manifest. Tools with extraordinary release layouts can override this by
// reassigning the variable before calling Update.
var MaxChecksumsSize int64 = 1 << 20 // 1 MiB

// MaxBinaryDownloadSize is the maximum byte length of a downloaded binary
// asset. Configurable for tools that ship exceptionally large archives.
var MaxBinaryDownloadSize int64 = 512 << 20 // 512 MiB

// DefaultRequireChecksum controls whether checksum verification is
// required by default when no explicit config is set. Tool authors
// should set this to true in main() for security-critical tools.
var DefaultRequireChecksum = false

// VerifyChecksumFromManifest verifies data against a named entry in a
// GoReleaser-style checksums manifest. The manifest format is one
// "<hex-sha256-of-64-chars>  <filename>" entry per line, with optional
// blank lines at end-of-file.
//
// Comparison uses crypto/subtle.ConstantTimeCompare on decoded hash bytes.
// Returns nil if the checksum matches, ErrChecksumAssetNotFound if the
// filename is not listed, ErrChecksumManifestMalformed for invalid
// manifest syntax, or a mismatch error with a hint.
func VerifyChecksumFromManifest(manifest []byte, filename string, data []byte) error

// VerifyChecksumFromManifestReader is the streaming equivalent of
// VerifyChecksumFromManifest. It computes the SHA-256 of dataReader while
// copying into a destination (for update install paths), avoiding a
// second full pass over multi-megabyte binary data.
//
// The caller supplies the destination writer (typically a temp file).
// Returns the bytes copied on success, or an error on checksum failure.
func VerifyChecksumFromManifestReader(
    manifest []byte,
    filename string,
    dataReader io.Reader,
    dst io.Writer,
    maxBytes int64,
) (int64, error)
```

### New Sentinel Errors

```go
// ErrChecksumAssetNotFound is returned when the target filename is not
// listed in the checksums manifest.
var ErrChecksumAssetNotFound = errors.New("asset not found in checksums manifest")

// ErrChecksumManifestMalformed is returned when the checksums manifest
// does not conform to the expected GoReleaser format.
var ErrChecksumManifestMalformed = errors.New("checksums manifest is malformed")

// ErrChecksumTooLarge is returned when the checksums manifest or binary
// download exceeds the configured size limit.
var ErrChecksumTooLarge = errors.New("download exceeds maximum size")
```

### New Configuration Keys

```yaml
# In tool config (e.g., ~/.config/<tool>/config.yaml):
update:
  require_checksum: false    # Phase 1 default; see DefaultRequireChecksum
  require_signature: false   # Phase 2 default; see DefaultRequireSignature
  signature_asset_name: ""   # override for non-default "checksums.txt.sig"
  sigstore_rekor_url: ""     # Phase 3 only (cosign verification)
```

Resolution order for `require_checksum` and `require_signature` is identical:

1. Config value if set (via `cfg.Has`).
2. Environment variable `<TOOL>_UPDATE_REQUIRE_CHECKSUM` / `_SIGNATURE` (via the config env-prefix mechanism).
3. The compile-time default (`setup.DefaultRequireChecksum` / `DefaultRequireSignature`).

### New Functions and Types in `pkg/setup/signing.go` (Phase 2)

```go
// MaxSignatureSize caps the bytes read from a detached signature download.
// GPG detached signatures are typically 400–800 bytes; 8 KiB is generous.
var MaxSignatureSize int64 = 8 << 10

// MaxWKDResponseSize caps the bytes read from a WKD public-key fetch.
var MaxWKDResponseSize int64 = 64 << 10

// DefaultRequireSignature is the compile-time default for signature
// enforcement. Tool authors should set this to true in main() once the
// first signed release is available.
var DefaultRequireSignature = false

// DefaultKeySource is the compile-time default for the key-source mode.
// Accepted values: "embedded", "external", "both" (default).
var DefaultKeySource = "both"

// DefaultRequireExternalCrosscheck controls whether a failure to reach
// the external key resolver (WKD) aborts the update. Set to true in
// production environments where silent fallback to embedded-only is
// unacceptable.
var DefaultRequireExternalCrosscheck = false

// DefaultExternalKeyEmail is the email used to derive the WKD URL.
// Tool authors should set this in main() to their release email.
var DefaultExternalKeyEmail = "release@phpboyscout.uk"

// TrustSet is the collection of public keys that can validate update
// signatures. Constructed by a KeyResolver per update attempt.
type TrustSet struct {
    keys []*packet.PublicKey // from ProtonMail go-crypto
}

// Fingerprints returns the 40-char uppercase hex fingerprints of every
// key in the trust set, sorted. Used for cross-check equality.
func (t *TrustSet) Fingerprints() []string

// LoadTrustSet parses one or more ASCII-armored public keys and
// enforces the minimum-strength policy. Returns an error for any key
// below the strength threshold.
func LoadTrustSet(armoredKeys ...[]byte) (*TrustSet, error)

// VerifyManifestSignature verifies an ASCII-armored detached signature
// against the checksums manifest using any key in the trust set. Returns
// nil on the first successful verification, ErrSignatureInvalid on
// failure of all keys.
func (t *TrustSet) VerifyManifestSignature(manifest, signature []byte) error

// KeyResolver returns the TrustSet used to verify release signatures.
type KeyResolver interface {
    Name() string
    Resolve(ctx context.Context) (*TrustSet, error)
}

// NewEmbeddedResolver returns a KeyResolver that returns a TrustSet
// parsed from the provided ASCII-armored public key bytes.
func NewEmbeddedResolver(armoredKeys ...[]byte) KeyResolver

// NewWKDResolver returns a KeyResolver that fetches a public key from a
// Web Key Directory URL derived from the provided email.
func NewWKDResolver(email string, httpClient *http.Client) KeyResolver

// CompositeResolver combines multiple resolvers with fingerprint cross-check.
// RequireAll controls whether a single resolver failure aborts (true) or
// whether the composite returns the first-successful resolver's set (false,
// with a warning). Default is RequireAll=true for correctness.
type CompositeResolver struct {
    Resolvers  []KeyResolver
    RequireAll bool
}

func (c *CompositeResolver) Name() string
func (c *CompositeResolver) Resolve(ctx context.Context) (*TrustSet, error)

// SignatureAssetName returns the default filename "checksums.txt.sig"
// unless overridden by the tool's ReleaseSource.Params["signature_asset_name"].
func (s *SelfUpdater) SignatureAssetName() string

// WithKeyResolver is a SelfUpdater option that overrides the default
// KeyResolver (CompositeResolver{Embedded, WKD}).
func WithKeyResolver(r KeyResolver) SelfUpdaterOption
```

### New Sentinel Errors (Phase 2)

```go
// ErrSignatureInvalid is returned when no key in the trust set validates
// the detached signature over the checksums manifest.
var ErrSignatureInvalid = errors.New("signature verification failed")

// ErrSignatureMissing is returned when require_signature is true and no
// signature asset was found in the release.
var ErrSignatureMissing = errors.New("signature asset not found in release")

// ErrWeakKey is returned when a public key (embedded or fetched) fails
// the minimum-strength policy.
var ErrWeakKey = errors.New("public key fails minimum-strength policy")

// ErrSignatureTooLarge is returned when the signature download exceeds
// MaxSignatureSize.
var ErrSignatureTooLarge = errors.New("signature download exceeds maximum size")

// ErrKeyResolverMismatch is returned by CompositeResolver when the
// fingerprint sets returned by child resolvers do not match.
var ErrKeyResolverMismatch = errors.New("key resolvers returned mismatched trust sets")

// ErrKeyResolverUnavailable is returned when a key resolver cannot
// fetch its keys (network failure, DNS, TLS) and RequireAll/
// RequireExternalCrosscheck is true.
var ErrKeyResolverUnavailable = errors.New("key resolver unavailable")

// ErrWKDResponseTooLarge is returned when a WKD response exceeds
// MaxWKDResponseSize.
var ErrWKDResponseTooLarge = errors.New("WKD response exceeds maximum size")
```

### Extended `SelfUpdater` (Internal Change, No Signature Change)

The `Update()` method gains checksum verification internally. Its public signature does not change:

```go
func (s *SelfUpdater) Update(ctx context.Context) (string, error)
```

### Direct Provider: Activated `checksum_url_template`

The existing but unused `checksum_url_template` param in `DirectReleaseProvider` is activated. When set, the provider downloads the checksum file from the expanded URL and verifies the binary after download.

---

## Internal Implementation

### Phase 1: Same-Origin Checksum Verification

#### `pkg/setup/checksum.go` Additions

The existing `VerifyChecksum` is updated to use constant-time comparison; the new `VerifyChecksumFromManifest` is added alongside:

```go
import (
    "bufio"
    "bytes"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/hex"
    "io"
    "regexp"
    "strings"
    "github.com/cockroachdb/errors"
)

var checksumLinePattern = regexp.MustCompile(`^([0-9a-fA-F]{64})\s+(\S+)$`)

// parseChecksumManifest extracts a hash for the given filename.
// Validates every line against the expected format; rejects malformed input.
func parseChecksumManifest(manifest []byte, filename string) (expectedHex string, err error) {
    scanner := bufio.NewScanner(bytes.NewReader(manifest))
    scanner.Buffer(make([]byte, 0, 4096), 64*1024) // large-line safety
    var found string
    for scanner.Scan() {
        line := strings.TrimRight(scanner.Text(), "\r")
        if line == "" {
            continue
        }
        m := checksumLinePattern.FindStringSubmatch(line)
        if m == nil {
            return "", errors.WithHintf(ErrChecksumManifestMalformed,
                "unexpected line in manifest: %q", line)
        }
        if m[2] == filename {
            found = m[1]
        }
    }
    if err := scanner.Err(); err != nil {
        return "", errors.Wrap(err, "reading checksums manifest")
    }
    if found == "" {
        return "", errors.WithHintf(ErrChecksumAssetNotFound,
            "The file %q was not found in the checksums manifest. "+
            "The release may have been created without GoReleaser.", filename)
    }
    return found, nil
}

func compareChecksum(expectedHex, actualHex string) error {
    expected, err := hex.DecodeString(expectedHex)
    if err != nil {
        return errors.Wrap(ErrChecksumManifestMalformed, "expected hash is not valid hex")
    }
    actual, err := hex.DecodeString(actualHex)
    if err != nil {
        return errors.Wrap(err, "computed hash is not valid hex") // should be impossible
    }
    if subtle.ConstantTimeCompare(expected, actual) != 1 {
        // Log scheme/filename context only; never log the hashes at non-DEBUG levels
        // in production (they can appear in attacker-influenced log scraping).
        return errors.WithHint(
            errors.Newf("checksum mismatch: expected %s, got %s", expectedHex, actualHex),
            "The downloaded file may be corrupted or tampered with. "+
            "Try updating again or download manually from a trusted source.",
        )
    }
    return nil
}

func VerifyChecksumFromManifest(manifest []byte, filename string, data []byte) error {
    expected, err := parseChecksumManifest(manifest, filename)
    if err != nil {
        return err
    }
    actual := fmt.Sprintf("%x", sha256.Sum256(data))
    return compareChecksum(expected, actual)
}

// VerifyChecksumFromManifestReader streams data through a SHA-256 hasher
// and a destination writer simultaneously, enforcing a size cap.
func VerifyChecksumFromManifestReader(
    manifest []byte, filename string,
    dataReader io.Reader, dst io.Writer, maxBytes int64,
) (int64, error) {
    expected, err := parseChecksumManifest(manifest, filename)
    if err != nil {
        return 0, err
    }
    h := sha256.New()
    limited := io.LimitReader(dataReader, maxBytes+1)
    n, err := io.Copy(io.MultiWriter(dst, h), limited)
    if err != nil {
        return n, errors.Wrap(err, "reading asset")
    }
    if n > maxBytes {
        return n, errors.WithHintf(ErrChecksumTooLarge,
            "asset exceeds configured maximum of %d bytes", maxBytes)
    }
    actual := fmt.Sprintf("%x", h.Sum(nil))
    return n, compareChecksum(expected, actual)
}
```

The existing `VerifyChecksum` (single-entry sidecar path) is updated to call `compareChecksum` internally, gaining constant-time comparison without changing its signature.

#### `pkg/setup/update.go` Changes

The `Update()` method is modified to:

1. After calling `findReleaseAsset()` for the binary, also search for a checksums asset.
2. Download the checksums asset if found.
3. After downloading the binary asset, call `VerifyChecksumFromManifest()`.
4. On mismatch, return an error (abort the update).
5. On missing checksums asset, check `update.require_checksum` config: if true, abort; if false, log a warning and proceed.

New helper method on `SelfUpdater`:

```go
const defaultChecksumsAssetName = "checksums.txt"

func (s *SelfUpdater) findChecksumsAsset(rel release.Release) (release.ReleaseAsset, bool) {
    checksumName := s.getChecksumsAssetName()
    for _, asset := range rel.GetAssets() {
        if asset.GetName() == checksumName {
            return asset, true
        }
    }
    return nil, false
}

func (s *SelfUpdater) getChecksumsAssetName() string {
    // Allow override via ReleaseSource.Params for non-standard naming.
    if name, ok := s.Tool.ReleaseSource.Params["checksum_asset_name"]; ok && name != "" {
        return name
    }
    return defaultChecksumsAssetName
}
```

Updated flow within `Update()`:

```go
// 1. Locate and download the checksums manifest FIRST (before the binary).
//    If require_checksum is enabled and the manifest is missing, fail
//    before expending bandwidth on the binary.
checksumAsset, hasChecksum := s.findChecksumsAsset(latestVersion)
var manifestBytes []byte
if hasChecksum {
    limited := io.LimitReader(/*stream of checksumAsset*/, MaxChecksumsSize+1)
    manifestBytes, err = io.ReadAll(limited)
    if err != nil {
        return targetPath, errors.Wrap(err, "failed to download checksums manifest")
    }
    if int64(len(manifestBytes)) > MaxChecksumsSize {
        return targetPath, errors.Wrap(ErrChecksumTooLarge, "checksums manifest too large")
    }
} else if s.requireChecksum() {
    return targetPath, errors.WithHint(
        errors.New("checksums manifest not found in release assets"),
        "Set update.require_checksum to false to allow updates without checksum verification.",
    )
} else {
    s.logger.Warn("no checksums manifest found in release, skipping verification",
        "asset_name", s.getChecksumsAssetName())
}

// 2. Download and verify the binary. If manifest is present, stream-hash
//    during download. Otherwise, download as before.
if manifestBytes != nil {
    // Streaming verification: copy asset -> tempfile while hashing.
    tempFile, err := s.Fs.Create(targetPath + "_")
    if err != nil {
        return targetPath, err
    }
    defer func() {
        _ = tempFile.Close()
        _ = s.Fs.Remove(targetPath + "_")
    }()

    rc, redirectURL, err := s.releaseClient.DownloadReleaseAsset(
        ctx, owner, repo, asset)
    if err != nil {
        return targetPath, err
    }
    if rc != nil {
        defer func() { _ = rc.Close() }()
    }
    if redirectURL != "" {
        return targetPath, errors.Newf("redirected to %s", redirectURL)
    }

    _, err = VerifyChecksumFromManifestReader(
        manifestBytes, asset.GetName(), rc, tempFile, MaxBinaryDownloadSize)
    if err != nil {
        return targetPath, err
    }

    s.logger.Info("checksum verified", "asset", asset.GetName())
    // Proceed with extract from tempFile...
} else {
    // Legacy path: download without verification
    file, err := s.DownloadAsset(ctx, asset)
    if err != nil {
        return targetPath, err
    }
    return targetPath, s.extract(file, targetPath)
}
```

The flow prioritises the manifest download so strict-mode failures happen early. For `DefaultRequireChecksum = false` tools with no manifest present, the existing behaviour is preserved exactly.

#### Direct Provider: `checksum_url_template` Activation

In `pkg/vcs/direct/provider.go`, add a method to download and return checksum data:

```go
func (p *DirectReleaseProvider) downloadChecksum(ctx context.Context, version string) ([]byte, error) {
    if p.checksumURLTemplate == "" {
        return nil, nil // no checksum configured
    }
    url := p.expandTemplate(p.checksumURLTemplate, version)
    // ... HTTP GET, return body bytes
}
```

The `SelfUpdater` calls this for the direct provider path. Since the direct provider generates synthetic releases with synthetic assets, the checksums asset is not part of `GetAssets()`. Instead, the `SelfUpdater` checks whether the release provider implements an optional `ChecksumProvider` interface:

```go
// ChecksumProvider is an optional interface that release providers can
// implement to supply checksum data through a provider-specific mechanism
// rather than as a release asset.
type ChecksumProvider interface {
    DownloadChecksumManifest(ctx context.Context, version string) ([]byte, error)
}
```

### Per-Provider Behaviour

| Provider | Checksum Source | Notes |
|----------|----------------|-------|
| **GitHub** | `checksums.txt` release asset | GoReleaser attaches this automatically. Found via `GetAssets()`. |
| **GitLab** | `checksums.txt` release link | GitLab models assets as "release links". Same `GetAssets()` abstraction works. |
| **Gitea/Codeberg** | `checksums.txt` release asset | Gitea's releases API includes assets. Same pattern as GitHub. |
| **Bitbucket** | `checksums.txt` in Downloads list | Bitbucket has no releases; the checksums file must be uploaded alongside binaries. Found by name in the downloads list. |
| **Direct** | `checksum_url_template` param | Provider implements `ChecksumProvider` to fetch from the configured URL. |

### GoReleaser Integration — Phase 1 (Checksums)

GoReleaser generates `checksums.txt` by default with the following format:

```
<sha256-hex>  <archive-name>
```

Example:

```
a1b2c3d4...  gtb_Linux_x86_64.tar.gz
e5f6a7b8...  gtb_Darwin_arm64.tar.gz
f9e8d7c6...  gtb_Darwin_x86_64.tar.gz
b5a4c3d2...  gtb_Windows_x86_64.tar.gz
```

The current `.goreleaser.yaml` does not disable or customise the checksum configuration, so the default `checksums.txt` is already being produced and attached to every release. No changes to `.goreleaser.yaml` are required for Phase 1.

The GoReleaser `checksum` configuration block (if added in the future) can customise the algorithm, filename, and whether to generate per-file sidecar `.sha256` files. The current defaults (SHA-256, `checksums.txt`, no sidecars) are exactly what this specification consumes.

---

## GPG Signing Design (Phase 2)

### Threat Coverage

See [Trust Model With GPG Signing](#trust-model-with-gpg-signing-phase-2) for the full analysis. In brief, Phase 2 closes the same-origin trust gap: an attacker who replaces `checksums.txt` on the VCS cannot also produce a valid `checksums.txt.sig` without the signing key. The remaining residual risks (compromise of the build machine, compromise of the source tree holding the embedded public key) are operational concerns addressed by branch protection, required reviews, and (in future) reproducible builds.

### Signing Flow (Release-Time)

```
┌───────────────────────────────────────────────────────────────┐
│                      Release workflow                         │
│                                                               │
│  goreleaser build → checksums.txt                             │
│           │                                                   │
│           ▼                                                   │
│  goreleaser signs block                                       │
│           │                                                   │
│           │  (invokes scripts/sign-release.sh)                │
│           ▼                                                   │
│  KMS / HSM / disk key signs checksums.txt                     │
│           │                                                   │
│           ▼                                                   │
│  checksums.txt.sig (detached ASCII-armored)                   │
│           │                                                   │
│           ▼                                                   │
│  Uploaded to release alongside binaries                       │
└───────────────────────────────────────────────────────────────┘
```

### Verification Flow (Client-Side Update)

```
┌───────────────────────────────────────────────────────────────┐
│                       SelfUpdater.Update                      │
│                                                               │
│  1. Download checksums.txt         (≤ MaxChecksumsSize)       │
│  2. Download checksums.txt.sig     (≤ MaxSignatureSize)       │
│     │                                                         │
│     ▼                                                         │
│  3. TrustSet.VerifyManifestSignature(manifest, signature)     │
│     │  — iterates embedded keys                               │
│     │  — fails if no key validates                            │
│     ▼                                                         │
│  4. parseChecksumManifest (only after signature verifies)     │
│     │                                                         │
│     ▼                                                         │
│  5. Download binary (streaming hash)                          │
│     ▼                                                         │
│  6. VerifyChecksumFromManifestReader                          │
│     ▼                                                         │
│  7. Atomic rename → install                                   │
└───────────────────────────────────────────────────────────────┘
```

The signature is verified **before the manifest is parsed**. Unsigned/invalid-signed manifests are never handed to the parser — the "don't parse untrusted input" discipline.

### Key Distribution and Resolution

The trust set used for signature verification is resolved through a pluggable `KeyResolver` abstraction. Phase 2 ships three implementations and defaults to a composite of "embedded + WKD".

```go
// KeyResolver returns the trust set used to verify release signatures.
// Implementations may read embedded data, fetch over the network, or
// combine multiple sources with cross-checks.
type KeyResolver interface {
    // Name returns a short identifier used in logs and diagnostics
    // (e.g. "embedded", "wkd:openpgpkey.example.com", "composite").
    Name() string

    // Resolve returns the trust set for the current update attempt.
    // Callers are responsible for caching where appropriate; Resolve
    // may perform I/O on every call.
    Resolve(ctx context.Context) (*TrustSet, error)
}
```

#### Implementations

**`EmbeddedResolver`** — returns the keys baked into the binary via `//go:embed`. Always available, no I/O, no failure modes beyond weak-key rejection at init.

**`WKDResolver`** — fetches a public key from a Web Key Directory URL. WKD is an OpenPGP draft standard that publishes keys at deterministic HTTPS paths derived from the local-part of an email address. For example, key for `release@phpboyscout.uk` lives at `https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/<z-base-32-hash-of-release>`. The resolver validates TLS, enforces the response size cap, applies the minimum-strength policy to fetched keys, and returns a `TrustSet`.

**`CompositeResolver`** — wraps an ordered list of resolvers and requires them to **agree** on the set of key fingerprints. If any resolver returns a set of fingerprints that does not exactly match every other resolver's set, the composite fails with `ErrKeyResolverMismatch`. The default GTB configuration is `CompositeResolver{Embedded, WKD}`. Downstream tool authors can wrap additional resolvers (e.g. DNS-based) by supplying a custom composite.

```go
// NewSelfUpdater option added in Phase 2:
func WithKeyResolver(r KeyResolver) SelfUpdaterOption

// Default when the option is not supplied:
func defaultKeyResolver(cfg config.Containable) KeyResolver {
    mode := cfg.GetString("update.key_source")
    switch mode {
    case "embedded":
        return trustkeys.EmbeddedResolver()
    case "external":
        return newWKDResolver(cfg)
    default: // "both" or unset
        return &CompositeResolver{
            Resolvers: []KeyResolver{
                trustkeys.EmbeddedResolver(),
                newWKDResolver(cfg),
            },
            // Failure to reach one resolver: fail-open by default,
            // fail-closed when update.require_external_crosscheck is true
            // or DefaultRequireExternalCrosscheck is set.
            RequireAll: requireExternalCrosscheck(cfg),
        }
    }
}
```

#### Embedded Key Layout

A new package `internal/version/trustkeys/` holds the project's public keys:

```
internal/version/trustkeys/
  trustkeys.go         # LoadEmbedded() → EmbeddedResolver
  signing-key-v1.asc   # ASCII-armored public key (embedded via //go:embed)
  rotation-key-v1.asc  # Reserved for key rotation — Phase 2 includes the
                       # structure; the rotation mechanism is Phase 4
  README.md            # Invariants and change-review process
```

```go
package trustkeys

import (
    _ "embed"

    "github.com/phpboyscout/go-tool-base/pkg/setup"
)

//go:embed signing-key-v1.asc
var signingKeyV1 []byte

// EmbeddedResolver returns a KeyResolver backed by the keys //go:embed'd
// in this package. Called once at SelfUpdater construction.
func EmbeddedResolver() setup.KeyResolver {
    return setup.NewEmbeddedResolver(signingKeyV1)
}
```

The `.asc` files are exported via `gpg --armor --export <KEYID>` and committed through normal PR review. The private key never touches the repository (enforced by a CI gate that fails if any file matching `*private*`, `*secret*`, `*.gpg` (binary), or `*.key` is present in `internal/version/trustkeys/`).

Minimum-strength policy (`LoadTrustSet`) is enforced at init time. A weak-key commit fails CI before the binary is built.

#### WKD Endpoint

GTB publishes its release public key at the following WKD URL:

```
Local-part of email:  release@phpboyscout.uk
WKD advanced URL:     https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/<z-base-32>?l=release
WKD direct URL:       https://phpboyscout.uk/.well-known/openpgpkey/hu/<z-base-32>?l=release
```

Where `<z-base-32>` is the z-base-32 encoding of the SHA-1 of the lower-cased local-part, per the WKD draft.

The advanced URL (dedicated `openpgpkey.` subdomain) is preferred and probed first; if it fails with any error (DNS, connect, 404, non-200), the direct URL is tried as a fallback. This matches GPG's own WKD client behaviour.

The WKD endpoint is served from a static HTTPS origin controlled by the project. The DNS record and TLS certificate are administered independently from GitHub — this is the crucial property that makes the cross-check meaningful.

#### Config Keys

```yaml
update:
  # Where to source public keys for signature verification.
  #   embedded — only keys baked into the binary.
  #   external — only WKD (requires external_key_url or default).
  #   both     — composite of embedded + external; cross-checked (default).
  key_source: both

  # Override the WKD email used to derive the URL. Defaults to the
  # project-default compiled into the binary at build time
  # (for GTB: "release@phpboyscout.uk").
  external_key_email: ""

  # Abort the update if the external resolver cannot be reached.
  # Defaults to false (graceful degradation: if WKD is unreachable,
  # fall back to embedded with a WARN log). Set true in locked-down
  # environments where silent fallback is unacceptable.
  require_external_crosscheck: false
```

The corresponding tool-author compile-time overrides:

```go
// Set in main.go by tool authors to change defaults for their binary:
setup.DefaultKeySource = "both"
setup.DefaultRequireExternalCrosscheck = false
setup.DefaultExternalKeyEmail = "release@yourtool.example.com"
```

### Key Management

The private signing key must be held in a way that resists both single-actor compromise and workflow compromise:

| Tier | Mechanism | Pros | Cons |
|------|-----------|------|------|
| **Recommended** | AWS KMS / GCP Cloud KMS with OIDC federation | Key never leaves KMS; access audited; rotation via KMS lifecycle | KMS cost (negligible); requires cloud account setup |
| **Good** | HashiCorp Vault Transit engine | Self-hostable; audited; pluggable | Requires Vault ops |
| **Acceptable** | YubiKey / hardware token + attended release | Strong hardware isolation | Requires human presence; impedes fully-automated releases |
| **Minimum** | GitHub environment secret + required reviewers | Native to GitHub Actions | Key material is in GitHub's infrastructure; trust model depends on GitHub |
| **Unacceptable** | Plain workflow secret without environment protection | Any workflow change could leak the key | — |

The default `scripts/sign-release.sh` in the skeleton template uses a GPG disk key (for local development). Downstream tools are expected to replace this with a KMS-backed variant for production releases. `docs/how-to/secure-releases.md` documents each tier with concrete setup instructions.

### Key Rotation

At any time the trust set may contain multiple keys. The release workflow can sign with multiple keys; the binary's verification passes if **any** key validates.

**Planned rotation (dual-sign overlap):**

1. Generate new key pair `signing-key-v2`.
2. Add `signing-key-v2.asc` to `internal/version/trustkeys/` in a PR alongside a commit that updates `LoadEmbedded()` to include it. Release this binary.
3. Update the release workflow to sign with both `signing-key-v1` and `signing-key-v2` (GoReleaser supports multiple `signs` entries).
4. Keep dual-signing for the supported-version window (e.g. 6 months for a yearly LTS cadence).
5. Remove `signing-key-v1.asc` from the trust set in a subsequent release. Update the workflow to sign only with v2.

**Emergency rotation (key compromise):**

Out of scope for Phase 2 implementation but designed for. The mechanism would use a second embedded "rotation-authority" key with a highly restricted private half. A signed `rotate-keys.json` manifest, distributed in a release, updates the local trust set on next update. This is deferred to [Future Considerations](#future-considerations).

### GoReleaser Configuration

```yaml
# .goreleaser.yaml additions (existing blocks kept):

signs:
  - id: gpg-sign-checksums
    cmd: bash
    args:
      - "scripts/sign-release.sh"
      - "${signature}"
      - "${artifact}"
    artifacts: checksum
    signature: "${artifact}.sig"

# scripts/sign-release.sh (default; replaced by CI with KMS variant):
#!/usr/bin/env bash
set -euo pipefail
signature=$1
artifact=$2
gpg --batch --yes --detach-sign --armor \
    --local-user "${GTB_SIGNING_KEY_ID}" \
    --output "$signature" \
    "$artifact"
```

The `GTB_SIGNING_KEY_ID` and GPG passphrase (or the KMS equivalent credentials) come from the workflow environment. For KMS variants, `scripts/sign-release.sh` is replaced with a shim that calls `aws kms sign` (or equivalent) and formats the output as a GPG-compatible ASCII-armored signature.

### Build-Time Reproducibility

The embedded public key is hashed into the binary. A reproducible build confirms that the public key in a downloaded binary matches the one in the source tree at the tagged commit. This is out of scope for Phase 2 implementation but the design does not preclude it:

- No time-dependent fields in the embedded key material (it's just the ASCII-armored PEM).
- No randomised build-time inputs introduced by this change.
- `go build -trimpath` already in use.

---

## Install-Time Verification

The recommended installation path for GTB (and for downstream tools generated by GTB) is the `install.sh` / `install.ps1` script downloaded from the repository. This path has the opportunity to verify the signature before the user ever runs the binary — **closing the "first install from a poisoned VCS" residual threat** that the update-path cross-check alone cannot address.

### Installation Paths in Scope

| Path | Verification story |
|------|---------------------|
| `curl … install.sh \| bash` | **Primary path.** Updated by this spec to perform full signature + checksum verification before installing. |
| `irm … install.ps1 \| iex` (Windows) | Updated in parallel with `install.sh`. |
| `brew install gtb` (Homebrew cask) | Updated GoReleaser config generates a cask that performs verification in a `preflight` block. |
| `go install github.com/phpboyscout/go-tool-base@latest` | **Not updatable.** The Go toolchain fetches source from the module proxy (which uses its own checksums via `sumdb`, providing weak integrity but no origin signing). Documented as a known limitation with explicit manual-verification instructions. |
| Manual download from GitHub Releases | Documented in `docs/how-to/verify-downloads-manually.md` with `gpg --verify` instructions. |
| Package managers (apt, yum, etc.) — future | Out of scope for this spec; when added, they have their own signing requirements. |

### `install.sh` (Linux/macOS) Updates

The existing `install.sh` currently downloads the binary and moves it into `$HOME/.local/bin`. It is updated to perform the following sequence **before** writing to the install path:

1. **Preflight**: check that `curl`, `gpg`, `sha256sum` (or `shasum -a 256` on macOS), `jq`, `tar` are available. If `gpg` is missing:
   - If `GTB_ALLOW_UNVERIFIED_INSTALL=1`, print a prominent warning and continue without signature verification.
   - Otherwise, exit with an error and link to install instructions for `gpg` on the current platform.
2. **Fetch WKD key** from `https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/<z-base-32>?l=release` with:
   - Connect timeout 10s, overall timeout 30s.
   - `curl --fail --location --retry 2`.
   - Response size cap: reject if `Content-Length` > 64 KiB or if received bytes exceed that.
   - Import into a **temporary, isolated GNUPGHOME** (`mktemp -d`) so the install does not touch the user's keyring. The temporary keyring is deleted on exit.
3. **Fetch `checksums.txt` and `checksums.txt.sig`** from the GitHub release for the target version.
4. **Verify the signature**: `gpg --homedir <tmp> --verify checksums.txt.sig checksums.txt`. Abort on non-zero exit.
5. **Fetch the binary archive** for the target platform.
6. **Verify the checksum**: `sha256sum --check --ignore-missing` (or `shasum -a 256 -c` on macOS, filtered to the target archive).
7. **Extract and install** the binary to `$HOME/.local/bin`.
8. **Log verification confirmation**: print the key fingerprint that validated the signature so the user can cross-reference against `SECURITY.md` or the project website.

Environment-variable controls:

```bash
# Override the WKD URL (for downstream tools or testing):
GTB_WKD_EMAIL="release@yourtool.example.com"

# Skip verification entirely (NOT RECOMMENDED; prints a warning and proceeds):
GTB_ALLOW_UNVERIFIED_INSTALL=1

# Pin a specific key fingerprint (defence against WKD compromise):
GTB_EXPECTED_KEY_FINGERPRINT="ABCD EF01 2345 6789 ABCD  EF01 2345 6789 ABCD EF01"
```

When `GTB_EXPECTED_KEY_FINGERPRINT` is set, the script verifies the WKD-fetched key matches the expected fingerprint before trusting it for signature verification. This is the strongest install-time guarantee (trust-on-first-install pinning).

### `install.ps1` (Windows) Updates

Mirrors `install.sh` using PowerShell-native tooling:

- `gpg` is assumed available (gpg4win or similar); the script provides clear installation guidance if missing.
- `Invoke-RestMethod` for HTTPS fetches with `-TimeoutSec` and response-size checks.
- Hashing uses `Get-FileHash -Algorithm SHA256`.
- The temporary GNUPGHOME is created via `New-Item -ItemType Directory -Path $env:TEMP/gtb-install-<rand>` and deleted in a `finally` block.
- Environment-variable controls mirror the bash version (`GTB_ALLOW_UNVERIFIED_INSTALL`, `GTB_EXPECTED_KEY_FINGERPRINT`, etc.).

### Homebrew Cask (macOS) Updates

GoReleaser generates `homebrew_casks` config in `.goreleaser.yaml`. The generated cask gains a `preflight` block that:

1. Downloads the `checksums.txt.sig` and the WKD-fetched key.
2. Verifies the signature using the user's existing `gpg` (Homebrew requires `gpg` as a dependency when the cask declares signature verification).
3. Aborts the install if verification fails.

Concretely, the GoReleaser template addition:

```yaml
homebrew_casks:
  - name: gtb
    binary: gtb
    conflicts:
      - formula: gtb
    preflight: |
      # Verify GPG signature of checksums before install.
      # See docs/how-to/secure-releases.md for the trust model.
      system_command "/usr/bin/gpg",
        args: ["--homedir", "#{staged_path}/.gnupg",
               "--verify", "#{staged_path}/checksums.txt.sig",
               "#{staged_path}/checksums.txt"]
```

The cask depends on `gpg` (added to the formula's `depends_on`). Users who already have `brew install gnupg` in their setup pay no additional cost.

### `go install` — Documented Limitation

`go install github.com/phpboyscout/go-tool-base@latest` fetches source via the Go module proxy, which uses its own checksum database (`sumdb`). This provides:

- **Good**: integrity against post-publish tampering of module content (once a module is in `sumdb`, its hash is fixed).
- **Missing**: origin signing. `sumdb` cannot verify that the module author is who they claim to be; it can only verify that the module content matches what it first saw.

The `go install` path is therefore **not** protected against:

- A one-time VCS compromise that poisons the source tree at tag time (`sumdb` will fix the bad hash as authoritative for that version).
- Malicious module proxy responses for unknown modules.

`docs/installation.md` is updated to:

1. Explicitly mark `curl … install.sh` as the recommended path with a "verified install" badge.
2. Keep `go install` as an option but prefix its section with a warning box explaining the trade-off and linking to `docs/how-to/verify-downloads-manually.md` for users who want to perform the equivalent verification manually after `go install`.
3. Document the command set for manual verification:

```bash
# After go install, manually verify the installed binary matches a signed release:
VERSION=$(gtb --version | awk '{print $NF}')
curl -fsSL "https://github.com/phpboyscout/go-tool-base/releases/download/${VERSION}/checksums.txt" -o /tmp/checksums.txt
curl -fsSL "https://github.com/phpboyscout/go-tool-base/releases/download/${VERSION}/checksums.txt.sig" -o /tmp/checksums.txt.sig
curl -fsSL "https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/..." | gpg --import
gpg --verify /tmp/checksums.txt.sig /tmp/checksums.txt
# Verify the go-installed binary hashes match what checksums.txt records for the gz archive —
# this is imperfect (the go-install path produces a different binary than the goreleaser archive),
# but the user can at least confirm that the SAME KEY has been signing releases for the version
# they have installed. Full equivalence requires reinstalling from the release archive.
```

### Security Properties of the Install Flow

- **Signature verified before binary is written to any install location**: the binary is downloaded to a temp directory, verified, then moved. A failed verification never leaves a partial binary.
- **Isolated keyring**: the script never imports keys into the user's default keyring. The temp `GNUPGHOME` is created per-invocation and removed on exit.
- **TLS hardened**: `curl --proto '=https' --tlsv1.2` enforced explicitly even though modern curls default to this.
- **No fallback to HTTP**: WKD and release URLs are HTTPS-only; any redirect to HTTP fails the fetch.
- **Size caps**: every fetched resource has a declared and enforced maximum size.
- **Idempotent**: re-running the script performs verification again; never trusts a previously-installed binary.

---

## Project Structure

### New Files (Phase 2)

| File | Purpose |
|------|---------|
| `pkg/setup/signing.go` | `TrustSet`, `LoadTrustSet`, `VerifyManifestSignature`, `KeyResolver` interface, Phase 2 errors and size constants |
| `pkg/setup/signing_embedded.go` | `EmbeddedResolver` helper (wraps byte slices into `KeyResolver`) |
| `pkg/setup/signing_wkd.go` | `WKDResolver`: URL derivation, HTTPS fetch, TLS-hardened, size-capped |
| `pkg/setup/signing_composite.go` | `CompositeResolver`: cross-check multiple resolvers |
| `pkg/setup/signing_test.go` | Unit tests for signature verification, weak-key policy, trust-set iteration |
| `pkg/setup/signing_wkd_test.go` | Unit + integration tests for WKD URL derivation, TLS, error handling |
| `pkg/setup/signing_composite_test.go` | Unit tests for cross-check pass/fail modes |
| `pkg/setup/signing_fuzz_test.go` | Fuzz test for signature parsing/verification |
| `internal/version/trustkeys/trustkeys.go` | `EmbeddedResolver()` returning the active embedded resolver |
| `internal/version/trustkeys/signing-key-v1.asc` | ASCII-armored public key (GTB project's own key; downstream tools replace) |
| `internal/version/trustkeys/README.md` | Documentation: invariants, change-review process, never-commit-private-key |
| `scripts/sign-release.sh` | Default GoReleaser signing shim (GPG disk key); template in generator |
| `scripts/sign-release-kms.sh.example` | Example KMS-backed shim for documentation |

### Modified Files (Phase 2 — install-time)

| File | Change |
|------|--------|
| `install.sh` | Full rewrite per [Install-Time Verification](#install-time-verification). Adds WKD fetch, GPG verify, checksum verify, expected-fingerprint pinning, `GTB_ALLOW_UNVERIFIED_INSTALL` escape hatch. |
| `install.ps1` | Parallel rewrite for Windows using PowerShell-native tooling. |
| `.goreleaser.yaml` | `homebrew_casks` block gains `preflight` signature verification; `depends_on gpg` added. |
| `docs/installation.md` | Reorganise: recommended (verified) paths first, `go install` marked with explicit trust-model warning linking to manual-verification instructions. |
| `docs/how-to/verify-downloads-manually.md` | New. Step-by-step manual verification for `go install` users and anyone who wants out-of-band verification. |

### Modified Files

| File | Change |
|------|--------|
| `pkg/setup/checksum.go` | Add `VerifyChecksumFromManifest()`, `VerifyChecksumFromManifestReader()`, `ErrChecksumAssetNotFound`, `ErrChecksumManifestMalformed`, `ErrChecksumTooLarge`, size-bound variables, `DefaultRequireChecksum` |
| `pkg/setup/checksum_test.go` | Add tests for manifest-based verification, size-bound, constant-time, fuzz corpus |
| `pkg/setup/update.go` | Add checksum + signature download and verification to `Update()` flow; signature verified before manifest parsed |
| `pkg/setup/update_test.go` | Add tests for full signed-update flow, key-rotation scenarios, strict-mode failure modes |
| `pkg/vcs/direct/provider.go` | Implement `ChecksumProvider` and `SignatureProvider`, activate `checksum_url_template` and new `signature_url_template` param |
| `pkg/vcs/direct/provider_test.go` | Add tests for checksum + signature URL download |
| `pkg/vcs/release/provider.go` | Add optional `ChecksumProvider` and `SignatureProvider` interfaces |
| `pkg/vcs/bitbucket/release.go` | Locate `checksums.txt` and `checksums.txt.sig` by name in downloads list |
| `.goreleaser.yaml` | Add `signs` block for GPG signature generation |
| `internal/generator/templates/skeleton-goreleaser.go` | Emit `signs` block in generated `.goreleaser.yaml`, gated by the tool author opting in during generate |
| `internal/generator/assets/skeleton/scripts/sign-release.sh` | Scaffold for generated tools |
| `go.mod` | Add `github.com/ProtonMail/go-crypto` dependency |

---

## Generator Impact

The generator templates in `internal/generator/` produce `.goreleaser.yaml` files for scaffolded tools. These already use GoReleaser defaults which include `checksums.txt` generation. No template changes are required.

Downstream tools built with GTB will automatically benefit from checksum verification in their self-update flow once they upgrade to a GTB version containing this feature, provided their releases include `checksums.txt` (which they do by default via GoReleaser).

---

## Error Handling

| Scenario | Behaviour | Error / Log |
|----------|-----------|-------------|
| `checksums.txt` found, hash matches | Update proceeds normally | `INFO: checksum verified` |
| `checksums.txt` found, hash mismatch | Update aborted | `checksum mismatch for <asset>: expected <x>, got <y>` with hint to re-download |
| `checksums.txt` found, target filename missing from manifest | Update aborted | `ErrChecksumAssetNotFound` with hint about GoReleaser |
| `checksums.txt` not in release, `require_checksum: false` | Update proceeds with warning | `WARN: no checksums manifest found in release, skipping verification` |
| `checksums.txt` not in release, `require_checksum: true` | Update aborted | `checksums manifest not found in release assets` with hint to disable requirement |
| `checksums.txt` download fails | Update aborted | `failed to download checksums manifest: <underlying error>` |
| Direct provider, `checksum_url_template` not set | Update proceeds with warning | Same as missing checksums above |
| Direct provider, checksum URL returns non-200 | Update aborted | `failed to download checksum: HTTP <status>` |

All errors use `cockroachdb/errors` with `WithHint()` for user-facing guidance.

---

## Testing Strategy

### Unit Tests

| Test | Package | Description |
|------|---------|-------------|
| `TestVerifyChecksumFromManifest_Match` | `setup` | Valid manifest, target file present, hash matches |
| `TestVerifyChecksumFromManifest_Mismatch` | `setup` | Valid manifest, target file present, hash does not match |
| `TestVerifyChecksumFromManifest_NotFound` | `setup` | Valid manifest, target filename not listed |
| `TestVerifyChecksumFromManifest_EmptyManifest` | `setup` | Empty or whitespace-only manifest → `ErrChecksumAssetNotFound` |
| `TestVerifyChecksumFromManifest_MultipleEntries` | `setup` | Manifest with many entries, correct one matched |
| `TestVerifyChecksumFromManifest_CaseInsensitiveHex` | `setup` | Uppercase vs lowercase hex hash both accepted (via `hex.DecodeString`) |
| `TestVerifyChecksumFromManifest_Malformed` | `setup` | Manifest with non-64-char hash, binary data, missing filename → `ErrChecksumManifestMalformed` |
| `TestVerifyChecksumFromManifest_LineTooLong` | `setup` | Single line exceeds scanner buffer → error, not panic |
| `TestVerifyChecksumFromManifest_DoubleSpaceSeparator` | `setup` | GoReleaser uses two spaces; also accept any whitespace |
| `TestVerifyChecksumFromManifest_CRLF` | `setup` | Manifest with Windows line endings |
| `TestVerifyChecksumFromManifestReader_Match` | `setup` | Streaming path with matching hash |
| `TestVerifyChecksumFromManifestReader_TooLarge` | `setup` | Oversized stream → `ErrChecksumTooLarge`; partial writes undone |
| `TestCompareChecksum_ConstantTime` | `setup` | Sanity check that `ConstantTimeCompare` is used (table-driven mismatch cases) |
| `TestUpdate_ChecksumVerified` | `setup` | Mock release with checksums asset, verify hash check runs |
| `TestUpdate_ChecksumMismatch_Aborts` | `setup` | Mock release returns bad checksum, update fails, no partial install |
| `TestUpdate_NoChecksum_WarnAndProceed` | `setup` | `require_checksum: false`, no manifest → success + warning |
| `TestUpdate_NoChecksum_RequireMode_Aborts` | `setup` | `require_checksum: true`, no manifest → abort before binary download |
| `TestUpdate_DefaultRequireChecksum_OverrideFromMain` | `setup` | `DefaultRequireChecksum = true`, no config → abort |
| `TestUpdate_ConfigOverridesDefault` | `setup` | Config `require_checksum: false` wins over `DefaultRequireChecksum = true` |
| `TestUpdate_ChecksumsDownloadFails` | `setup` | Manifest asset exists but download 500s → abort (never fall back to no-checksum) |
| `TestUpdate_OversizedManifest_Aborts` | `setup` | Manifest exceeds `MaxChecksumsSize` → abort before binary download |
| `TestUpdate_OversizedBinary_Aborts` | `setup` | Binary stream exceeds `MaxBinaryDownloadSize` → abort, temp file cleaned up |
| `TestDirectProvider_ChecksumURLTemplate` | `vcs/direct` | Checksum URL expanded and fetched correctly |
| `TestDirectProvider_ChecksumProvider_NotConfigured` | `vcs/direct` | Returns nil when no template set |
| `TestBitbucket_ChecksumAssetByName` | `vcs/bitbucket` | Bitbucket provider returns `checksums.txt` from downloads list when present |

### Fuzz Tests

```go
func FuzzParseChecksumManifest(f *testing.F) {
    f.Add([]byte("a1b2...c3d4  tool_linux.tar.gz\n"), "tool_linux.tar.gz")
    f.Fuzz(func(t *testing.T, manifest []byte, filename string) {
        // Property: never panics, always returns either a valid hex string
        // of length 64 or an error from the ErrChecksum* family.
        expected, err := parseChecksumManifest(manifest, filename)
        if err == nil {
            if len(expected) != 64 {
                t.Errorf("expected 64-char hex, got %d", len(expected))
            }
        }
    })
}
```

Fuzz is seeded with real GoReleaser manifests, malformed manifests, binary blobs, and corrupted UTF-8.

### Integration Tests

| Test | Tag | Description |
|------|-----|-------------|
| `TestUpdate_Checksum_GitHub_Integration` | `INT_TEST_VCS` | End-to-end update against a real GitHub release with checksums.txt |

### BDD Scenarios

Checksum verification is an internal security mechanism, not a user-facing workflow with multi-step interaction. BDD scenarios are not required for Phase 1. If Phase 2 adds a `--verify-signature` flag or interactive signature trust prompts, BDD scenarios should be added at that point.

---

## Migration & Compatibility

### Backward Compatibility

- **No breaking API changes**: `Update()` signature is unchanged. `VerifyChecksum()` is unchanged. The new `VerifyChecksumFromManifest()` is additive.
- **Fail-open default**: Existing releases without `checksums.txt` continue to work. The only visible change is a new warning log line.
- **Config key is optional**: `update.require_checksum` defaults to `false`. Tools that do not set it behave identically to today.

### Downstream Consumer Impact

Tools built on GTB that upgrade to this version will automatically gain checksum verification for their remote updates. No action required from downstream consumers unless they want to enable strict mode.

### API Stability

`VerifyChecksumFromManifest()` and `ErrChecksumAssetNotFound` are new public symbols in `pkg/setup`, which is a Stable-tier package. Once released, they are subject to the backward-compatibility guarantee. The function signature and error value must remain stable.

The optional `ChecksumProvider` interface in `pkg/vcs/release` is additive and does not affect existing `Provider` implementations. Providers are not required to implement it.

---

## Non-Functional Requirements

### Testing & Quality Gates

**Phase 1 — Checksum verification:**

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 90 % for `pkg/setup/checksum.go` (new and modified code) |
| Branch coverage | ≥ 90 % for the manifest parser and verification helpers |
| Race detector | `go test -race ./pkg/setup/...` and `./pkg/vcs/...` must pass |
| Fuzz testing | **Required**. `FuzzParseChecksumManifest` runs ≥ 60 s in CI; corpus seeded with real GoReleaser manifests and known-malformed inputs |
| Constant-time sanity | A dedicated test confirms `crypto/subtle.ConstantTimeCompare` is on the hash-comparison path (e.g. by injecting a known-length-difference mismatch and asserting the error does not surface timing-leaking values) |
| Size-bound assertions | Tests confirm `ErrChecksumTooLarge` fires at exactly `Max*+1` bytes for both checksums manifest and binary |
| Integration | `INT_TEST_VCS=1` must cover a GitHub end-to-end update against a real release with `checksums.txt` |
| Bitbucket coverage | Unit test for `BitbucketReleaseProvider` adds `checksums.txt` to the asset list when present in downloads |
| Golangci-lint | No new findings; no `//nolint` directives |
| Regression | Existing `TestUpdateFromFile_*` and `TestUpdate_*` must pass unchanged (fail-open default preserves legacy behaviour) |
| E2E / BDD | At least one Gherkin scenario in `features/cli/update.feature` covering strict-mode with a missing manifest (fails clearly) and successful verification |

**Phase 2 — GPG signature verification (core):**

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 90 % for `pkg/setup/signing*.go` and `internal/version/trustkeys/` |
| Branch coverage | ≥ 90 % for signature-verification paths including key-rotation scenarios |
| Fuzz testing | `FuzzVerifyManifestSignature` runs ≥ 60 s in CI; corpus seeded with valid signatures, bit-flipped signatures, truncated signatures, garbage, and signatures over different manifests |
| Key strength policy | Tests assert `LoadTrustSet` rejects RSA < 3072 bits, 1024-bit RSA, DSA, and weak-curve keys with `ErrWeakKey`; tests cover both embedded and WKD-fetched paths |
| Trust set iteration | Test with N keys; signature from any single key must validate; signature from none must fail cleanly without timing-leaking which key rejected |
| Key rotation | Test: two keys in trust set; a release signed by only the newer key validates; a release signed by only the older key validates; a release signed by neither fails |
| Tampering | Test: modify one byte of `checksums.txt` after signing → verification fails with `ErrSignatureInvalid` |
| Stripped signature | Test: `require_signature: true`, release with no `checksums.txt.sig` → abort with `ErrSignatureMissing` |
| Weak signing | Reject Ed25519 signatures with modified DER wrappers; reject non-detached signatures |
| Integration | `INT_TEST_VCS=1` covers a real-release signed-update round-trip (requires pre-set-up signing key in CI) |
| Build-time verification | CI step that verifies the embedded public key parses, meets strength policy, and produces a stable binary hash across repeated builds (reproducibility indicator) |
| Signing workflow | Release workflow includes a dry-run signing step on every PR to confirm the signing pipeline still works (without actually publishing) |
| Private-key CI gate | CI job fails if any file matching `*private*`, `*.gpg` (binary), `*secret*`, or `*.key` is committed under `internal/version/trustkeys/` |
| Golangci-lint | No new findings; `gosec` passes on the signing path |
| Regression | Phase 1 tests pass unchanged. A tool that does not opt in to `DefaultRequireSignature` continues to work as before. |

**Phase 2 — Key resolution (embedded + WKD cross-check):**

| Requirement | Target |
|-------------|--------|
| `EmbeddedResolver` coverage | ≥ 95 % — small, straightforward; aim for near-total coverage |
| `WKDResolver` coverage | ≥ 90 %, including URL-derivation for advanced and direct forms, TLS failure, DNS failure, non-200 response, oversized response, malformed key material |
| `CompositeResolver` coverage | ≥ 95 %: fingerprint-match pass, mismatch fail, child resolver unreachable (fail-open if `RequireAll=false`, fail-closed if `true`) |
| WKD URL derivation | Test the z-base-32 SHA-1 derivation against the reference examples in the WKD draft spec |
| WKD unit test | Use `httptest.Server` with self-signed cert + custom `http.Client` to exercise the full TLS path without external network |
| Cross-check mismatch | Test: embedded key fingerprint `A`, WKD key fingerprint `B` → `ErrKeyResolverMismatch` with hint text |
| Cross-check unavailable (fail-open) | Test: `RequireExternalCrosscheck=false`, WKD returns 500 → composite falls back to embedded-only with WARN log |
| Cross-check unavailable (fail-closed) | Test: `RequireExternalCrosscheck=true`, WKD returns 500 → `ErrKeyResolverUnavailable`, update aborts |
| `DefaultKeySource` override | Test: setting `embedded`/`external`/`both` picks the correct resolver chain |
| TLS hardening | Test: WKD endpoint advertising TLS 1.0 is rejected; plain-HTTP URL is rejected; cross-host redirect is rejected |
| Response size cap | Test: WKD response > `MaxWKDResponseSize` returns `ErrWKDResponseTooLarge` with no trust set returned |
| Integration | New `INT_TEST_VCS_WKD=1` gate that exercises a real WKD endpoint (can be a staging endpoint) |

**Phase 2 — Install-time verification:**

| Requirement | Target |
|-------------|--------|
| `install.sh` verification | Integration test: a CI job runs `bash install.sh` against a staging release with a known-good signature; verifies the binary is installed and the fingerprint is logged |
| `install.sh` tampering detection | Integration test: serve a release with a valid binary but a tampered `checksums.txt` → script exits non-zero before installing |
| `install.sh` WKD failure (GTB_ALLOW_UNVERIFIED_INSTALL=0) | Integration test: WKD endpoint returns 500 → script exits non-zero with clear error |
| `install.sh` unverified mode | Integration test: `GTB_ALLOW_UNVERIFIED_INSTALL=1` + missing `gpg` → script warns loudly and proceeds |
| `install.sh` fingerprint pinning | Integration test: `GTB_EXPECTED_KEY_FINGERPRINT` set to wrong fingerprint → script exits non-zero even if the signature would otherwise validate |
| `install.sh` temp cleanup | Test: script run with partial interruption leaves no files in `/tmp` (all tempdirs cleaned via trap) |
| `install.ps1` parity | PowerShell equivalent of all the above, run on a Windows CI runner |
| Homebrew cask | Generated cask has `preflight` block and `depends_on` for `gpg`; smoke-tested via `brew style` and `brew audit` in CI |
| ShellCheck | `install.sh` passes `shellcheck` with `-e SC2034` allowed for intentionally-unused vars |

**Phase 2 — BDD scenarios:**

| Scenario | Description |
|----------|-------------|
| `features/cli/update.feature` — happy path signed update | Valid signature + valid checksum → update succeeds; log contains key fingerprint |
| `features/cli/update.feature` — tampered manifest | Signed manifest mutated post-download → `ErrSignatureInvalid`; update aborts without overwriting binary |
| `features/cli/update.feature` — key mismatch | Embedded and WKD keys differ → `ErrKeyResolverMismatch`; hint text mentions both sources |
| `features/cli/update.feature` — WKD unavailable, fail-open | WKD returns 500, `require_external_crosscheck=false` → update succeeds with WARN |
| `features/cli/update.feature` — WKD unavailable, fail-closed | Same with `require_external_crosscheck=true` → update aborts |
| `features/cli/update.feature` — key rotation overlap | Trust set contains two keys; release signed by only the newer key; update succeeds |
| `features/cli/install.feature` — install.sh happy path | Full install succeeds; fingerprint logged |
| `features/cli/install.feature` — install.sh tampering | Install aborts; no binary is installed; tempdirs cleaned up |

### Documentation Deliverables

**Phase 1:**

| Artefact | Scope |
|----------|-------|
| `docs/components/setup.md` | Update. Add "Update integrity verification" section explaining the full flow, per-provider behaviour, and the `require_checksum` / `DefaultRequireChecksum` options. |
| `docs/about/security.md` | Update. Add "Update trust model" subsection covering same-origin checksum limitations and how Phase 2 addresses them. |
| `docs/migration/<version>-checksum-verification.md` | New. Explain the new warning log for releases without `checksums.txt`, the config keys, and how to opt into strict mode. |
| `docs/components/vcs/release.md` | Update. Document the optional `ChecksumProvider` interface and how the `Direct` provider activates `checksum_url_template`. |
| Package doc comment on `pkg/setup/checksum.go` | New top-of-file block explaining the two verification paths (sidecar vs manifest) and the constant-time invariant. |
| CLAUDE.md | Update. Mention under "Architecture / Version Control" that downstream tools should set `setup.DefaultRequireChecksum = true` for security-critical use cases. |
| BDD feature files | New scenarios in `features/cli/update.feature`. Living documentation for the update flow. |

**Phase 2:**

| Artefact | Scope |
|----------|-------|
| `docs/how-to/secure-releases.md` | **Comprehensive rewrite**. Covers: enabling `DefaultRequireChecksum` and `DefaultRequireSignature`; generating and managing a GPG key pair; WKD endpoint setup (DNS, TLS, file layout); storing the private key in AWS KMS, GCP Cloud KMS, Vault, or (least preferred) GitHub encrypted secrets; configuring the signing shim; key rotation procedure with the dual-sign overlap (both embedded and WKD updates); emergency rotation outline (Phase 4). |
| `docs/how-to/verify-downloads-manually.md` | New. Instructions for users to verify a release out-of-band — `gpg --verify checksums.txt.sig checksums.txt` — establishing trust before first install. Crucial for the bootstrap trust anchor. Includes the `go install` manual-verification workflow. |
| `docs/how-to/setup-wkd-endpoint.md` | New. Tool-author guide for publishing a public key via WKD: z-base-32 URL derivation, static file hosting, TLS requirements, multi-key layouts, key rotation via WKD. |
| `docs/installation.md` | Reorganise. "Verified install" (curl/iwr scripts) shown first as the recommended path; Homebrew second; `go install` last with a prominent trust-model warning and link to manual verification. |
| `docs/about/security.md` | Extend with "Cryptographic provenance of updates" subsection documenting the Phase 2 trust model, KMS recommendations, the hybrid key-distribution rationale, the install-script threat coverage, and the residual threats (build machine, simultaneous VCS+WKD compromise). Call out that `go install` is not covered by install-time verification. |
| `docs/components/setup/signing.md` | New. Reference doc for `TrustSet`, `VerifyManifestSignature`, `KeyResolver` (with all three implementations), the minimum-strength policy, and the embedded-key convention. |
| `docs/components/setup/key-resolution.md` | New. Deep-dive on `KeyResolver`: when to use each implementation, how to implement custom resolvers (e.g. DNS-based, cloud-KMS-backed), the cross-check semantics. |
| `internal/version/trustkeys/README.md` | New. In-tree documentation explaining what the `.asc` files are, how to regenerate them, the review process for changes, and the never-commit-private-key invariant. |
| `docs/migration/<version>-gpg-signing.md` | New. Documents the first signed release, how downstream tools add signing to their own pipeline, setting up a WKD endpoint for their own tool, updating their install scripts, and the flip of `DefaultRequireSignature = true`. |
| Package doc comment on `pkg/setup/signing.go` | New top-of-file block explaining trust set semantics, verification order (key-resolve → signature-verify → manifest-parse), the KeyResolver abstraction, and the ProtonMail go-crypto library choice. |
| Package doc comment on `pkg/setup/signing_wkd.go` | New block explaining WKD URL derivation and the TLS/hygiene controls. |
| CLAUDE.md | Update. Add "Release signing" section to Architecture with a pointer to `docs/how-to/secure-releases.md` and the critical invariants (private key never in source, never in plain workflow secrets, minimum strength enforced, WKD endpoint on a domain independent from the VCS). |
| `SECURITY.md` | Update. Add "Release integrity" section with the public key fingerprint, WKD URL, key-rotation announcement process, contact path for suspected signing-key or WKD-endpoint compromise. |
| Release notes template | Include the key fingerprint and signing-key version in every release note, so users have an audit trail in the release page itself. Also surface whether the release was verified against WKD at build time. |
| `install.sh` / `install.ps1` inline comments | Substantial top-of-file documentation explaining what the script does and how to audit it (since users paste it into `curl \| bash`). |
| BDD feature files | Extend `features/cli/update.feature` with Phase 2 update scenarios; new `features/cli/install.feature` for install-script scenarios. |

### Observability

**Phase 1 — Checksum verification:**

| Event | Level | Fields |
|-------|-------|--------|
| Manifest located | DEBUG | `asset_name`, `size` |
| Checksum verified | INFO | `asset_name`, `bytes` |
| Checksum mismatch | ERROR (fatal) | `asset_name`, `expected`, `got` — hashes are non-secret but must be truncated to first/last 8 chars in any surface accessible outside trusted telemetry |
| Missing manifest, fail-open | WARN | `asset_name`, `expected_name`; hint text guides user to enable strict mode |
| Missing manifest, fail-closed | ERROR (fatal) | `expected_name`; hint text explains how to disable strict mode if needed |
| Manifest download exceeded size | ERROR (fatal) | `limit_bytes`, `attempted_bytes` |
| Manifest malformed | ERROR (fatal) | `line_number`, `line_preview` (first 120 chars quoted) |
| Binary download exceeded size | ERROR (fatal) | `limit_bytes`, `written_bytes` (partial tempfile is deleted) |

**Phase 2 — GPG signature verification:**

| Event | Level | Fields |
|-------|-------|--------|
| Signature asset located | DEBUG | `asset_name`, `size` |
| Signature verified | INFO | `asset_name`, `key_fingerprint` (40-char hex) |
| Trust set loaded at startup | DEBUG | Array of `{fingerprint, algorithm, bits}` for each embedded key |
| Weak key in trust set | FATAL (at startup) | `fingerprint`, `reason` (algorithm / bit-length); binary refuses to start |
| Signature invalid | ERROR (fatal) | `asset_name`, list of `key_fingerprint`s tried (none matched); never includes the signature bytes |
| Signature missing, fail-open | WARN | `expected_name`; hint guides user to enable strict mode |
| Signature missing, fail-closed | ERROR (fatal) | `expected_name`; hint explains how to disable strict mode if needed |
| Signature download exceeded size | ERROR (fatal) | `limit_bytes`, `attempted_bytes` |
| Signature malformed | ERROR (fatal) | `asset_name`, `parse_error` |

**Redaction invariants**:
1. Hash bytes are non-secret and safe to log in full; truncate in user-visible error messages to prevent noise.
2. Signature bytes are non-secret but have no diagnostic value to log in full — log only the key fingerprint and the parse/verify outcome.
3. Public key fingerprints ARE logged — they identify which key was used and are the correct value for users to cross-reference against `SECURITY.md`.
4. Never log the private key material, which does not appear in the client at all — this is a runtime invariant, not a redaction concern.

### Performance Bounds

| Metric | Bound | Notes |
|--------|-------|-------|
| Manifest parse | O(n) in manifest size | Single regex per line, no backtracking; ≤ `MaxChecksumsSize` = 1 MiB enforced |
| Binary hash (streaming) | O(n) wall-clock; O(1) memory | `io.MultiWriter(destination, sha256.New())` with the default `io.Copy` 32 KiB buffer |
| Verification overhead on update | ≤ 10 % added wall-clock vs pre-change baseline for a 50 MiB binary | Verification is streaming, so overhead is dominated by SHA-256 throughput (~500 MiB/s modern CPU) |
| Memory footprint during verification | ≤ 2 MiB beyond the download pipeline | No buffering of full binary in RAM (that was the pre-change behaviour; removed) |
| Constant-time compare | Constant time on the 32-byte decoded hashes regardless of position of mismatch | via `crypto/subtle` |
| Signature verification (Phase 2) | ≤ 50 ms for a detached Ed25519 signature over a 1-KiB manifest | Ed25519 is fast; RSA-4096 verification is ~1 ms on modern hardware |
| Trust set load (Phase 2) | ≤ 10 ms per key at init | Parsing ASCII armor + key structure is fast |
| Trust set size | ≤ 16 keys in the embedded trust set | Practical limit; 2–4 is typical (current + overlap) |
| Signature download size cap | `MaxSignatureSize = 8 KiB` | GPG detached signatures are ~400–800 bytes; 8 KiB is 10× headroom |

### Security Invariants

Summarised from the [Resolved Decisions](#resolved-decisions) and threat model:

**Phase 1:**

1. Hash comparison is constant-time via `crypto/subtle.ConstantTimeCompare` on hex-decoded bytes.
2. Manifest downloads are bounded at `MaxChecksumsSize` (1 MiB default) via `io.LimitReader`.
3. Binary downloads are bounded at `MaxBinaryDownloadSize` (512 MiB default).
4. Manifest format is strictly validated — unknown/malformed lines fail the update rather than silently skip.
5. Strict mode (`require_checksum: true`) aborts on missing manifest; fail-open remains the library default for backward compatibility but tool authors can opt in to fail-closed at compile time via `setup.DefaultRequireChecksum = true`.
6. Partial temp files are always cleaned up on failure; the target binary is never overwritten until verification passes.
7. Manifest is downloaded and verified **before** the binary to fail fast in strict mode.

**Phase 2:**

8. The trust set is populated at package init from `//go:embed`ed public keys. Runtime addition of trust keys is not supported — the trust set is immutable for the lifetime of the binary.
9. `LoadTrustSet` enforces minimum-strength policy: Ed25519 accepted; RSA ≥ 3072 bits accepted; all else rejected with `ErrWeakKey` at startup (fail-loud).
10. Signature verification (`VerifyManifestSignature`) is performed **before** the manifest is parsed. Unsigned / invalid-signed content is never handed to the parser.
11. Signature download is bounded at `MaxSignatureSize` (8 KiB default).
12. Strict-signature mode (`require_signature: true` or `DefaultRequireSignature = true`) aborts on missing or invalid signature.
13. The private signing key is never in source; `internal/version/trustkeys/` contains only public keys. A CI gate fails the build if any file matching `*private*`, `*.gpg`, or `*.key` is present in the directory.
14. Key rotation is additive: new keys are added to the trust set in a release before the signing workflow switches. Old keys remain in the trust set until the rotation overlap window closes.
15. Hash and signature bytes are non-secret and may be logged; key fingerprints are logged at INFO; the signature bytes themselves have no diagnostic value and are logged only at DEBUG for troubleshooting.
16. Apple notarization (existing) and GPG signing (new) run independently on their respective platforms; a failure in one does not disable the other. Both must succeed for a release to publish.

---

---

## Future Considerations

### Phase 3: Cosign Keyless Verification (Future Specification)

Cosign keyless verification via Sigstore is a natural complement to GPG signing, not a replacement. It addresses threats that GPG alone does not:

- **Transparency log audit trail** — every signed artefact recorded in Rekor; anyone can detect unauthorised signing events.
- **No long-lived private key** — the signing identity is the OIDC identity of the CI workflow, not a stored key. Reduces the blast radius of a key compromise.

Trade-offs:
- Requires network access to Rekor at verification time (or a cached inclusion proof) — unsuitable for air-gapped environments that GPG already supports.
- Ties verification to Sigstore's availability and continued operation of the public good instance.

**Phase 3 design (deferred to a future spec)**:
- Add `cosign` verification of `checksums.txt.cosign.bundle` (cosign's bundle format includes signature, certificate, and Rekor inclusion proof).
- Verification precedence: GPG first (offline-capable); cosign as an additional check when `update.require_sigstore_verification` is true.
- GoReleaser config: add a second `signs` entry with `cmd: cosign`.
- Library: `github.com/sigstore/cosign/v2/pkg/cosign`.
- `update.sigstore_rekor_url` config key for self-hosted Rekor deployments.
- Document the OIDC identity that releases are signed with (e.g. `https://github.com/phpboyscout/go-tool-base/.github/workflows/release.yaml@refs/tags/v*`).

### Phase 4: Emergency Key Rotation Mechanism

Deferred. The design is sketched in [Resolved Decisions #7](#resolved-decisions):

- A second embedded "rotation-authority" key with a highly-restricted private half.
- A signed `rotate-keys.json` manifest distributed in a release.
- The local trust set is updated on next update when the manifest is validly signed by the rotation-authority key.
- Protects against "lost key" / "compromised key" events without forcing a coordinated user action.

### Phase 5: Build Provenance (SLSA)

Deferred. Adds a SLSA provenance attestation to each release (`<artifact>.intoto.jsonl`) describing the build environment, source commit, and workflow. Verification of provenance closes the build-machine-compromise residual threat. GitHub Actions has native SLSA v1.0 support via `slsa-framework/slsa-github-generator`.

### Phase 6: Checksum Pinning and Binary Transparency

Long-term consideration. Publishing release checksums to a public transparency log (Go sumdb model) so that tampering is publicly auditable. Combines with Phase 3 (Sigstore Rekor) for defence-in-depth.

---

## Implementation Phases

### Phase 1: Same-Origin Checksum Verification

| Step | Description | Effort |
|------|-------------|--------|
| 1 | Add `VerifyChecksumFromManifest()`, `VerifyChecksumFromManifestReader()`, `ErrChecksum*` sentinels, size-bound vars, `DefaultRequireChecksum` to `pkg/setup/checksum.go` | Small |
| 2 | Unit and fuzz tests for manifest parsing and verification | Small |
| 3 | Add `findChecksumsAsset()` helper to `SelfUpdater` | Small |
| 4 | Integrate checksum download and streaming verification into `Update()` | Medium |
| 5 | Add `update.require_checksum` config support and env-var resolution via the config env-prefix mechanism | Small |
| 6 | Activate `checksum_url_template` in `DirectReleaseProvider` | Medium |
| 7 | Add optional `ChecksumProvider` interface to `pkg/vcs/release` | Small |
| 8 | Bitbucket: locate `checksums.txt` by exact filename in downloads | Small |
| 9 | Integration and unit tests for the full update flow | Medium |
| 10 | Update `docs/components/setup.md` and `docs/components/vcs/release.md`; add `docs/how-to/secure-releases.md` (checksum portion) | Small |

Estimated total effort: **1–2 days**.

### Phase 2: GPG Signature Verification

Phase 2 is split into three sub-phases that ship in order. Phase 2a and 2b can merge separately; Phase 2c depends on 2a+2b.

**Phase 2a — Signature verification core:**

| Step | Description | Effort |
|------|-------------|--------|
| 1 | Add `github.com/ProtonMail/go-crypto` to `go.mod` | Trivial |
| 2 | Create `pkg/setup/signing.go`: `TrustSet`, `LoadTrustSet` with minimum-strength policy, `VerifyManifestSignature`, Phase 2 errors and size constants | Medium |
| 3 | Create `internal/version/trustkeys/trustkeys.go`, README, and commit the project's first public key (`signing-key-v1.asc`) with CI gate preventing private-key files | Small |
| 4 | Add `SignatureProvider` optional interface in `pkg/vcs/release` | Small |
| 5 | Add `scripts/sign-release.sh` (default GPG-disk-key variant) | Small |
| 6 | Update `.goreleaser.yaml` with `signs` block | Small |
| 7 | Set up the private signing key in a KMS (per [Key Management](#key-management)) — one-time operational step | Varies |

**Phase 2b — Key resolution (embedded + WKD cross-check):**

| Step | Description | Effort |
|------|-------------|--------|
| 8 | Add `KeyResolver` interface, `EmbeddedResolver`, `WKDResolver`, `CompositeResolver` to `pkg/setup/signing.go` and companion files | Medium |
| 9 | Stand up the WKD endpoint: DNS record for `openpgpkey.phpboyscout.uk`, TLS certificate, WKD-compliant static file layout | Medium (ops) |
| 10 | Wire `KeyResolver` into `SelfUpdater` at construction time with `WithKeyResolver` option; default to `CompositeResolver{Embedded, WKD}` | Small |
| 11 | Extend `Update()` flow: resolve trust set → download signature → verify against resolved trust set → parse manifest | Medium |
| 12 | Add config keys: `update.key_source`, `update.external_key_email`, `update.require_external_crosscheck`, `update.require_signature`; and matching `Default*` compile-time overrides | Small |
| 13 | Unit, fuzz, and integration tests for all three resolvers and for composite cross-check pass/fail scenarios (mismatch, unavailability) | Medium |
| 14 | Document KMS-backed signing variants, key-management tiers, WKD endpoint setup, and the trust model in `docs/how-to/secure-releases.md` | Medium |

**Phase 2c — Install-time verification:**

| Step | Description | Effort |
|------|-------------|--------|
| 15 | Rewrite `install.sh`: WKD fetch, GPG verify, checksum verify, expected-fingerprint pinning, `GTB_ALLOW_UNVERIFIED_INSTALL` escape hatch, isolated temp GNUPGHOME | Medium |
| 16 | Rewrite `install.ps1` with PowerShell equivalents | Medium |
| 17 | Update `.goreleaser.yaml` `homebrew_casks` block with `preflight` signature verification and `depends_on gpg` | Small |
| 18 | Rewrite `docs/installation.md`: recommended-first ordering, `go install` warning box, manual-verification link | Small |
| 19 | New `docs/how-to/verify-downloads-manually.md` with step-by-step out-of-band verification | Small |
| 20 | Integration tests: `install.sh` against a staging release; `install.ps1` on a Windows runner | Medium |

**Phase 2d — Generator & BDD:**

| Step | Description | Effort |
|------|-------------|--------|
| 21 | Generator: add `signs` block scaffolding to generated `.goreleaser.yaml` and the `sign-release.sh` shim; add `homebrew_casks` preflight scaffold; add WKD email prompt to wizard | Medium |
| 22 | Generator: scaffold `install.sh` and `install.ps1` for generated tools (parameterised by tool name, WKD email, expected-fingerprint placeholder) | Medium |
| 23 | BDD scenarios for signed-update happy path, tampering rejection, key-mismatch cross-check failure, WKD unavailability (fail-open and fail-closed modes) | Small |
| 24 | First signed release: after Phase 2 is shipped in a release, flip `setup.DefaultRequireSignature = true` in a follow-up release | — |

Estimated total effort: **5–8 days** of engineering work plus operational setup of the signing key in KMS and the WKD endpoint.

### Phase 3+ (Future)

Phase 3 (cosign) and beyond are deferred to follow-up specs. The architecture introduced in Phase 2 (`TrustSet`, `SignatureProvider` interface, trust-set-iteration verification) accommodates additional signature schemes without restructuring.
