---
title: "Remote Update Checksum Verification Specification"
description: "Add SHA-256 checksum verification to the remote self-update path, downloading and validating checksums.txt from VCS release assets before installing binaries. Addresses trust model limitations of same-origin checksums and defines a path toward cryptographic signature verification."
date: 2026-04-02
status: DRAFT
tags:
  - specification
  - setup
  - security
  - update
  - checksum
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Remote Update Checksum Verification Specification

Authors
:   Matt Cockayne

Date
:   02 April 2026

Status
:   DRAFT

---

## Overview

A security audit identified that the self-update mechanism in `pkg/setup` downloads release binaries from VCS providers (GitHub, GitLab, Gitea, Bitbucket, Direct) without verifying their integrity beyond HTTPS transport security. The `UpdateFromFile()` path already supports SHA-256 checksum verification via a `.sha256` sidecar file and the `VerifyChecksum()` function in `pkg/setup/checksum.go`, but the remote download path (`Update()`) performs no post-download integrity check.

This specification adds checksum verification to the remote update path by:

1. Downloading the GoReleaser-generated `checksums.txt` file from the same release.
2. Extracting the expected SHA-256 hash for the target asset from that file.
3. Verifying the downloaded binary data against the expected hash before extraction.
4. Providing clear, actionable error messages on checksum mismatch.
5. Defining a configuration option to enforce or relax verification behaviour.

### Trust Model Limitations

Checksums hosted alongside binaries on the same VCS provider do not protect against a VCS platform compromise or a MITM attack at the TLS termination point. If an attacker can replace the binary, they can also replace `checksums.txt`. This is a fundamental limitation of same-origin integrity verification.

Same-origin checksums still provide value against:

- **Accidental corruption**: network errors, truncated downloads, CDN cache poisoning that affects only some assets.
- **Partial compromise**: an attacker who gains write access to a single release asset but not all assets (e.g., via a compromised CI job that uploads one artifact).
- **Replay/substitution attacks**: serving a legitimate older binary in place of the expected version, detectable when the checksum does not match.

For defence-in-depth against full platform compromise, cryptographic signature verification (GPG or cosign) is required. This specification scopes signature verification as a future phase but defines the extension points needed to support it.

---

## Design Decisions

**Reuse and extend `VerifyChecksum()`**: The existing function in `pkg/setup/checksum.go` reads a sidecar file from the filesystem and compares the first hash. For remote updates, we need a new companion function that accepts multi-entry `checksums.txt` content (GoReleaser format: one `<hash>  <filename>` per line) and looks up the hash by filename. The existing single-entry `VerifyChecksum()` remains unchanged for the offline path.

**Download `checksums.txt` as a release asset**: GoReleaser produces a `checksums.txt` file and attaches it to the release as an asset. The update flow already enumerates release assets to find the platform binary; the same enumeration can locate `checksums.txt`. This avoids constructing URLs manually and works uniformly across all VCS providers that expose release assets.

**Fail-open by default with a loud warning (Phase 1)**: If `checksums.txt` is not found among release assets (e.g., the release predates this feature, or the release was created manually without GoReleaser), the update proceeds with a warning. This matches the existing `UpdateFromFile()` behaviour when no `.sha256` sidecar exists. A configuration key (`update.require_checksum`) allows operators to enforce strict mode where missing checksums abort the update.

**No new dependencies**: SHA-256 is in the standard library. The `checksums.txt` format is trivially parseable with `strings.Fields()` and a line scanner. No new modules are needed.

**`checksums.txt` filename is configurable for the Direct provider**: The Direct provider already has a `checksum_url_template` param that is declared but not implemented. This specification activates that param and also adds a `checksum_asset_name` param for VCS providers that use a non-standard checksums filename.

---

## Open Questions

1. **Strict mode default**: Should `update.require_checksum` default to `false` (fail-open, Phase 1 recommendation) or `true` (fail-closed)? Fail-open avoids breaking tools whose releases predate checksum generation, but reduces the security posture for new deployments.

2. **Signature verification scope (Phase 2)**: Should Phase 2 support GPG only, cosign only, or both? GoReleaser supports both via its `signs` and `docker_signs` configurations. Supporting both adds complexity; supporting only cosign is more forward-looking but excludes teams without Sigstore infrastructure.

3. **Key distribution for signature verification**: For GPG, should the public key be embedded in the binary at build time, fetched from a keyserver, or configured via a config file path? For cosign, should verification use the Sigstore public good instance (Rekor transparency log) or require a self-hosted instance?

