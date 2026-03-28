---
title: "Offline Update Support Specification"
description: "Add an offline update mode to pkg/setup that accepts a local tarball path instead of downloading from a VCS release, enabling air-gapped and high-security environments."
date: 2026-03-26
status: IMPLEMENTED
tags:
  - specification
  - setup
  - update
  - offline
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Offline Update Support Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   26 March 2026

Status
:   DRAFT

---

## Overview

The `SelfUpdater` in `pkg/setup` always contacts the VCS API (GitHub or GitLab) to check for and download new releases. This is a hard requirement that blocks adoption in air-gapped environments, corporate networks with restricted egress, and high-security contexts where binaries must go through an internal approval pipeline before deployment.

This specification adds an offline update mode that accepts a local `.tar.gz` file path, verifies its integrity, and installs it using the existing `extract()` flow. The feature is exposed via a `--from-file` flag on the `update` command and a programmatic `UpdateFromFile` method on `SelfUpdater`.

---

## Design Decisions

**Reuse existing `extract()` flow**: The current `extract()` method already handles `.tar.gz` decompression, tar entry scanning, and binary installation with chunk-based copying (mitigating decompression bombs). The offline path reads the file into a `bytes.Buffer` and feeds it directly to `extract()`, avoiding code duplication.

**Checksum verification via sidecar file**: When a `.sha256` sidecar file exists alongside the tarball (e.g., `tool_Linux_x86_64.tar.gz.sha256`), it is automatically verified before extraction. This matches the GoReleaser default output format. Verification is mandatory when a sidecar is present and optional when absent (with a warning).

**No signature verification in Phase 1**: GPG/cosign signature verification is valuable but adds significant complexity (key distribution, trust chain). Phase 1 focuses on checksum verification; signature support is a future consideration.

**`--from-file` flag on update command**: The flag is mutually exclusive with `--version`. When provided, it skips all VCS API calls (version check, release listing, asset download). The `--force` flag is still respected to skip the "already up to date" check.

**Version extraction from filename**: The tarball filename is expected to follow GoReleaser conventions (`<tool>_<OS>_<arch>.tar.gz`). The version is not embedded in this name, so when updating from file the version check is skipped entirely. The installed binary reports its own version via ldflags.

---

## Public API Changes

### New Method on `SelfUpdater`

```go
// UpdateFromFile installs a binary from a local .tar.gz file.
// If a .sha256 sidecar file exists at filePath+".sha256", the checksum
// is verified before extraction. Returns the installation target path.
func (s *SelfUpdater) UpdateFromFile(filePath string) (string, error)
```

### New Function for Checksum Verification

```go
// VerifyChecksum reads a SHA-256 sidecar file and verifies it against
// the provided data. The sidecar format is "<hex-hash>  <filename>\n"
// (matching sha256sum output and GoReleaser checksums.txt entries).
// Returns nil if the checksum matches, or an error with a hint on mismatch.
func VerifyChecksum(fs afero.Fs, sidecarPath string, data []byte) error
```

### Update Command Flag

```go
// In pkg/cmd/update/update.go:
cmd.Flags().String("from-file", "", "path to a local .tar.gz release archive for offline installation")
```

### Usage Examples

```bash
# Standard offline update
mytool update --from-file /path/to/mytool_Linux_x86_64.tar.gz

# With sidecar checksum (auto-detected)
ls /path/to/
# mytool_Linux_x86_64.tar.gz
# mytool_Linux_x86_64.tar.gz.sha256
mytool update --from-file /path/to/mytool_Linux_x86_64.tar.gz

# Force install even if version appears current
mytool update --from-file /path/to/mytool_Linux_x86_64.tar.gz --force
```

---

## Internal Implementation

### `UpdateFromFile` Method

```go
func (s *SelfUpdater) UpdateFromFile(filePath string) (string, error) {
    targetPath, err := s.resolveTargetPath()
    if err != nil {
        return "", err
    }

    // Read the tarball
    data, err := afero.ReadFile(s.Fs, filePath)
    if err != nil {
        return "", errors.Wrap(err, "failed to read update file")
    }

    // Check for sidecar checksum
    sidecarPath := filePath + ".sha256"
    exists, _ := afero.Exists(s.Fs, sidecarPath)
    if exists {
        if err := VerifyChecksum(s.Fs, sidecarPath, data); err != nil {
            return "", err
        }
        s.logger.Info("checksum verified", "file", filePath)
    } else {
        s.logger.Warn("no checksum sidecar found, skipping verification", "expected", sidecarPath)
    }

    // Feed into existing extract flow
    file := bytes.Buffer{}
    file.Write(data)

    defer func() {
        _ = SetTimeSinceLast(s.Fs, s.Tool.Name, UpdatedKey)
    }()

    return targetPath, s.extract(file, targetPath)
}
```

### Checksum Verification

```go
func VerifyChecksum(fs afero.Fs, sidecarPath string, data []byte) error {
    sidecarContent, err := afero.ReadFile(fs, sidecarPath)
    if err != nil {
        return errors.Wrap(err, "failed to read checksum sidecar")
    }

    // Parse "<hex>  <filename>" or "<hex> <filename>" format
    expectedHash := strings.Fields(strings.TrimSpace(string(sidecarContent)))[0]

    actualHash := fmt.Sprintf("%x", sha256.Sum256(data))

    if !strings.EqualFold(actualHash, expectedHash) {
        return errors.WithHint(
            errors.Newf("checksum mismatch: expected %s, got %s", expectedHash, actualHash),
            "The file may be corrupted or tampered with. Re-download from a trusted source.",
        )
    }

    return nil
}
```

