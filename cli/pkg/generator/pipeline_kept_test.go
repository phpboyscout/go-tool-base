package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestReRegisterChildCommands_LeavesAKeptFileAlone: when the parent's cmd.go
// was kept (a hand edit under --overwrite deny), the child re-registration
// must neither rewrite the kept file nor record its hash as the generated
// one. Doing so made the hand edit read as generated, so the next regenerate
// saw no conflict and overwrote it silently.
func TestReRegisterChildCommands_LeavesAKeptFileAlone(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/work"

	m := Manifest{Commands: []ManifestCommand{{Name: "hello", Commands: []ManifestCommand{{Name: "sub"}}}}}
	m.Properties.Name = "tool"
	m.Version.GoToolBase = "v1.0.0"
	raw, err := yaml.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", raw, 0o644))

	cmdDir := root + "/pkg/cmd/hello"
	kept := []byte("package hello\n\n// hand edit, kept under --overwrite deny\n")
	require.NoError(t, fs.MkdirAll(cmdDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, cmdDir+"/cmd.go", kept, 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t)}, &Config{Path: root, Name: "hello"})
	g.registrationKept = true

	hashes := map[string]string{"cmd.go": "the-resolver's-hash"}
	require.NoError(t, g.reRegisterChildCommands(cmdDir, hashes))

	got, err := afero.ReadFile(fs, cmdDir+"/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, string(kept), string(got), "the kept file is not touched")
	assert.Equal(t, "the-resolver's-hash", hashes["cmd.go"], "the recorded hash stays the resolver's, so the next run still sees the edit")
}
