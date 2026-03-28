package setup

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyChecksum_ValidHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
	}{
		{name: "lowercase", format: "%x"},
		{name: "uppercase", format: "%X"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			data := []byte("hello world")
			hash := fmt.Sprintf(tt.format, sha256.Sum256(data))

			require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte(hash+"  somefile.tar.gz\n"), 0o644))

			err := VerifyChecksum(fs, "/sidecar.sha256", data)
			assert.NoError(t, err)
		})
	}
}

func TestVerifyChecksum_InvalidHash(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	data := []byte("hello world")

	require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte("0000000000000000000000000000000000000000000000000000000000000000  somefile.tar.gz\n"), 0o644))

	err := VerifyChecksum(fs, "/sidecar.sha256", data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestVerifyChecksum_SingleSpaceSeparator(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	data := []byte("test data")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte(hash+" somefile.tar.gz\n"), 0o644))

	err := VerifyChecksum(fs, "/sidecar.sha256", data)
	assert.NoError(t, err)
}

func TestVerifyChecksum_HashOnly(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	data := []byte("test data")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte(hash+"\n"), 0o644))

	err := VerifyChecksum(fs, "/sidecar.sha256", data)
	assert.NoError(t, err)
}

func TestVerifyChecksum_MalformedSidecar_Empty(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte(""), 0o644))

	err := VerifyChecksum(fs, "/sidecar.sha256", []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty or malformed")
}

func TestVerifyChecksum_MalformedSidecar_Whitespace(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte("   \n  \n"), 0o644))

	err := VerifyChecksum(fs, "/sidecar.sha256", []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty or malformed")
}

func TestVerifyChecksum_SidecarNotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	err := VerifyChecksum(fs, "/nonexistent.sha256", []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read checksum sidecar")
}
