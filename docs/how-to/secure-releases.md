---
title: Secure Releases — Checksum Verification
description: How to publish and consume cryptographically-verifiable releases so self-updates reject tampered binaries. Phase 1 covers same-origin SHA-256 checksum verification; Phase 2 (GPG signature verification) is a planned extension.
tags: [how-to, update, security, checksum, releases]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Secure Releases — Checksum Verification

GTB's self-update flow verifies every downloaded binary against a GoReleaser-produced `checksums.txt` manifest before installing it. A tampered or truncated binary is rejected; a passing check is logged at INFO (`"checksum verified"`) and the update proceeds.

This is **Phase 1** of the release-integrity work from [`2026-04-02-remote-update-checksum-verification`](../development/specs/2026-04-02-remote-update-checksum-verification.md). **Phase 2** adds a GPG signature over the manifest, closing the same-origin trust gap (an attacker who can replace the binary on the release platform can also replace `checksums.txt` — only a signature from an off-platform key defeats that). Phase 2's code is implemented and dormant; see [Phase 2 below](#phase-2-gpg-signed-manifests).

## How it fits together

```
Update() →  findReleaseAsset()           = target binary
         →  fetchChecksumsManifest()     = checksums.txt (via ChecksumProvider or asset list)
         →  VerifyChecksumFromManifest() = binary SHA-256 vs manifest entry
         →  extract()                    = only reached when verify succeeds
```

`checksums.txt` is GoReleaser's default manifest — one `<hex-sha256>  <filename>` entry per line. If your `.goreleaser.yaml` uses the defaults, no changes are needed; the file is already attached to every release.

## Producing verifiable releases

### GoReleaser (recommended)

The default GoReleaser `checksum` block generates `checksums.txt` and attaches it to the release. No configuration change is required. Verify locally with:

```bash
just snapshot
ls dist/checksums.txt
```

### Manual / CI pipelines

If you don't use GoReleaser, produce the manifest with standard `sha256sum` output and upload it alongside the binaries. The file format is:

```
<64-hex-hash>  <filename>
<64-hex-hash>  <filename>
```

Blank lines at end-of-file are tolerated; **every other line must match** or the whole manifest is rejected (a truncated manifest never produces a false pass).

### Bitbucket

Upload `checksums.txt` to the repository's **Downloads** alongside the binaries (same upload flow as your release assets). The Bitbucket provider looks it up by exact filename — not via the asset-name regex that the binary uses.

### Direct HTTP releases

Set `checksum_url_template` in your `ReleaseSource.Params` to a URL template that expands to the manifest location:

```go
props.Tool.ReleaseSource = props.ReleaseSource{
    Type: "direct",
    Params: map[string]string{
        "url_template":          "https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}",
        "checksum_url_template": "https://releases.example.com/{tool}/{version}/checksums.txt",
    },
}
```

The same placeholders (`{version}`, `{version_bare}`, `{os}`, `{arch}`, `{tool}`, `{ext}`) are available.

## Consuming (tool author)

### Pick a failure mode

By default, a release without `checksums.txt` logs a warning and the update proceeds. This preserves backward compatibility for tools whose existing releases predate this feature. Once your tool has shipped at least one release with a manifest, flip the default to fail-closed:

```go
package main

import "gitlab.com/phpboyscout/go-tool-base/pkg/setup"

func main() {
    setup.DefaultRequireChecksum = true
    // ...
}
```

### Overriding at runtime

End users can override via config file:

```yaml
update:
  require_checksum: true          # abort if manifest missing or verification fails
  checksum_asset_name: ""         # override default "checksums.txt" filename
```

