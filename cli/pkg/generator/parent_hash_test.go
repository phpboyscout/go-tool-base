package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestAddChild_LeavesADivergedParentsHashAlone (F23 of the v0.43.0 manual
// round): generate command --parent hello must register the child in
// hello's cmd.go, and it did; it then recorded the file's new hash as the
// generated one, so a parent the author had hand-edited read as pristine and
// the next regenerate overwrote the edit with no conflict. The registration
// is still inserted; the recorded hash moves only when the parent was
// pristine before this run touched it.
func TestAddChild_LeavesADivergedParentsHashAlone(t *testing.T) {
	t.Parallel()

	edited := pristineHelloCmd + "\n// hand edit\n"
	fs, root := addChildToHello(t, edited)

	parent, err := afero.ReadFile(fs, root+"/pkg/cmd/hello/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(parent), "sub.NewCmdSub(props)", "the child is registered in the parent")
	assert.Contains(t, string(parent), "// hand edit", "and the hand edit is untouched")

	after := readManifest(t, fs, root)
	assert.Equal(t, calculateHash([]byte(pristineHelloCmd)), after.Commands[0].Hashes["cmd.go"],
		"the parent's recorded hash stays the pristine one, so the next regenerate still sees the divergence")
}

// A parent nobody edited gets its recorded hash moved to the content the
// registration wrote, or the next regenerate reports a false conflict (#212).
func TestAddChild_RefreshesAPristineParentsHash(t *testing.T) {
	t.Parallel()

	fs, root := addChildToHello(t, pristineHelloCmd)

	parent, err := afero.ReadFile(fs, root+"/pkg/cmd/hello/cmd.go")
	require.NoError(t, err)
	require.Contains(t, string(parent), "sub.NewCmdSub(props)", "the child is registered in the parent")

	after := readManifest(t, fs, root)
	assert.Equal(t, calculateHash(parent), after.Commands[0].Hashes["cmd.go"],
		"the parent's recorded hash is the hash of what the registration wrote")
}

// A parent the manifest records no hash for, as after regenerate manifest, was
// adopted with its edit, so removing its last child later replaced it (#255).
func TestAddChild_LeavesAnUnrecordedParentUnrecorded(t *testing.T) {
	t.Parallel()

	for name, onDisk := range map[string]string{
		"edited":   pristineHelloCmd + "\n// hand edit\n",
		"unedited": pristineHelloCmd,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, root := addChildToHelloRecording(t, onDisk, nil)

			parent, err := afero.ReadFile(fs, root+"/pkg/cmd/hello/cmd.go")
			require.NoError(t, err)
			assert.Contains(t, string(parent), "sub.NewCmdSub(props)", "the child is registered in the parent")

			after := readManifest(t, fs, root)
			assert.Empty(t, after.Commands[0].Hashes["cmd.go"], "a file gtb has no hash for stays unrecorded")
		})
	}
}

const pristineHelloCmd = "package hello\n\nimport \"example.com/tool/pkg/props\"\n\nfunc NewCmdHello(props *props.Props) *setup.Command {\n\tcmd := &setup.Command{}\n\n\treturn cmd\n}\n"

// addChildToHello runs generate command --parent hello over a hello cmd.go
// holding parentOnDisk, whose recorded hash is pristineHelloCmd's.
func addChildToHello(t *testing.T, parentOnDisk string) (afero.Fs, string) {
	t.Helper()

	return addChildToHelloRecording(t, parentOnDisk, map[string]string{"cmd.go": calculateHash([]byte(pristineHelloCmd))})
}

// addChildToHelloRecording is addChildToHello with the hashes the manifest
// records for hello.
func addChildToHelloRecording(t *testing.T, parentOnDisk string, hashes map[string]string) (afero.Fs, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	root := "/work"

	m := Manifest{Commands: []ManifestCommand{{Name: "hello", Hashes: hashes}}}
	m.Properties.Name = "tool"
	m.Version.GoToolBase = "v1.0.0"
	raw, err := yaml.Marshal(m)
	require.NoError(t, err)

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", raw, 0o644))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module example.com/tool\n"), 0o644))
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/hello", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/hello/cmd.go", []byte(parentOnDisk), 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t), Version: version.NewInfo("v1.0.0", "", "")}
	g := New(p, &Config{Path: root, Name: "sub", Parent: "hello", NoVerify: true})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.Generate(context.Background()))

	return fs, root
}
