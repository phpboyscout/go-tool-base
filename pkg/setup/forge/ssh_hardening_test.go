package forge

import (
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateAndSaveSSHKey_PrivateKeyMode0600 pins the generated private key's
// permissions. A private key must be 0o600 (owner rw) — the convention for key
// files — not the 0o700 directory constant it was previously written with.
func TestGenerateAndSaveSSHKey_PrivateKeyMode0600(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	_, err := generateAndSaveSSHKey(fs, "/id_ed25519", "")
	require.NoError(t, err)

	info, err := fs.Stat("/id_ed25519")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"a generated private key must be mode 0o600")
}