### Update Command Integration

```go
func NewCmdUpdate(props *p.Props) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "update",
        Short: "Update to the latest version",
        RunE: func(cmd *cobra.Command, args []string) error {
            fromFile, _ := cmd.Flags().GetString("from-file")
            if fromFile != "" {
                return updateFromFile(cmd.Context(), props, fromFile)
            }
            // ... existing online update path ...
        },
    }

    cmd.Flags().String("from-file", "", "path to a local .tar.gz release archive for offline installation")
    cmd.MarkFlagsMutuallyExclusive("from-file", "version")

    return cmd
}

func updateFromFile(ctx context.Context, props *p.Props, filePath string) error {
    updater := &SelfUpdater{
        Tool:   props.Tool,
        logger: props.Logger,
        Fs:     props.FS,
    }

    targetPath, err := updater.UpdateFromFile(filePath)
    if err != nil {
        return err
    }

    props.Logger.Infof("successfully installed from %s to %s", filePath, targetPath)
    return nil
}
```

---

## Project Structure

```
pkg/setup/
├── update.go              ← MODIFIED: add UpdateFromFile method
├── update_test.go         ← MODIFIED: add offline update tests
├── checksum.go            ← NEW: VerifyChecksum function
├── checksum_test.go       ← NEW: checksum verification tests
pkg/cmd/update/
├── update.go              ← MODIFIED: add --from-file flag and handler
├── update_test.go         ← MODIFIED: add --from-file integration tests
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestUpdateFromFile_Success` | Valid tarball with matching checksum sidecar &#8594; binary installed |
| `TestUpdateFromFile_NoSidecar` | Valid tarball without sidecar &#8594; warning logged, binary installed |
| `TestUpdateFromFile_ChecksumMismatch` | Sidecar hash does not match &#8594; error with hint, no installation |
| `TestUpdateFromFile_FileNotFound` | Tarball path does not exist &#8594; error |
| `TestUpdateFromFile_InvalidTarball` | File is not valid gzip &#8594; error from extract() |
| `TestUpdateFromFile_BinaryNotInArchive` | Tarball does not contain expected binary name &#8594; no error (silent, matching existing extract behaviour) |
| `TestVerifyChecksum_ValidHash` | SHA-256 matches &#8594; nil |
| `TestVerifyChecksum_InvalidHash` | SHA-256 mismatch &#8594; error with hint |
| `TestVerifyChecksum_MalformedSidecar` | Sidecar file is empty or unparseable &#8594; error |
| `TestVerifyChecksum_SidecarFormats` | Both `<hash>  <file>` and `<hash> <file>` formats accepted |
| `TestUpdateCmd_FromFileFlag` | `--from-file` flag parsed, calls UpdateFromFile |
| `TestUpdateCmd_MutualExclusion` | `--from-file` and `--version` together &#8594; cobra error |

### Test Fixtures

Tests use `afero.MemMapFs` with programmatically-created `.tar.gz` archives containing a mock binary. No real filesystem access needed.

### Coverage

- Target: 90%+ for `checksum.go` and new paths in `update.go`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for `UpdateFromFile` and `VerifyChecksum`.
- Update `docs/components/setup.md` with offline update usage instructions.
- Add a "Air-Gapped Environments" section to the setup documentation explaining the workflow: download release on a connected machine, transfer tarball + checksum, run `update --from-file`.

---

## Backwards Compatibility

- **No breaking changes**. The `--from-file` flag is additive. Existing `update` command behaviour is unchanged when the flag is absent.
- `SelfUpdater.Update()` method is unchanged. `UpdateFromFile` is a new method.
- The `NewUpdater` constructor is not required for offline updates (no VCS client needed), so `updateFromFile` constructs a minimal `SelfUpdater` directly.

---

## Future Considerations

- **Signature verification**: GPG or cosign signature validation for higher assurance. Requires key distribution strategy (embedded public key, config-specified keyring).
- **Checksums.txt support**: GoReleaser produces a single `checksums.txt` with all platform hashes. Parsing this instead of individual sidecar files would simplify distribution.
- **Mirror support**: A `--mirror` flag or config key pointing to an internal HTTP server hosting releases, as a middle ground between full online and fully offline.
- **Rollback**: Save the previous binary before overwriting, enabling `update --rollback` if the new version has issues.

---

## Implementation Phases

### Phase 1 — Checksum Verification
1. Implement `VerifyChecksum` function
2. Add unit tests for all sidecar formats and error cases

### Phase 2 — UpdateFromFile
1. Implement `SelfUpdater.UpdateFromFile` method
2. Wire checksum verification into the flow
3. Add unit tests with in-memory tarball fixtures

### Phase 3 — Command Integration
1. Add `--from-file` flag to update command
2. Implement mutual exclusion with `--version`
3. Add command-level tests

---

## Verification

```bash
go build ./...
go test -race ./pkg/setup/...
go test -race ./pkg/cmd/update/...
go test ./...
golangci-lint run --fix

# Verify new method exists
grep -n 'func.*UpdateFromFile' pkg/setup/update.go

# Verify checksum function exists
grep -n 'func VerifyChecksum' pkg/setup/checksum.go

# Verify flag registration
grep -n 'from-file' pkg/cmd/update/update.go
```
