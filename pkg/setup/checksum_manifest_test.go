package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goreleaserManifest returns a valid multi-entry manifest in
// GoReleaser's default format. Kept as a helper because several
// tests need a clean baseline to mutate.
func goreleaserManifest(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	for name, body := range entries {
		sum := sha256.Sum256(body)
		fmt.Fprintf(&buf, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}

	return buf.Bytes()
}

func TestVerifyChecksumFromManifest_HappyPath(t *testing.T) {
	t.Parallel()

	body := []byte("example binary")
	manifest := goreleaserManifest(t, map[string][]byte{
		"gtb_Linux_x86_64.tar.gz": body,
		"gtb_Darwin_arm64.tar.gz": []byte("other"),
	})

	err := VerifyChecksumFromManifest(manifest, "gtb_Linux_x86_64.tar.gz", body)
	require.NoError(t, err)
}

func TestVerifyChecksumFromManifest_Mismatch(t *testing.T) {
	t.Parallel()

	manifest := goreleaserManifest(t, map[string][]byte{
		"gtb_Linux_x86_64.tar.gz": []byte("original"),
	})

	err := VerifyChecksumFromManifest(manifest, "gtb_Linux_x86_64.tar.gz", []byte("tampered"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestVerifyChecksumFromManifest_AssetNotListed(t *testing.T) {
	t.Parallel()

	manifest := goreleaserManifest(t, map[string][]byte{
		"gtb_Linux_x86_64.tar.gz": []byte("body"),
	})

	err := VerifyChecksumFromManifest(manifest, "gtb_FreeBSD_amd64.tar.gz", []byte("anything"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChecksumAssetNotFound,
		"missing filename must surface as ErrChecksumAssetNotFound so callers can distinguish it from a mismatch")
}

func TestVerifyChecksumFromManifest_MalformedLineRejectsManifest(t *testing.T) {
	t.Parallel()

	// A single malformed line must reject the whole manifest — a
	// truncated or corrupted manifest must never produce a false
	// "not found" that the caller could mistake for a missing asset.
	manifest := []byte("not-a-valid-checksum-line\n" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  foo.tar.gz\n")

	err := VerifyChecksumFromManifest(manifest, "foo.tar.gz", []byte("body"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChecksumManifestMalformed)
}

func TestVerifyChecksumFromManifest_CRLFLineEndings(t *testing.T) {
	t.Parallel()

	// Windows round-trip is a real operator footgun; CRLF must work.
	body := []byte("example")
	sum := sha256.Sum256(body)
	manifest := []byte(hex.EncodeToString(sum[:]) + "  gtb_Windows_x86_64.zip\r\n")

	err := VerifyChecksumFromManifest(manifest, "gtb_Windows_x86_64.zip", body)
	require.NoError(t, err)
}

func TestVerifyChecksumFromManifest_BlankTrailingLines(t *testing.T) {
	t.Parallel()

	body := []byte("example")
	sum := sha256.Sum256(body)
	manifest := []byte(hex.EncodeToString(sum[:]) + "  gtb.tar.gz\n\n\n")

	err := VerifyChecksumFromManifest(manifest, "gtb.tar.gz", body)
	require.NoError(t, err)
}

func TestVerifyChecksumFromManifest_HashCaseInsensitive(t *testing.T) {
	t.Parallel()

	// SHA hashes may be uppercase if produced by certain tooling.
	// Constant-time compare operates on decoded bytes so the hex
	// casing of the expected value must not affect equality.
	body := []byte("example")
	sum := sha256.Sum256(body)
	upper := strings.ToUpper(hex.EncodeToString(sum[:]))
	manifest := []byte(upper + "  gtb.tar.gz\n")

	err := VerifyChecksumFromManifest(manifest, "gtb.tar.gz", body)
	require.NoError(t, err)
}

func TestVerifyChecksumFromManifestReader_HappyPath(t *testing.T) {
	t.Parallel()

	body := []byte("streaming payload")
	manifest := goreleaserManifest(t, map[string][]byte{
		"gtb_Linux_x86_64.tar.gz": body,
	})

	var dst bytes.Buffer

	n, err := VerifyChecksumFromManifestReader(
		manifest,
		"gtb_Linux_x86_64.tar.gz",
		bytes.NewReader(body),
		&dst,
		MaxBinaryDownloadSize,
	)
	require.NoError(t, err)
	assert.EqualValues(t, len(body), n)
	assert.Equal(t, body, dst.Bytes(),
		"dst must receive every byte of the stream in addition to being hashed")
}

func TestVerifyChecksumFromManifestReader_MismatchPreservesCopy(t *testing.T) {
	t.Parallel()

	// Document the "dst may contain bytes on mismatch" contract — the
	// streaming design writes-as-it-hashes so the caller must treat
	// dst as untrusted until the function returns nil. The test
	// asserts both the error and that dst is the streamed tampered
	// content (not the genuine one).
	genuine := []byte("genuine")
	tampered := []byte("tampered")
	manifest := goreleaserManifest(t, map[string][]byte{
		"gtb.tar.gz": genuine,
	})

	var dst bytes.Buffer

	_, err := VerifyChecksumFromManifestReader(
		manifest,
		"gtb.tar.gz",
		bytes.NewReader(tampered),
		&dst,
		MaxBinaryDownloadSize,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.Equal(t, tampered, dst.Bytes())
}

func TestVerifyChecksumFromManifestReader_RejectsOversizedStream(t *testing.T) {
	t.Parallel()

	// A hostile server streaming beyond MaxBinaryDownloadSize must
	// abort before we reach the hash comparison. Use a small cap so
	// the test doesn't need megabytes of data.
	body := bytes.Repeat([]byte("a"), 1024)
	manifest := goreleaserManifest(t, map[string][]byte{
		"gtb.tar.gz": body,
	})

	var dst bytes.Buffer

	_, err := VerifyChecksumFromManifestReader(
		manifest,
		"gtb.tar.gz",
		bytes.NewReader(body),
		&dst,
		16, // deliberately below len(body)
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChecksumTooLarge)
}

func TestVerifyChecksumFromManifestReader_AssetNotListedAbortsBeforeIO(t *testing.T) {
	t.Parallel()

	// Manifest parsing must happen before any bytes are copied; a
	// missing-asset error must leave dst empty because the stream
	// never started.
	manifest := goreleaserManifest(t, map[string][]byte{
		"gtb.tar.gz": []byte("body"),
	})

	var dst bytes.Buffer

	_, err := VerifyChecksumFromManifestReader(
		manifest,
		"missing.tar.gz",
		iotest.ReaderFuncFail(t),
		&dst,
		MaxBinaryDownloadSize,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChecksumAssetNotFound)
	assert.Equal(t, 0, dst.Len(), "dst must be untouched when manifest lookup fails")
}

// iotest holds helpers local to this file — keeping them out of the
// exported test surface so they can't be imported by other packages.
var iotest = struct {
	ReaderFuncFail func(t *testing.T) io.Reader
}{
	ReaderFuncFail: func(t *testing.T) io.Reader {
		return readerFunc(func(_ []byte) (int, error) {
			t.Helper()
			t.Fatal("reader must not be consumed when manifest parsing fails")

			return 0, io.EOF
		})
	},
}

// readerFunc adapts a function to the [io.Reader] interface.
type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// TestConstantTimeHexEqual_RejectsMalformedHex verifies that a hex
// string which fails decoding produces "not equal" rather than a
// panic or a spurious match. Defence-in-depth: even though the
// manifest regex enforces 64-hex, an upstream bug shouldn't turn
// into a false pass.
func TestConstantTimeHexEqual_RejectsMalformedHex(t *testing.T) {
	t.Parallel()

	validHex := strings.Repeat("a", 64)

	// Left side malformed.
	assert.False(t, constantTimeHexEqual("not-hex", validHex))
	// Right side malformed.
	assert.False(t, constantTimeHexEqual(validHex, "zz"+strings.Repeat("a", 62)))
	// Both malformed, even if they're identical strings.
	assert.False(t, constantTimeHexEqual("not-hex", "not-hex"))
	// Different-length valid hex strings (32 bytes vs 1 byte).
	assert.False(t, constantTimeHexEqual(validHex, "aa"))
	// Identical valid hex is equal.
	assert.True(t, constantTimeHexEqual(validHex, validHex))
	// Uppercase vs lowercase of the same bytes is equal.
	assert.True(t, constantTimeHexEqual(validHex, strings.ToUpper(validHex)))
}

// FuzzParseChecksumManifest exercises the parser against arbitrary
// byte sequences. The parser must never panic and must reject
// anything that isn't exactly "<64-hex>  <filename>" on every
// non-blank line.
func FuzzParseChecksumManifest(f *testing.F) {
	// Seeds: well-formed, malformed, and edge cases.
	body := []byte("seed")
	sum := sha256.Sum256(body)
	seed := hex.EncodeToString(sum[:]) + "  file\n"
	f.Add([]byte(seed))
	f.Add([]byte(""))
	f.Add([]byte("not-hex  file\n"))
	f.Add([]byte(strings.Repeat("a", 64) + "  file\n" + strings.Repeat("a", 64) + "  other\n"))
	f.Add([]byte("\n\n\n"))
	f.Add([]byte(strings.Repeat("z", 64) + "  file\n")) // invalid hex char 'z'

	f.Fuzz(func(t *testing.T, manifest []byte) {
		_, err := parseChecksumManifest(manifest, "file")
		if err == nil {
			return
		}
		// Any error must be one of the sentinels or a wrapped IO error;
		// unexpected error types indicate a parser bug.
		if errors.Is(err, ErrChecksumAssetNotFound) || errors.Is(err, ErrChecksumManifestMalformed) {
			return
		}
		// Scanner.Err() wrapped — e.g. token too long. Acceptable.
		if strings.Contains(err.Error(), "reading checksums manifest") {
			return
		}
		t.Fatalf("unexpected error type for manifest %q: %v", string(manifest), err)
	})
}
