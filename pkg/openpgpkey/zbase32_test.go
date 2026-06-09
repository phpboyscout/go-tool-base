package openpgpkey

import (
	"crypto/sha1" //nolint:gosec // see wkd.go header.
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestZbase32Encode_GPGWKSCanonicalExample anchors the encoder against
// the WKD hash that GnuPG's own gpg-wks-client emits for
// `joe.user@example.org`:
//
//	$ gpg-wks-client --print-wkd-hash <<<"joe.user@example.org"
//	1eywsd1ce1nfs68idcpb69buui6xnxrd joe.user@example.org
//
// gpg-wks-client is the reference implementation of the WKD spec;
// matching its output for the canonical example proves byte-for-byte
// compatibility with the IETF-mandated z-base-32 + SHA-1 combination.
// If this ever drifts, our published WKD URLs will be unfetchable by
// every WKD-aware client.
func TestZbase32Encode_GPGWKSCanonicalExample(t *testing.T) {
	t.Parallel()

	sum := sha1.Sum([]byte("joe.user")) //nolint:gosec // see wkd.go header.

	assert.Equal(t, "1eywsd1ce1nfs68idcpb69buui6xnxrd", zbase32Encode(sum[:]),
		"WKD hash of `joe.user` must match gpg-wks-client --print-wkd-hash")
}

func TestZbase32Encode_EmptyInput(t *testing.T) {
	t.Parallel()

	assert.Empty(t, zbase32Encode(nil))
	assert.Empty(t, zbase32Encode([]byte{}))
}

// TestZbase32Encode_LengthInvariant verifies the output length is
// always ceil(len(src)*8/5). z-base-32 has no padding, so any
// length-bug would distort the WKD URL.
func TestZbase32Encode_LengthInvariant(t *testing.T) {
	t.Parallel()

	for inLen := 1; inLen <= 32; inLen++ {
		buf := make([]byte, inLen)
		got := zbase32Encode(buf)
		want := (inLen*8 + 4) / 5
		assert.Lenf(t, got, want,
			"input %d bytes should encode to %d chars (got %d: %q)", inLen, want, len(got), got)
	}
}

// TestZbase32Encode_AlphabetOnly verifies that the encoder never
// emits a character outside the z-base-32 alphabet. A bug pulling
// characters from a different alphabet would silently produce
// unfetchable WKD URLs.
func TestZbase32Encode_AlphabetOnly(t *testing.T) {
	t.Parallel()

	allowed := map[byte]bool{}
	for i := range []byte(zbase32Alphabet) {
		allowed[zbase32Alphabet[i]] = true
	}

	// Cover the full byte-value space.
	src := make([]byte, 256)
	for i := range src {
		src[i] = byte(i)
	}

	for _, c := range []byte(zbase32Encode(src)) {
		assert.Truef(t, allowed[c], "encoder produced non-alphabet byte 0x%02x", c)
	}
}

// TestZbase32Encode_SHA1AlwaysEncodesTo32Chars locks the WKD-specific
// invariant: SHA-1 is 20 bytes → 32 z-base-32 chars exactly. If this
// ever fails, the WKD URLs we emit will be the wrong length.
func TestZbase32Encode_SHA1AlwaysEncodesTo32Chars(t *testing.T) {
	t.Parallel()

	for _, local := range []string{"release", "security", "joe.user", "x", ""} {
		sum := sha1.Sum([]byte(local)) //nolint:gosec // see wkd.go header.
		got := zbase32Encode(sum[:])
		assert.Lenf(t, got, 32, "SHA-1(%q) must encode to 32 chars; got %d: %q", local, len(got), got)
	}
}
