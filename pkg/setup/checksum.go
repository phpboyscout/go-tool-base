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

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// Size bounds on untrusted inputs. Exported as variables so tools with
// exceptional release layouts can reassign them before calling Update;
// the defaults are generous but protect against a hostile server
// streaming an unbounded response.
//
//nolint:gochecknoglobals // size bounds are tool-author tunables
var (
	// MaxChecksumsSize caps the byte length of a downloaded checksums
	// manifest. A GoReleaser manifest for a typical multi-OS release
	// is ~1 KiB; 1 MiB is 1000× headroom.
	MaxChecksumsSize int64 = 1 << 20

	// MaxBinaryDownloadSize caps the byte length of a downloaded
	// binary asset. 512 MiB is far above any realistic CLI binary;
	// raise this only for tools that legitimately ship larger artefacts.
	MaxBinaryDownloadSize int64 = 512 << 20
)

// DefaultRequireChecksum is the compile-time default for checksum
// enforcement when neither config nor env var provides one. Tool
// authors should set this to true in main() for security-critical
// tools that want fail-closed verification from day one.
//
//nolint:gochecknoglobals // tool-author compile-time override
var DefaultRequireChecksum = false

// ErrChecksumAssetNotFound is returned when the target filename is
// not listed in the checksums manifest. The release may have been
// created without GoReleaser or with a non-default checksums layout.
//
//nolint:gochecknoglobals // sentinel error
var ErrChecksumAssetNotFound = errors.New("asset not found in checksums manifest")

// ErrChecksumManifestMalformed is returned when the checksums
// manifest does not conform to the expected GoReleaser format
// (`<sha256-hex>  <filename>` per line). Rather than silently skip
// malformed lines, the parser rejects the entire manifest so a
// truncated or corrupted download never produces a false pass.
//
//nolint:gochecknoglobals // sentinel error
var ErrChecksumManifestMalformed = errors.New("checksums manifest is malformed")

// ErrChecksumTooLarge is returned when either the checksums manifest
// or the binary download exceeds its configured size bound. Indicates
// a hostile or misbehaving server; the update aborts before hashing.
//
//nolint:gochecknoglobals // sentinel error
var ErrChecksumTooLarge = errors.New("download exceeds maximum size")

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
// or the manifest is rejected as malformed.
//
// Returns nil if the checksum matches, [ErrChecksumAssetNotFound] if
// the filename is not listed, [ErrChecksumManifestMalformed] on
// invalid syntax, or an error wrapping [errors.WithHint] on mismatch.
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

		if m[2] == filename {
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