4. **Bitbucket special handling**: Bitbucket Downloads has no native release concept and version detection is filename-based. Should the Bitbucket provider attempt to locate a `checksums.txt` file by name from the downloads list, or should checksum verification be unsupported for Bitbucket in Phase 1?

---

## Public API Changes

### New Function: `VerifyChecksumFromManifest`

```go
// VerifyChecksumFromManifest verifies data against a named entry in a
// GoReleaser-style checksums manifest. The manifest format is one
// "<hex-sha256>  <filename>" entry per line.
// Returns nil if the checksum matches, ErrChecksumAssetNotFound if the
// filename is not present in the manifest, or a mismatch error with a hint.
func VerifyChecksumFromManifest(manifest []byte, filename string, data []byte) error
```

### New Sentinel Error

```go
// ErrChecksumAssetNotFound is returned when the target filename is not
// listed in the checksums manifest.
var ErrChecksumAssetNotFound = errors.New("asset not found in checksums manifest")
```

### New Configuration Key

```yaml
# In tool config (e.g., ~/.config/<tool>/config.yaml):
update:
  require_checksum: false  # default; set to true to abort on missing checksums
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

Add `VerifyChecksumFromManifest()` alongside the existing `VerifyChecksum()`:

```go
func VerifyChecksumFromManifest(manifest []byte, filename string, data []byte) error {
    scanner := bufio.NewScanner(bytes.NewReader(manifest))
    for scanner.Scan() {
        fields := strings.Fields(scanner.Text())
        if len(fields) >= 2 && fields[1] == filename {
            expectedHash := fields[0]
            actualHash := fmt.Sprintf("%x", sha256.Sum256(data))
            if !strings.EqualFold(actualHash, expectedHash) {
                return errors.WithHint(
                    errors.Newf("checksum mismatch for %s: expected %s, got %s", filename, expectedHash, actualHash),
                    "The downloaded file may be corrupted or tampered with. Try updating again or download manually from a trusted source.",
                )
            }
            return nil
        }
    }
    return errors.WithHintf(ErrChecksumAssetNotFound,
        "The file %q was not found in the checksums manifest. The release may have been created without GoReleaser.", filename)
}
```

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
// After finding and downloading the binary asset...
checksumAsset, hasChecksum := s.findChecksumsAsset(latestVersion)
if hasChecksum {
    checksumData, err := s.DownloadAsset(ctx, checksumAsset)
    if err != nil {
        return targetPath, errors.Wrap(err, "failed to download checksums manifest")
    }
    if err := VerifyChecksumFromManifest(checksumData.Bytes(), asset.GetName(), file.Bytes()); err != nil {
        return targetPath, err
    }
    s.logger.Info("checksum verified", "asset", asset.GetName())
} else if s.requireChecksum() {
    return targetPath, errors.WithHint(
        errors.New("checksums manifest not found in release assets"),
        "Set update.require_checksum to false to allow updates without checksum verification.",
    )
} else {
    s.logger.Warn("no checksums manifest found in release, skipping verification")
}
```

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

### GoReleaser Integration

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

## Project Structure

### New Files

None. All changes go into existing files.

### Modified Files

| File | Change |
|------|--------|
| `pkg/setup/checksum.go` | Add `VerifyChecksumFromManifest()`, `ErrChecksumAssetNotFound` |
| `pkg/setup/checksum_test.go` | Add tests for manifest-based verification |
| `pkg/setup/update.go` | Add checksum download and verification to `Update()` flow |
| `pkg/setup/update_test.go` | Add tests for checksum verification in remote update path |
| `pkg/vcs/direct/provider.go` | Implement `ChecksumProvider` interface, activate `checksum_url_template` |
| `pkg/vcs/direct/provider_test.go` | Add tests for checksum URL download |
| `pkg/vcs/release/provider.go` | Add optional `ChecksumProvider` interface |

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
| `TestVerifyChecksumFromManifest_EmptyManifest` | `setup` | Empty or whitespace-only manifest |
| `TestVerifyChecksumFromManifest_MultipleEntries` | `setup` | Manifest with many entries, correct one matched |
| `TestVerifyChecksumFromManifest_CaseInsensitive` | `setup` | Uppercase vs lowercase hex hash |
| `TestUpdate_ChecksumVerified` | `setup` | Mock release with checksums asset, verify hash check runs |
| `TestUpdate_ChecksumMismatch_Aborts` | `setup` | Mock release returns bad checksum, update fails |
| `TestUpdate_NoChecksum_WarnAndProceed` | `setup` | Mock release without checksums asset, update succeeds with warning |
| `TestUpdate_NoChecksum_RequireMode_Aborts` | `setup` | `require_checksum: true`, no checksums asset, update fails |
| `TestDirectProvider_ChecksumURLTemplate` | `vcs/direct` | Checksum URL expanded and fetched correctly |
| `TestDirectProvider_ChecksumProvider_NotConfigured` | `vcs/direct` | Returns nil when no template set |

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

