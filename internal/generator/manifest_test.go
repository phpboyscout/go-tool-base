package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/version"
)

func TestVerifyProjectVersion(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	manifestPath := filepath.Join(".gtb", "manifest.yaml")
	require.NoError(t, fs.MkdirAll(".gtb", 0755))

	cfg := &Config{
		Path: "",
	}
	g := New(p, cfg)

	t.Run("CLI version >= Manifest version", func(t *testing.T) {
		manifestContent := "version:\n  gtb: v1.0.0\n"
		require.NoError(t, afero.WriteFile(fs, manifestPath, []byte(manifestContent), 0644))

		err := g.verifyProject()
		assert.NoError(t, err)
	})

	t.Run("CLI version < Manifest version", func(t *testing.T) {
		manifestContent := "version:\n  gtb: v1.1.0\n"
		require.NoError(t, afero.WriteFile(fs, manifestPath, []byte(manifestContent), 0644))

		err := g.verifyProject()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "lower than the version specified in the manifest")
	})

	t.Run("CLI is dev version", func(t *testing.T) {
		p.Version = version.NewInfo("dev", "", "")
		manifestContent := "version:\n  gtb: v2.0.0\n"
		require.NoError(t, afero.WriteFile(fs, manifestPath, []byte(manifestContent), 0644))

		err := g.verifyProject()
		assert.NoError(t, err)
	})
}

func TestManifest_MarshalYAML(t *testing.T) {
	t.Run("ManifestCommand with warning", func(t *testing.T) {
		cmd := ManifestCommand{
			Name:    "warn-cmd",
			Warning: "Careful here",
		}
		data, err := yaml.Marshal(cmd)
		require.NoError(t, err)
		assert.Contains(t, string(data), "name: warn-cmd")
		assert.Contains(t, string(data), "Careful here")
	})

	t.Run("ManifestFlag with warning", func(t *testing.T) {
		flag := ManifestFlag{
			Name:    "warn-flag",
			Default: "val",
			Warning: "Careful flag",
		}
		data, err := yaml.Marshal(flag)
		require.NoError(t, err)
		assert.Contains(t, string(data), "default: val")
		assert.Contains(t, string(data), "Careful flag")
	})
}

func TestRemoveFromManifest_Missing(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{FS: fs, Logger: l, Version: version.NewInfo("v1.0.0", "", "")}

	manifestPath := "/work/.gtb/manifest.yaml"
	_ = fs.MkdirAll("/work/.gtb", 0755)

	m := Manifest{
		Version:  ManifestVersion{GoToolBase: "v1"},
		Commands: []ManifestCommand{{Name: "exists"}},
	}
	data, _ := yaml.Marshal(m)
	_ = afero.WriteFile(fs, manifestPath, data, 0644)

	g := New(p, &Config{Path: "/work", Name: "missing"})

	err := g.removeFromManifest()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in manifest")
}
