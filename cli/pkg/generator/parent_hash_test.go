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

	fs := afero.NewMemMapFs()
	root := "/work"

	pristine := "package hello\n\nimport \"example.com/tool/pkg/props\"\n\nfunc NewCmdHello(props *props.Props) *setup.Command {\n\tcmd := &setup.Command{}\n\n\treturn cmd\n}\n"
	edited := pristine + "\n// hand edit\n"

	m := Manifest{Commands: []ManifestCommand{{Name: "hello", Hashes: map[string]string{"cmd.go": calculateHash([]byte(pristine))}}}}
	m.Properties.Name = "tool"
	m.Version.GoToolBase = "v1.0.0"
	raw, err := yaml.Marshal(m)
	require.NoError(t, err)

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", raw, 0o644))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module example.com/tool\n"), 0o644))
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/hello", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/hello/cmd.go", []byte(edited), 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t), Version: version.NewInfo("v1.0.0", "", "")}
	g := New(p, &Config{Path: root, Name: "sub", Parent: "hello", NoVerify: true})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.Generate(context.Background()))

	parent, err := afero.ReadFile(fs, root+"/pkg/cmd/hello/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(parent), "sub.NewCmdSub(props)", "the child is registered in the parent")
	assert.Contains(t, string(parent), "// hand edit", "and the hand edit is untouched")

	after := readManifest(t, fs, root)
	assert.Equal(t, calculateHash([]byte(pristine)), after.Commands[0].Hashes["cmd.go"],
		"the parent's recorded hash stays the pristine one, so the next regenerate still sees the divergence")
}
