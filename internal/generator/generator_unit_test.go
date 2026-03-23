package generator

import (
	"context"
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyProject(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Version: version.NewInfo("v2.0.0", "", ""),
	}

	root := "/work"
	g := New(p, &Config{Path: root})

	t.Run("No manifest", func(t *testing.T) {
		err := g.verifyProject()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Please run 'generate skeleton' first")
	})

	t.Run("Valid manifest - same version", func(t *testing.T) {
		_ = fs.MkdirAll(root+"/.gtb", 0755)
		_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("version:\n  gtb: v2.0.0\n"), 0644)
		err := g.verifyProject()
		assert.NoError(t, err)
	})

	t.Run("Manifest version is newer", func(t *testing.T) {
		_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("version:\n  gtb: v2.1.0\n"), 0644)
		err := g.verifyProject()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "is lower than the version specified in the manifest")
	})

	t.Run("CLI version is newer (breaking changes)", func(t *testing.T) {
		p.Version = version.NewInfo("v2.2.0", "", "")
		_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("version:\n  gtb: v1.0.0\n"), 0644)
		err := g.verifyProject()
		assert.NoError(t, err) // It only warns, doesn't error
	})

	t.Run("Dev version bypasses check", func(t *testing.T) {
		p.Version = version.NewInfo("dev", "", "")
		err := g.verifyProject()
		assert.NoError(t, err)
	})
}

func TestGetImportPath_Generator(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs}
	root := "/work"

	_ = fs.MkdirAll(root, 0755)
	_ = afero.WriteFile(fs, root+"/go.mod", []byte("module test-module\n"), 0644)

	g := &Generator{
		props: p,
		config: &Config{
			Path: root,
			Name: "mycmd",
		},
	}

	path, err := g.getImportPath()
	require.NoError(t, err)
	assert.Equal(t, "test-module/pkg/cmd/mycmd", path)
}

func TestSetProtection(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	root := "/work"

	manifestPath := root + "/.gtb/manifest.yaml"
	_ = fs.MkdirAll(root+"/.gtb", 0755)
	_ = afero.WriteFile(fs, manifestPath, []byte("commands:\n  - name: mycmd\n    protected: false\n"), 0644)

	g := New(p, &Config{Path: root})

	err := g.SetProtection(context.Background(), "mycmd", true)
	require.NoError(t, err)

	// Verify manifest
	data, _ := afero.ReadFile(fs, manifestPath)
	assert.Contains(t, string(data), "protected: true")
}
