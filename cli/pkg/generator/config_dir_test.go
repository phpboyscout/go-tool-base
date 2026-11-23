package generator

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func renderRootWithConfigDir(t *testing.T, dir string) string {
	t.Helper()

	src, err := renderRoot(t, templates.SkeletonRootData{
		Name: "placed", Description: "a tool", Org: "acme", RepoName: "placed", ConfigDir: dir,
	})
	require.NoError(t, err)

	return src
}

// Spec 0221 D3: only an xdg tool's root carries the field, so a project that
// declares nothing regenerates as it did before.
func TestConfigDir_EmittedOnlyWhenXDG(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderRootWithConfigDir(t, "xdg"), "props.ConfigSpec{Dir: props.ConfigDirXDG}")
	assert.NotContains(t, renderRootWithConfigDir(t, "home"), "ConfigDir")
	assert.NotContains(t, renderRootWithConfigDir(t, ""), "ConfigDir")
}

func TestConfigDir_RegenerateCarriesIt(t *testing.T) {
	t.Parallel()

	for dir, want := range map[string]string{"xdg": "xdg", "home": "", "": ""} {
		m := ManifestFromSkeletonConfig(SkeletonConfig{Name: "placed", Repo: "acme/placed", ConfigDir: dir}, nil, "v1.0.0")
		assert.Equal(t, want, m.Properties.Config.Dir, "manifest %q", dir)

		m.Properties.Config.Dir = dir
		assert.Equal(t, want, buildSkeletonTemplateDataFrom(m).ConfigDir, "template %q", dir)
		assert.Equal(t, want, buildSkeletonRootData(m, nil, nil).ConfigDir, "root %q", dir)
	}
}

func TestExtractProperties_ReadsTheConfigDirBack(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	for dir, want := range map[string]string{"xdg": "xdg", "": ""} {
		path := "/work/pkg/cmd/root/cmd.go"
		require.NoError(t, afero.WriteFile(fs, path, []byte(renderRootWithConfigDir(t, dir)), 0o644))

		properties, _, err := g.extractProjectProperties(path)
		require.NoError(t, err)
		assert.Equal(t, want, properties.Config.Dir, "rendered %q", dir)
	}

	path := "/work/pkg/cmd/root/cmd.go"
	literal := strings.Replace(renderRootWithConfigDir(t, "xdg"), "props.ConfigDirXDG", `"xdg"`, 1)
	require.NoError(t, afero.WriteFile(fs, path, []byte(literal), 0o644))

	properties, _, err := g.extractProjectProperties(path)
	require.NoError(t, err)
	assert.Equal(t, "xdg", properties.Config.Dir, "a hand-written literal")

	for _, home := range []string{"props.ConfigDirHome", `"home"`} {
		src := strings.Replace(renderRootWithConfigDir(t, "xdg"), "props.ConfigDirXDG", home, 1)
		require.NoError(t, afero.WriteFile(fs, path, []byte(src), 0o644))

		properties, _, err = g.extractProjectProperties(path)
		require.NoError(t, err)
		assert.Empty(t, properties.Config.Dir, home)
	}
}

func TestValidateConfigDir(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "home", "xdg"} {
		require.NoError(t, ValidateConfigDir(ok), ok)
	}

	require.Error(t, ValidateConfigDir("~/.mytool"))
}

func TestSetSetting_ConfigDirRecordsAndRenders(t *testing.T) {
	t.Parallel()

	g, fs, _ := newPerimeterTestProject(t, settingsManifest)
	rootGo := g.config.Path + "/pkg/cmd/root/cmd.go"
	require.NoError(t, afero.WriteFile(fs, rootGo, []byte(renderRootWithConfigDir(t, "")), 0o644))

	require.NoError(t, g.SetSetting(context.Background(), "config.dir", []string{"xdg"}))

	manifest, err := afero.ReadFile(fs, g.config.Path+"/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(manifest), "config:\n    dir: xdg\n")

	src, err := afero.ReadFile(fs, rootGo)
	require.NoError(t, err)
	assert.Contains(t, string(src), "Dir: props.ConfigDirXDG")

	require.Error(t, g.SetSetting(context.Background(), "config.dir", []string{"native"}))
}
