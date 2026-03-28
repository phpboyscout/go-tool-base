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

func TestVerifyChecksum_WhitespaceVariants(t *testing.T) {
	t.Parallel()

	data := []byte("hello world")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "multiple spaces between hash and filename",
			content: hash + "    somefile.tar.gz\n",
			wantErr: false,
		},
		{
			name:    "tab separator between hash and filename",
			content: hash + "\tsomefile.tar.gz\n",
			wantErr: false,
		},
		{
			name:    "mixed spaces and tabs between hash and filename",
			content: hash + " \t  somefile.tar.gz\n",
			wantErr: false,
		},
		{
			name:    "trailing whitespace after hash line",
			content: hash + "  somefile.tar.gz\n   \n",
			wantErr: false,
		},
		{
			name:    "leading whitespace before hash",
			content: "   " + hash + "  somefile.tar.gz\n",
			wantErr: false,
		},
		{
			name:    "leading newlines and spaces before hash",
			content: "\n  \n  " + hash + "  somefile.tar.gz\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte(tt.content), 0o644))

			err := VerifyChecksum(fs, "/sidecar.sha256", data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVerifyChecksum_MultipleChecksumLines(t *testing.T) {
	t.Parallel()

	targetData := []byte("target file content")
	targetHash := fmt.Sprintf("%x", sha256.Sum256(targetData))

	otherData := []byte("other file content")
	otherHash := fmt.Sprintf("%x", sha256.Sum256(otherData))

	// strings.Fields on the full content flattens all lines, so fields[0]
	// is always the hash from the first line. Verify the first hash wins.
	sidecar := otherHash + "  other.tar.gz\n" + targetHash + "  target.tar.gz\n"

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/sidecar.sha256", []byte(sidecar), 0o644))

	// Should match against the first hash (otherHash), so verifying
	// targetData should fail because its hash differs from otherHash.
	err := VerifyChecksum(fs, "/sidecar.sha256", targetData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")

	// Verifying otherData should succeed because its hash matches fields[0].
	err = VerifyChecksum(fs, "/sidecar.sha256", otherData)
	assert.NoError(t, err)
}
