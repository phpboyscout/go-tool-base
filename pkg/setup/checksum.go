package setup

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// Default size bounds on untrusted inputs. Generous, but they stop a hostile
// server streaming an unbounded response into memory.
//
// These are constants, and a tool with an exceptional release layout raises
// them per-updater with [WithMaxChecksumsSize] or [WithMaxBinaryDownloadSize]
// rather than by reassigning a package variable. That is a deliberate change:
// the previous mutable globals raced under t.Parallel — which is why the
// oversized-response test could not run in parallel — and scoped a bound to
// the whole process rather than to the updater that needed it.
const (
	// DefaultMaxChecksumsSize caps a downloaded checksums manifest. A
	// GoReleaser manifest for a typical multi-OS release is ~1 KiB, so 1 MiB
	// is roughly 1000x headroom.
	DefaultMaxChecksumsSize int64 = 1 << 20

	// DefaultMaxBinaryDownloadSize caps a downloaded binary asset. 512 MiB is
	// far above any realistic CLI binary.
	DefaultMaxBinaryDownloadSize int64 = 512 << 20
)

// requireChecksumDefault is the fallback for checksum enforcement when neither
// config nor environment provides one.
//
// A tool wanting fail-closed verification from day one passes
// [WithRequireChecksum] when constructing its updater, rather than mutating a
// package variable at init. Same reasoning as the size bounds above: process-
// wide mutable state is not a configuration mechanism.
const requireChecksumDefault = false

// ErrChecksumAssetNotFound is returned when the target filename is
// not listed in the checksums manifest. The release may have been
// created without GoReleaser or with a non-default checksums layout.
//
//nolint:gochecknoglobals // sentinel error
var ErrChecksumAssetNotFound = errors.NewSentinel("gtb.setup.checksum_asset_not_found", "asset not found in checksums manifest")

// ErrChecksumManifestMalformed is returned when the checksums
// manifest does not conform to the expected GoReleaser format
// (`<sha256-hex>  <filename>` per line). Rather than silently skip
// malformed lines, the parser rejects the entire manifest so a
// truncated or corrupted download never produces a false pass.
//
//nolint:gochecknoglobals // sentinel error
var ErrChecksumManifestMalformed = errors.NewSentinel("gtb.setup.checksum_manifest_malformed", "checksums manifest is malformed")

// ErrChecksumManifestDuplicate is returned when a filename appears more
// than once in the checksums manifest. A duplicate entry is ambiguous —
// silently letting the last one win would let a tampered manifest shadow
// the genuine hash with an attacker-chosen one — so the whole manifest is
// rejected.
//
//nolint:gochecknoglobals // sentinel error
var ErrChecksumManifestDuplicate = errors.NewSentinel("gtb.setup.checksum_manifest_duplicate", "checksums manifest contains a duplicate filename")

// ErrChecksumTooLarge is returned when either the checksums manifest
// or the binary download exceeds its configured size bound. Indicates
// a hostile or misbehaving server; the update aborts before hashing.
//
//nolint:gochecknoglobals // sentinel error
var ErrChecksumTooLarge = errors.NewSentinel("gtb.setup.checksum_too_large", "download exceeds maximum size")

// ErrBinaryTooLarge is returned when a downloaded release binary exceeds
// MaxBinaryDownloadSize. Indicates a hostile or misbehaving server.
var ErrBinaryTooLarge = errors.NewSentinel("gtb.setup.binary_too_large", "binary download exceeds maximum size")

// ErrBinaryNotInArchive is returned when the release archive does not
// contain a binary matching the tool name. Without this, extraction
// silently reported success while leaving the old binary in place.
var ErrBinaryNotInArchive = errors.NewSentinel("gtb.setup.binary_not_in_archive", "expected binary not found in archive")

// checksumLinePattern matches a single GoReleaser manifest entry:
// 64 hex chars, any whitespace run, then a non-whitespace filename.
// Compiled once at package init.
//
//nolint:gochecknoglobals // compiled regex, constant pattern
var checksumLinePattern = regexp.MustCompile(`^([0-9a-fA-F]{64})\s+(\S+)$`)

