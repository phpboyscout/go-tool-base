package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestAddCommand_Lifecycle(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	// Config container requires a logger
	l := logger.NewNoop()
	conf := emptyTestStore(t)

	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Config:  conf,
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	root := "/work"
	_ = fs.MkdirAll(root+"/.gtb", 0755)
	_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\n"), 0644)
	_ = afero.WriteFile(fs, root+"/go.mod", []byte("module test-mod\n"), 0644)

	// Add root cmd.go as it's required for registration
	_ = fs.MkdirAll(root+"/pkg/cmd/root", 0755)
	rootCmdContent := `package root
import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/spf13/cobra"
)
func NewCmdRoot(p *props.Props) *cobra.Command {
	cmd := NewCmdRoot(p)
	return cmd
}`
	_ = afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte(rootCmdContent), 0644)

	g := New(p, &Config{
		Path: root,
		Name: "newcmd",
	})

	// Mock runCommand to avoid actual go fmt etc
	g.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	err := g.Generate(context.Background())
	require.NoError(t, err)

	exists, _ := afero.Exists(fs, filepath.Join(root, "pkg/cmd/newcmd/main.go"))
	assert.True(t, exists)
}

func TestRegenerateProject_Lifecycle(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	conf := emptyTestStore(t)

	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Config:  conf,
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	root := "/work"
	_ = fs.MkdirAll(root+"/.gtb", 0755)
	_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\ncommands:\n  - name: existing\n"), 0644)
	_ = afero.WriteFile(fs, root+"/go.mod", []byte("module test-mod\n"), 0644)

	// Add root cmd.go
	_ = fs.MkdirAll(root+"/pkg/cmd/root", 0755)
	_ = afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte("package root\nfunc NewCmdRoot(p interface{}) {}\n"), 0644)

	g := New(p, &Config{
		Path: root,
	})
	g.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	err := g.RegenerateProject(context.Background())
	require.NoError(t, err)

	exists, _ := afero.Exists(fs, filepath.Join(root, "pkg/cmd/existing/cmd.go"))
	assert.True(t, exists)
}

// TestRegenerateProject_DropsAPre0200GoModHash (#88): go.mod stopped being
// hash-compared (spec 0200 D1); an older manifest's entry for it is dropped
// on the first regenerate rather than refreshed on every one.
func TestRegenerateProject_DropsAPre0200GoModHash(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t), Version: version.NewInfo("v1.0.0", "", "")}

	root := "/work"
	_ = fs.MkdirAll(root+"/.gtb", 0o755)
	_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\ncommands: []\nhashes:\n  go.mod: deadbeef\n  justfile: cafe\n"), 0o644)
	_ = afero.WriteFile(fs, root+"/go.mod", []byte("module test-mod\n"), 0o644)
	_ = fs.MkdirAll(root+"/pkg/cmd/root", 0o755)
	_ = afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte("package root\nfunc NewCmdRoot(p interface{}) {}\n"), 0o644)

	g := New(p, &Config{Path: root, Overwrite: OverwriteAllow})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return []byte("done"), nil }

	require.NoError(t, g.RegenerateProject(context.Background()))

	m, err := g.loadManifest()
	require.NoError(t, err)
	_, hasGoMod := m.Hashes["go.mod"]
	assert.False(t, hasGoMod, "the stale go.mod hash is gone")
	assert.Contains(t, m.Hashes, "justfile", "every other hash is kept")
}

func TestRegenerateManifest_Lifecycle(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	conf := emptyTestStore(t)

	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Config:  conf,
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	root := "/work"
	_ = fs.MkdirAll(root+"/.gtb", 0755)
	_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\n"), 0644)
	_ = afero.WriteFile(fs, root+"/go.mod", []byte("module test-mod\n"), 0644)

	// Create a dummy cmd file to scan with correct signature
	_ = fs.MkdirAll(root+"/pkg/cmd/scanned", 0755)
	scannedContent := `package scanned
import (
	"github.com/spf13/cobra"
)
func NewCmdScanned(p interface{}) *cobra.Command {
	return &cobra.Command{Use: "scanned"}
}
`
	_ = afero.WriteFile(fs, root+"/pkg/cmd/scanned/cmd.go", []byte(scannedContent), 0644)

	// Create root command that links to scanned
	_ = fs.MkdirAll(root+"/pkg/cmd/root", 0755)
	rootContent := `package root
import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/spf13/cobra"
	"test-mod/pkg/cmd/scanned"
)
func NewCmdRoot(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "root"}
	cmd.AddCommand(scanned.NewCmdScanned(p))
	return cmd
}`
	_ = afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte(rootContent), 0644)

	g := New(p, &Config{
		Path: root,
	})

	err := g.RegenerateManifest(context.Background())
	require.NoError(t, err)

	manifestData, _ := afero.ReadFile(fs, filepath.Join(root, ".gtb/manifest.yaml"))
	assert.Contains(t, string(manifestData), "scanned")
}
