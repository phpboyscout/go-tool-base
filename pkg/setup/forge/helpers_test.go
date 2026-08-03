package forge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/keygen"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// newTestProps builds a fixture Props over an in-memory filesystem. It performs
// no env-var mutation, so callers may run in parallel; tests that need a
// specific CI state set it themselves (see newDualTestProps).
func newTestProps(t *testing.T) *props.Props {
	t.Helper()

	return &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Assets: props.NewAssets(),
		Tool:   props.Tool{Name: "testtool"},
	}
}

// newTestEditor materialises a config file under /cfgdir on p.FS (seeded with
// the given YAML document when non-empty) and opens a real store-backed editor
// over it, so wizard writes are observable through editor.View().
func newTestEditor(t *testing.T, p *props.Props, yamlDoc string) setup.Editor {
	t.Helper()

	const dir = "/cfgdir"

	if yamlDoc != "" {
		require.NoError(t, p.FS.MkdirAll(dir, 0o755))
		require.NoError(t, afero.WriteFile(p.FS,
			filepath.Join(dir, setup.DefaultConfigFilename), []byte(yamlDoc), 0o600))
	}

	editor, _, err := setup.OpenConfigEditor(t.Context(), p, dir, false)
	require.NoError(t, err)

	return editor
}

// generateUnencryptedKeyPEM generates a fresh ed25519 private key in OpenSSH PEM
// format (no passphrase).
func generateUnencryptedKeyPEM(t *testing.T) []byte {
	t.Helper()

	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	block, err := gossh.MarshalPrivateKey(privKey, "")
	require.NoError(t, err)

	return pem.EncodeToMemory(block)
}

// passphraseProtectedKeyPEM returns OpenSSH-format private key bytes encrypted
// with a passphrase, so ssh.ParseRawPrivateKey reports the "passphrase
// protected" sentinel that validateSSHKey/isValidSSHKey special-case.
func passphraseProtectedKeyPEM(t *testing.T) []byte {
	t.Helper()

	kp, err := keygen.New(filepath.Join(t.TempDir(), "id_test"),
		keygen.WithPassphrase("super-secret-pass"),
		keygen.WithKeyType(keygen.Ed25519),
	)
	require.NoError(t, err)

	return kp.RawProtectedPrivateKey()
}

// preserveSkipFlags restores the package-level skip flags after a test that
// registers them onto a command.
//
// cobra's BoolVar writes its default into the target variable immediately, and
// these flags default to os.Getenv("CI") == "true" — so under CI, merely
// registering them sets the flags for the rest of the process. A later test
// that builds an initialiser then gets nil, from a skip it never asked for.
// That is the package-level-mutable-state hazard AGENTS.md warns about, reached
// through cobra rather than through a test hook.
func preserveSkipFlags(t *testing.T) {
	t.Helper()

	login, key := skipLogin, skipKey
	bitbucket, gitlab, gitea := skipBitbucket, skipGitlab, skipGitea

	t.Cleanup(func() {
		skipLogin, skipKey = login, key
		skipBitbucket, skipGitlab, skipGitea = bitbucket, gitlab, gitea
	})
}