// manifestScannerMaxLineBytes caps a single manifest line to guard
// against a pathological entry longer than the default bufio buffer
// but still inside MaxChecksumsSize. 64 KiB comfortably exceeds
// any real filename + 64-hex-hash + whitespace.
const manifestScannerMaxLineBytes = 64 * 1024

// manifestScannerInitBuf is the initial bufio scanner buffer; it
// will grow up to manifestScannerMaxLineBytes on demand.
const manifestScannerInitBuf = 4096

// VerifyChecksum reads a SHA-256 sidecar file and verifies it against
// the provided data. The sidecar format is "<hex-hash>  <filename>"
// (matching sha256sum output and GoReleaser checksums.txt entries).
// Returns nil if the checksum matches, or an error with a hint on
// mismatch.
//
// Hash comparison uses [subtle.ConstantTimeCompare] on decoded bytes.
// This is defence-in-depth — practical timing attacks on checksum
// comparison of unknown binary content are infeasible, but the
// constant-time primitive eliminates the class of concern at near-zero
// cost and makes future audits simpler.
func VerifyChecksum(fs afero.Fs, sidecarPath string, data []byte) error {
	sidecarContent, err := afero.ReadFile(fs, sidecarPath)
	if err != nil {
		return errors.Wrap(err, "failed to read checksum sidecar")
	}

	fields := strings.Fields(strings.TrimSpace(string(sidecarContent)))
	if len(fields) == 0 {
		return errors.New("empty or malformed checksum sidecar file")
	}

	expectedHex := fields[0]
	actualHex := fmt.Sprintf("%x", sha256.Sum256(data))

	if !constantTimeHexEqual(expectedHex, actualHex) {
		return errors.WithHint(
			errors.Newf("checksum mismatch: expected %s, got %s", expectedHex, actualHex),
			"The file may be corrupted or tampered with. Re-download from a trusted source.",
		)
	}

	return nil
}

// VerifyChecksumFromManifest verifies data against a named entry in a
// GoReleaser-style checksums manifest. The manifest format is one
// "<hex-sha256>  <filename>" entry per line; blank lines are permitted
// at end-of-file. Every non-blank line must match the expected shape
// or the manifest is rejected as malformed. A filename listed more than
// once rejects the whole manifest ([ErrChecksumManifestDuplicate]) rather
// than letting the last entry silently win.
//
// Returns nil if the checksum matches, [ErrChecksumAssetNotFound] if
// the filename is not listed, [ErrChecksumManifestMalformed] on
// invalid syntax, [ErrChecksumManifestDuplicate] on a repeated filename,
// or an error wrapping [errors.WithHint] on mismatch.
func VerifyChecksumFromManifest(manifest []byte, filename string, data []byte) error {
	expectedHex, err := parseChecksumManifest(manifest, filename)
	if err != nil {
		return err
	}

	actualHex := fmt.Sprintf("%x", sha256.Sum256(data))

	if !constantTimeHexEqual(expectedHex, actualHex) {
		return errors.WithHint(
			errors.Newf("checksum mismatch for %q: expected %s, got %s", filename, expectedHex, actualHex),
			"The file may be corrupted or tampered with. Re-run update or re-download from a trusted source.",
		)
	}

	return nil
}

