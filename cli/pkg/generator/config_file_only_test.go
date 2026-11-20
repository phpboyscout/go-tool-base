package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/errors"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func renderRootWithFileOnly(t *testing.T, fileOnly []string) string {
	t.Helper()

	src, err := renderRoot(t, templates.SkeletonRootData{
		Name: "guarded", Description: "a tool", Org: "acme", RepoName: "guarded", ConfigFileOnly: fileOnly,
	})
	require.NoError(t, err)

	return src
}

// Spec 0214 D7: the root carries the list only when the manifest declares one.
func TestConfigFileOnly_EmittedOnlyWhenDeclared(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderRootWithFileOnly(t, []string{"update.**", "config.sources.**"}),
		`props.ConfigSpec{FileOnly: []string{"update.**", "config.sources.**"}}`)
	assert.NotContains(t, renderRootWithFileOnly(t, nil), "FileOnly")
}

func TestConfigFileOnly_SurvivesRegenerateProject(t *testing.T) {
	t.Parallel()

	const doc = `properties:
  name: guarded
  description: a tool
  config:
    file_only:
      - update.**
release_source:
  type: github
  host: github.com
  org: acme
  repo: guarded
`

	var m Manifest
	require.NoError(t, yaml.Unmarshal([]byte(doc), &m))
	require.Equal(t, []string{"update.**"}, m.Properties.Config.FileOnly, "fixture assumption: the yaml tag binds config.file_only")

	src, err := renderRoot(t, buildSkeletonRootData(m, nil))
	require.NoError(t, err)
	assert.Contains(t, src, `FileOnly: []string{"update.**"}`)
}

// regenerate manifest from scratch reads the list back from the root.
func TestExtractProperties_ReadsTheFileOnlyListBack(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	for _, list := range [][]string{{"update.**", "a.*.c"}, nil} {
		path := "/work/pkg/cmd/root/cmd.go"
		require.NoError(t, afero.WriteFile(fs, path, []byte(renderRootWithFileOnly(t, list)), 0o644))

		properties, _, err := g.extractProjectProperties(path)
		require.NoError(t, err)
		assert.Equal(t, list, properties.Config.FileOnly)
	}
}

func TestValidateManifest_RefusesAMalformedFileOnlyPattern(t *testing.T) {
	t.Parallel()

	err := validateManifestConfig(&ManifestProperties{Config: ManifestConfig{FileOnly: []string{"update.**.x"}}})
	require.ErrorIs(t, err, props.ErrConfigFileOnly)

	require.NoError(t, validateManifestConfig(&ManifestProperties{Config: ManifestConfig{FileOnly: []string{"update.**"}}}))
}

// Spec 0214 D9: the list is edited in the manifest, never through gtb set.
func TestSetSetting_RefusesTheFileOnlyList(t *testing.T) {
	t.Parallel()

	g, _, _ := newPerimeterTestProject(t, "properties:\n  name: mytool\n  config:\n    file_only:\n      - update.**\n"+
		"release_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: org\n  repo: mytool\n"+
		"version:\n  gtb: v1.0.0\n  go: \"1.26\"\ncommands: []\n")

	for _, path := range []string{"config.file_only", "properties.config.file_only"} {
		err := g.SetSetting(context.Background(), path, []string{"telemetry.**"})
		require.ErrorIs(t, err, ErrSettingReadOnly, path)
		assert.Contains(t, errors.FlattenHints(err), ".gtb/manifest.yaml")

		require.ErrorIs(t, g.UnsetSetting(context.Background(), path), ErrSettingReadOnly, path)
	}
}

// The wizard writes every author setting back from its pages, none of which
// asks about the list (spec 0214 D9), so a revisit keeps it.
func TestApplyAuthorSettings_KeepsTheFileOnlyList(t *testing.T) {
	t.Parallel()

	m := &Manifest{Properties: ManifestProperties{Config: ManifestConfig{FileOnly: []string{"update.**"}, KeyCase: "exact"}}}

	cfg := SkeletonConfigFromManifest(*m)
	cfg.ConfigFileOnly = nil
	cfg.ConfigKeyCase = ""
	ApplyAuthorSettingsTo(m, cfg)

	assert.Equal(t, []string{"update.**"}, m.Properties.Config.FileOnly)
	assert.Empty(t, m.Properties.Config.KeyCase, "the rest of the block still takes the wizard's answers")
}
