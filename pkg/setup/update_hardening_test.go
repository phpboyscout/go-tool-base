package setup

import (
	"archive/tar"
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarReaderFor returns a tar.Reader positioned at a single "tool" entry carrying
// payload.
func tarReaderFor(t *testing.T, payload []byte) *tar.Reader {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "tool", Mode: 0o755, Size: int64(len(payload))}))
	_, err := tw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	tr := tar.NewReader(&buf)
	_, err = tr.Next()
	require.NoError(t, err)

	return tr
}

// TestExtractAndInstallBinary_InstalledMode0755 pins the installed binary's
// permissions. chmod SETS the mode, so 0o111 would leave the binary
// execute-only (--x--x--x) — it runs, but the owner can no longer read, copy,
// checksum, or back it up. It must be 0o755.
func TestExtractAndInstallBinary_InstalledMode0755(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	s := &SelfUpdater{Fs: fs}

	require.NoError(t, s.extractAndInstallBinary(tarReaderFor(t, []byte("#!binary")), "/usr/local/bin/tool"))

	info, err := fs.Stat("/usr/local/bin/tool")
	require.NoError(t, err)
	assert.Equal(t, "-rwxr-xr-x", info.Mode().Perm().String(),
		"the installed binary must be 0o755, not execute-only")
}

// TestExtractAndInstallBinary_DecompressedSizeBound proves the extraction aborts
// once the cumulative decompressed size exceeds the bound, leaving no temp file
// behind — a gzip bomb cannot expand unbounded even though the compressed
// download cap is generous.
func TestExtractAndInstallBinary_DecompressedSizeBound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	s := &SelfUpdater{Fs: fs, maxExtractedBytes: 100}

	// 500 bytes of payload against a 100-byte bound.
	err := s.extractAndInstallBinary(tarReaderFor(t, bytes.Repeat([]byte{0x41}, 500)), "/usr/local/bin/tool")
	require.ErrorIs(t, err, errDecompressedSizeExceeded, "extraction beyond the decompressed-size bound must fail")

	orphan, statErr := afero.Exists(fs, "/usr/local/bin/tool_")
	require.NoError(t, statErr)
	assert.False(t, orphan, "the partial temp file must be removed on a bound violation")

	installed, statErr := afero.Exists(fs, "/usr/local/bin/tool")
	require.NoError(t, statErr)
	assert.False(t, installed, "a bounded-out install must not produce the target binary")
}