// VerifyChecksumFromManifestReader is the streaming equivalent of
// [VerifyChecksumFromManifest]. It computes the SHA-256 of dataReader
// while copying into dst, avoiding a second pass over multi-megabyte
// binary data.
//
// maxBytes bounds the total copied; exceeding it returns
// [ErrChecksumTooLarge]. A typical caller passes [MaxBinaryDownloadSize].
//
// Returns the number of bytes copied on success, or an error on
// checksum mismatch, size-limit violation, or copy/IO failure. The
// manifest is parsed before any bytes are hashed, so a manifest-
// lookup failure aborts without touching dst.
func VerifyChecksumFromManifestReader(
	manifest []byte,
	filename string,
	dataReader io.Reader,
	dst io.Writer,
	maxBytes int64,
) (int64, error) {
	expectedHex, err := parseChecksumManifest(manifest, filename)
	if err != nil {
		return 0, err
	}

	hasher := sha256.New()
	// io.MultiWriter fans the reader into both the destination and the
	// hasher in a single streaming pass.
	mw := io.MultiWriter(dst, hasher)
	// Cap the copy at maxBytes+1 so we can distinguish "exactly the
	// limit" (OK) from "exceeded the limit" (fail).
	limited := io.LimitReader(dataReader, maxBytes+1)

	n, err := io.Copy(mw, limited)
	if err != nil {
		return n, errors.Wrap(err, "copying binary during checksum verification")
	}

	if n > maxBytes {
		return n, errors.WithHintf(ErrChecksumTooLarge,
			"asset %q exceeded MaxBinaryDownloadSize (%d bytes); raise the limit if this is legitimate",
			filename, maxBytes)
	}

	actualHex := hex.EncodeToString(hasher.Sum(nil))

	if !constantTimeHexEqual(expectedHex, actualHex) {
		return n, errors.WithHint(
			errors.Newf("checksum mismatch for %q: expected %s, got %s", filename, expectedHex, actualHex),
			"The file may be corrupted or tampered with. Re-run update or re-download from a trusted source.",
		)
	}

	return n, nil
}

// parseChecksumManifest extracts the hex-encoded hash for filename
// from a GoReleaser-style manifest. Every non-blank line must match
// [checksumLinePattern]; a single malformed line rejects the whole
// manifest rather than silently skipping it. A truncated or
// corrupted manifest must never produce a "not found" that the
// caller could mistake for a missing asset.
func parseChecksumManifest(manifest []byte, filename string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(manifest))
	// Guard against a single pathological line that is still under
	// MaxChecksumsSize but longer than the default bufio buffer.
	scanner.Buffer(make([]byte, 0, manifestScannerInitBuf), manifestScannerMaxLineBytes)

	var found string

	seen := make(map[string]struct{})

	for scanner.Scan() {
		// GoReleaser's checksums.txt is LF-terminated, but accept CRLF
		// too — users occasionally round-trip the manifest through a
		// Windows tool before uploading.
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}

		m := checksumLinePattern.FindStringSubmatch(line)
		if m == nil {
			return "", errors.WithHintf(ErrChecksumManifestMalformed,
				"unexpected line in checksums manifest: %q", line)
		}

		entryName := m[2]
		if _, dup := seen[entryName]; dup {
			return "", errors.WithHintf(ErrChecksumManifestDuplicate,
				"%q is listed more than once in the checksums manifest", entryName)
		}

		seen[entryName] = struct{}{}

		if entryName == filename {
			found = m[1]
		}
	}

	if err := scanner.Err(); err != nil {
		return "", errors.Wrap(err, "reading checksums manifest")
	}

	if found == "" {
		return "", errors.WithHintf(ErrChecksumAssetNotFound,
			"%q was not found in the checksums manifest; the release may have been created without GoReleaser",
			filename)
	}

	return found, nil
}

// constantTimeHexEqual decodes both hex strings into byte slices and
// compares them with [subtle.ConstantTimeCompare]. Returns false if
// either side fails to decode — a malformed hex string is never equal.
//
// Comparing the decoded bytes (not the hex strings) means casing
// differences in the hex input do not produce spurious mismatches,
// and the constant-time guarantee is on the 32-byte SHA-256 payload
// itself rather than its human-readable representation.
func constantTimeHexEqual(aHex, bHex string) bool {
	a, err := hex.DecodeString(aHex)
	if err != nil {
		return false
	}

	b, err := hex.DecodeString(bHex)
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(a, b) == 1
}