## Future Considerations

### Phase 2: Cryptographic Signature Verification

Same-origin checksums do not protect against a full VCS platform compromise. Phase 2 should add cryptographic signature verification as defence-in-depth:

**GoReleaser signing support**: GoReleaser can sign checksums.txt using GPG (`signs` block) or cosign (`docker_signs` / `signs` with cosign). The signed artifact is typically `checksums.txt.sig` (GPG) or `checksums.txt.sig` / `checksums.txt.bundle` (cosign).

**GPG verification path**:
- GoReleaser config addition: `signs: [{ artifacts: checksum }]`
- Public key distribution: embed in the binary via `//go:embed` at build time, or fetch from a keyserver/config path.
- Verification: `golang.org/x/crypto/openpgp` (or a maintained fork) to verify the detached signature.
- Trust model: binary trusts the key it was built with. Key rotation requires a new binary release signed with both old and new keys.

**Cosign verification path**:
- GoReleaser config addition: `signs: [{ cmd: cosign, artifacts: checksum }]`
- Verification: `github.com/sigstore/cosign/v2/pkg/cosign` to verify against the Sigstore public good instance (Rekor transparency log) or a self-hosted instance.
- Trust model: keyless verification via OIDC identity (e.g., GitHub Actions workflow identity), recorded in Rekor. No key distribution problem.
- Trade-off: requires network access to Rekor for verification (or a cached/bundled inclusion proof).

**Recommended approach for Phase 2**: Start with cosign keyless verification tied to GitHub Actions OIDC identity. This provides the strongest trust model (the CI/CD identity is verified by Sigstore, not just by the VCS platform) with minimal key management burden. GPG support can be added later for teams that cannot use Sigstore.

**`.goreleaser.yaml` changes for Phase 2**:
```yaml
signs:
  - cmd: cosign
    artifacts: checksum
    output: true
    certificate: '${artifact}.pem'
    args:
      - sign-blob
      - '--output-certificate=${certificate}'
      - '--output-signature=${signature}'
      - '${artifact}'
      - --yes
```

### Phase 3: Checksum Pinning and Transparency

For maximum security, a future phase could support:

- **Checksum pinning**: a local manifest of expected checksums per version, allowing verification even when the VCS provider is untrusted.
- **Binary transparency log**: publishing checksums to a transparency log (e.g., Go sumdb model) so that tampering is publicly auditable.

These are long-term considerations and do not affect the Phase 1 design.

---

## Implementation Phases

### Phase 1: Same-Origin Checksum Verification (This Specification)

| Step | Description | Effort |
|------|-------------|--------|
| 1 | Add `VerifyChecksumFromManifest()` and `ErrChecksumAssetNotFound` to `pkg/setup/checksum.go` | Small |
| 2 | Add unit tests for manifest-based verification | Small |
| 3 | Add `findChecksumsAsset()` helper to `SelfUpdater` | Small |
| 4 | Integrate checksum download and verification into `Update()` | Medium |
| 5 | Add `update.require_checksum` config support | Small |
| 6 | Activate `checksum_url_template` in `DirectReleaseProvider` | Medium |
| 7 | Add `ChecksumProvider` interface to `pkg/vcs/release` | Small |
| 8 | Add integration and unit tests for the full update flow | Medium |
| 9 | Update `docs/components/setup.md` and `docs/components/vcs/release.md` | Small |

Estimated total effort: **1-2 days**

### Phase 2: Cosign Signature Verification (Future Specification)

- Add `cosign` verification of `checksums.txt.sig` / `checksums.txt.bundle`.
- Add `.goreleaser.yaml` signing configuration.
- Add `update.require_signature` config key.
- Update generator templates to include signing config.
- Requires a separate specification.

### Phase 3: GPG Signature Verification (Future Specification)

- Add GPG detached signature verification.
- Add public key embedding or config-based key path.
- Requires a separate specification.