…or via env var (respects the tool's configured env prefix):

```bash
export MYTOOL_UPDATE_REQUIRE_CHECKSUM=true
```

Config wins over env var; env var wins over `setup.DefaultRequireChecksum`.

> **GTB itself** ships with `setup.DefaultRequireChecksum = true` — every `gtb update` verifies. Override with `GTB_UPDATE_REQUIRE_CHECKSUM=false` or `update.require_checksum: false` in config only if you need to update across a legacy release that predates the manifest (all GoReleaser-built releases have it, so this should rarely apply).

### Size bounds

The manifest download is capped at `setup.MaxChecksumsSize` (1 MiB); the binary download at `setup.MaxBinaryDownloadSize` (512 MiB). A hostile server streaming beyond those bounds aborts with `ErrChecksumTooLarge`. Tool authors who legitimately ship larger artefacts can reassign either variable before calling `Update()`:

```go
setup.MaxBinaryDownloadSize = 2 << 30 // 2 GiB
```

## Phase 2: GPG-signed manifests

Phase 1 defends against accidental corruption and single-asset tampering, but a full VCS compromise can replace both the binary and `checksums.txt` on the release. Phase 2 closes that gap by signing the manifest with a project-controlled GPG key — an attacker who replaces the files on the VCS still cannot produce a valid `checksums.txt.sig` without access to the private key.

> **Status**: the **verification side** (`TrustSet`, the `KeyResolver` chain, the `SelfUpdater` verify-before-parse gate) and the **build side** (a GoReleaser `signs` block + `scripts/sign-release.sh`) are **implemented**. They are **dormant by default**: signing only runs when a signing key is provisioned, and `setup.DefaultRequireSignature` stays `false` until the rollout completes. What remains is **operational provisioning** — generating the KMS-held key, publishing it via WKD, embedding the public key, and flipping the require-signature default — per the [Phase 2 Signing Prep](../development/phase2-signing-prep.md) checklist. See also the [Signature Verification component](../components/setup/signature-verification.md) for the full verifier API.

### Producing signed releases

GoReleaser signs `checksums.txt` via a `signs` block that shells out to `scripts/sign-release.sh`, producing an ASCII-armored detached `checksums.txt.sig` (the exact shape `TrustSet.VerifyManifestSignature` expects):

```yaml
# .goreleaser.yaml
signs:
  - id: checksums
    cmd: scripts/sign-release.sh
    artifacts: checksum
    signature: "${artifact}.sig"
    args: ["${artifact}", "${signature}"]
    output: true
```

`scripts/sign-release.sh` signs with whatever key gpg resolves for the `GTB_SIGNING_KEY` env var. Key custody is deliberately indirect so the same script works for local development (an ordinary gpg secret key) and production (a KMS/HSM-backed key exposed to gpg) — only the key source changes.

**Gating (dormant until provisioned).** The release job runs GoReleaser with `--skip=sign` unless `GTB_SIGNING_KEY` is set, mirroring the existing notarize gate on `APPLE_DEV_CERT`:

```yaml
# .gitlab-ci.yml (goreleaser job)
script:
  - |
    if [ -n "${GTB_SIGNING_KEY:-}" ]; then
      goreleaser release --clean
    else
      goreleaser release --clean --skip=sign
    fi
```

So until a signing key is configured in CI, releases ship unsigned exactly as before; once `GTB_SIGNING_KEY` is set, every release gains a `checksums.txt.sig`. The sign→verify loop is covered end-to-end by `TestSignReleaseScript_VerifiesViaTrustSet` (gated `INT_TEST_SIGNING=1`).

### Trust model at a glance

A signature is only as trustworthy as the key used to verify it. Phase 2 uses a **composite trust set**: the verifier loads public keys from two independent sources and requires their fingerprints to agree before accepting a signature.

```
┌─────────────────────┐      ┌──────────────────────────────┐
│  embedded in binary │      │   external: Web Key Directory │
│  (//go:embed)       │      │   or custom HTTPS endpoint    │
└──────────┬──────────┘      └──────────────┬───────────────┘
           │                                │
           └──────────►  CompositeResolver ◄┘
                             fingerprints must match
                                     │
                                     ▼
                              TrustSet ──► verify(checksums.txt.sig)
```

- **Embedded key** — baked into each binary at build time via `//go:embed`. Works offline and in air-gapped environments. Rotates only when a new binary is shipped.
- **External key (third-party source)** — fetched from an HTTPS endpoint under a domain you control. For a VCS compromise to produce a valid signature, the attacker must *also* control your DNS and TLS termination; the two trust anchors are administered independently. The canonical implementation is [Web Key Directory (WKD)](https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/), an OpenPGP RFC-draft serving public keys from a well-known path. Other HTTPS endpoints (self-hosted, Vault, a static S3 bucket) are supported via a custom `KeyResolver`.

### Resolver implementations

```go
// Interface — implement this to plug in any key source.
type KeyResolver interface {
    Resolve(ctx context.Context) (*TrustSet, error)
}
```

Three ship with GTB:

| Resolver | Source | Offline? | Primary use |
|----------|--------|----------|-------------|
| `setup.EmbeddedResolver` | `//go:embed` of `*.asc` files in `internal/version/trustkeys/` | ✅ Yes | Always available; the fallback that keeps air-gapped updates working. |
| `setup.WKDResolver` | `https://openpgpkey.<domain>/.well-known/openpgpkey/<domain>/hu/<z-base-32>?l=<email>` | ❌ No | The project's public key published via the GPG WKD standard; cross-checks the embedded copy. |
| `setup.CompositeResolver{Embedded, WKD}` | Both, with fingerprint-equality enforcement | ⚠️ Partial | The production default. Offline builds still work via `update.key_source=embedded`. |

### Configuration surface

```yaml
update:
  require_signature: false               # library default; flip on via DefaultRequireSignature
  key_source: both                       # "embedded" | "external" | "both"
  external_key_email: release@example.com  # drives the WKD URL
  require_external_crosscheck: false     # true → WKD failure aborts update
  signature_asset_name: ""               # override default "checksums.txt.sig"
```

Compile-time overrides (tool authors in `main`):

```go
setup.DefaultRequireSignature = true
setup.DefaultKeySource = "both"
setup.DefaultExternalKeyEmail = "release@example.com"
setup.DefaultRequireExternalCrosscheck = true
```

### Publishing a public key

1. **Generate** an Ed25519 signing keypair (RSA-4096 is acceptable if your KMS doesn't support Ed25519). DSA, 1024-bit RSA, and weak curves are refused at load time.
2. **Embed** the public half. Drop the ASCII-armored file at `internal/version/trustkeys/signing-key-v1.asc` in your repo — `go:embed` picks it up at build time. Tests gate a CI check that refuses any accidentally committed private key.
3. **Publish** the same key via your chosen external source:
   - **WKD** — serve the ASCII-armored key at the WKD path under `openpgpkey.<yourdomain>`. DNS and TLS cert are your trust anchors, administered independently from your VCS.
   - **Custom HTTPS** — implement `KeyResolver` with your own endpoint (Vault, static S3, internal CA-served HTTPS). Register it via `setup.WithKeyResolver` on `SelfUpdater`.
4. **Store** the private half in a KMS (AWS/GCP/Azure), Vault Transit, or a hardware token. GitHub encrypted secrets are a last resort — see the spec's Key Management section.

### Diagnosing live updates from logs

Every `gtb update` (or your tool's equivalent) emits structured log lines that name the concrete resolver used:

```
INFO update signature verification configured resolver=composite[embedded,wkd:openpgpkey.<yourdomain>]
INFO signature verified resolver=composite[embedded,wkd:openpgpkey.<yourdomain>]
```

The `resolver=` value is the most useful single field for support triage. `composite[embedded,wkd:…]` means both trust anchors were consulted and agreed; `embedded` or `wkd:…` alone means only one anchor was consulted (cryptographically sound but lower defence-in-depth). Full interpretation table — including failure-side log shapes for active-tampering signals — lives in the [Signature Verification component reference][svdocs].

[svdocs]: ../components/setup/signature-verification.md#interpreting-verifier-log-output

### Custom resolvers (third-party key source)

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/setup"

type VaultResolver struct { /* ... */ }

func (r *VaultResolver) Resolve(ctx context.Context) (*setup.TrustSet, error) {
    // Fetch ASCII-armored key from Vault KV, call setup.LoadTrustSet,
    // return the resulting TrustSet (enforces the minimum-strength policy).
}

func main() {
    resolver := setup.CompositeResolver{
        setup.EmbeddedResolver{},
        &VaultResolver{ /* ... */ },
    }
    // Wire it in at SelfUpdater construction:
    //   setup.NewUpdater(ctx, props, version, force, setup.WithKeyResolver(resolver))
}
```

Any implementation must:

- Return a `*TrustSet` containing only keys that passed the minimum-strength policy.
- Honour the context's deadline and cancellation.
- Cap response bodies at `setup.MaxWKDResponseSize` (64 KiB) or an equivalent bound.
- Not leak private material anywhere — `log.Fatal` if it ever sees a secret key at load time.

### Key rotation

The trust set is a *set*, not a single key. During a rotation window, ship releases signed by both the old and new key; the verifier accepts either. Once all supported versions of the tool include the new key in their trust set, drop the old key from both the embedded `trustkeys` directory and the WKD endpoint.

For emergency rotation (compromise of the primary signing key), the design reserves a second "rotation-authority" key whose private half is stored offline. A release signed by the rotation-authority carries a `rotate-keys.json` manifest; the next update rewrites the embedded trust set from that manifest. This is documented in the spec and deferred to Phase 4.

---

## Testing

Run the Phase 1 tests:

```bash
go test ./pkg/setup/ -run "TestVerifyChecksum|TestVerifyAssetChecksum|TestFindChecksumsAsset"
```

…and the manifest fuzzer:

```bash
go test ./pkg/setup/ -run "^$" -fuzz=FuzzParseChecksumManifest -fuzztime=30s
```

## Related

- [Setup Package Reference](../components/setup/index.md) — `VerifyChecksumFromManifest`, `VerifyChecksumFromManifestReader`, `DefaultRequireChecksum`.
- [VCS Release Providers](../components/vcs/release.md) — the `ChecksumProvider` optional interface and per-provider behaviour.
- [Custom Release Source](custom-release-source.md) — implementing a custom `release.Provider` (and optionally `release.ChecksumProvider`) for a proprietary release backend.
- [Credential Storage Hardening Spec](../development/specs/2026-04-02-credential-storage-hardening.md) — the related defence-in-depth spec that covers credential storage during update and setup.
- [Remote Update Integrity Spec](../development/specs/2026-04-02-remote-update-checksum-verification.md) — the full design including Phase 2 (GPG) and Phase 3 (cosign).
