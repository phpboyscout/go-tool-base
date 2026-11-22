package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// The generated RotationAuthorityKeys is run, not just read: a malformed key
// reaches the updater (which refuses it), and a key in a subdirectory does not.
func TestSkeletonTrustKeys_RotationAuthorityKeysAsBuilt(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                 "module example.org/trustkeys\n\ngo 1.26\n",
		"trustkeys.go":           renderGoFile(t, SkeletonTrustKeys()),
		"keys/.gitkeep":          "",
		"authority/.gitkeep":     "",
		"rotation/a.pub":         "untrusted comment: minisign public key\nRWkey\n",
		"rotation/b.pub":         "not a minisign key",
		"rotation/retired/c.pub": "retired",
		"rotation/notes.asc":     "an OpenPGP key",
		"trustkeys_test.go": `package trustkeys

import (
	"reflect"
	"testing"
)

func TestRotationAuthorityKeys(t *testing.T) {
	want := []string{"untrusted comment: minisign public key\nRWkey\n", "not a minisign key"}
	if got := RotationAuthorityKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
`,
	}

	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}

	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
