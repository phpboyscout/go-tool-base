package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCommand_Lifecycle(t *testing.T) {
	fs := afero.NewMemMapFs()
	// Config container requires a logger
	l := logger.NewNoop()
	conf := config.NewFilesContainer(logger.NewNoop(), fs)

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
	"github.com/phpboyscout/gtb/pkg/props"
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
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	conf := config.NewFilesContainer(logger.NewNoop(), fs)

	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Config:  conf,
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	root := "/work"
	_ = fs.MkdirAll(root+"/.gtb", 0755)
	_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("version:\n  gtb: v1.0.0\ncommands:\n  - name: existing\n"), 0644)
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

func TestRegenerateManifest_Lifecycle(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	conf := config.NewFilesContainer(logger.NewNoop(), fs)

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
	"github.com/phpboyscout/gtb/pkg/props"
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
