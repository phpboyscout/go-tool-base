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

2. **Signature verification scope (Phase 2)**: **Cosign only**, using Sigstore keyless verification. Rationale:
   - Sigstore's public good instance removes key distribution entirely — the verifier trusts the OIDC identity recorded in Rekor (e.g. the GitHub Actions workflow that signed the release), not a long-lived private key.
   - Key rotation is a non-issue with keyless signing.
   - GPG's model (long-lived keys, trust on first use or manual distribution) does not improve upon `checksums.txt` in a meaningful way for most downstream tools.
   - Teams that require GPG can add it in a future phase; the architecture keeps signature verification pluggable.

3. **Key distribution for signature verification**: **Not needed for cosign keyless.** The verifier uses the Sigstore public good instance by default. A config key `update.sigstore_rekor_url` allows pointing at a self-hosted Rekor instance for air-gapped environments. No build-time key embedding.

4. **Bitbucket special handling**: **Best-effort same-origin checksum by name.** The Bitbucket provider's `DownloadReleaseAsset` is extended to look up `checksums.txt` by exact filename in the downloads list, matching the behaviour of other providers. If the release author did not upload `checksums.txt`, the usual fail-open / fail-closed config applies. Phase 1 does not special-case Bitbucket.

5. **Constant-time comparison**: Use `crypto/subtle.ConstantTimeCompare` on the hex-decoded hash bytes (32 bytes for SHA-256). This decision is not load-bearing for security in this spec's threat model, but follows Go crypto-library convention and makes future code audits simpler (every hash comparison is constant-time).

6. **Size bounds**: `MaxChecksumsSize = 1 MiB`, `MaxBinaryDownloadSize = 512 MiB`. Both configurable for downstream tools with exceptional requirements, via package-level variables `setup.MaxChecksumsSize` and `setup.MaxBinaryDownloadSize`. A binary over 512 MiB is almost certainly not a CLI tool; setting a high ceiling protects against hostile servers without restricting legitimate use.

7. **`ChecksumProvider` as optional interface**: Kept as an optional interface on `pkg/vcs/release` rather than a required method on `Provider`. Rationale: existing third-party implementations of `Provider` would otherwise need to be updated to add a method they have no meaningful implementation for. The optional-interface pattern is standard in Go (`io.WriterTo`, `io.StringWriter`) and the update flow handles both interface-implementing and non-implementing providers cleanly.

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
  require_checksum: false  # default value varies; see DefaultRequireChecksum
  sigstore_rekor_url: ""   # reserved for Phase 2 (cosign verification)
```

Resolution order for `require_checksum`:

1. Config value at `update.require_checksum` if set (via `cfg.Has`).
2. Environment variable `<TOOL>_UPDATE_REQUIRE_CHECKSUM` (via the config env-prefix mechanism).
3. The compile-time default `setup.DefaultRequireChecksum`.

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
