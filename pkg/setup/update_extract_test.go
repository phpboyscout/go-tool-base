package setup

import (
	"archive/tar"
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// truncatedTarReader returns a tar.Reader positioned at a file entry whose
// header claims more bytes than the stream contains, so a copy fails mid-way
// — the same failure mode as a context-cancelled download.
func truncatedTarReader(t *testing.T) *tar.Reader {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "tool", Mode: 0o755, Size: 4096}))

	_, err := tw.Write(bytes.Repeat([]byte{0xAB}, 16))
	require.NoError(t, err)
	// Deliberately no Close/Flush: the stream ends short of the declared size.

	tr := tar.NewReader(&buf)
	_, err = tr.Next()
	require.NoError(t, err)

	return tr
}

// TestExtractAndInstallBinary_RemovesTempFileOnFailure proves an interrupted
// install (e.g. SIGINT cancelling the download mid-copy) does not leave the
// partial "<target>_" temp file behind (spec D4, update-temp cleanup).
func TestExtractAndInstallBinary_RemovesTempFileOnFailure(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	s := &SelfUpdater{Fs: fs}

	err := s.extractAndInstallBinary(truncatedTarReader(t), "/usr/local/bin/tool")
	require.Error(t, err, "a truncated stream must fail the install")

	exists, statErr := afero.Exists(fs, "/usr/local/bin/tool_")
	require.NoError(t, statErr)
	assert.False(t, exists, "the partial temp file must be removed on failure")

	installed, statErr := afero.Exists(fs, "/usr/local/bin/tool")
	require.NoError(t, statErr)
	assert.False(t, installed, "a failed install must not produce the target binary")
}

// TestExtractAndInstallBinary_SuccessInstallsBinary guards the happy path:
// the temp file is renamed into place and no "<target>_" orphan remains.
func TestExtractAndInstallBinary_SuccessInstallsBinary(t *testing.T) {
	t.Parallel()

	payload := []byte("#!binary")

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "tool", Mode: 0o755, Size: int64(len(payload))}))
	_, err := tw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	tr := tar.NewReader(&buf)
	_, err = tr.Next()
	require.NoError(t, err)

	fs := afero.NewMemMapFs()
	s := &SelfUpdater{Fs: fs}

	require.NoError(t, s.extractAndInstallBinary(tr, "/usr/local/bin/tool"))

	installed, err := afero.ReadFile(fs, "/usr/local/bin/tool")
	require.NoError(t, err)
	assert.Equal(t, payload, installed)

	orphan, err := afero.Exists(fs, "/usr/local/bin/tool_")
	require.NoError(t, err)
	assert.False(t, orphan, "no temp file may remain after a successful install")
}
