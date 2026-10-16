package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func renderRootWithKeyCase(t *testing.T, keyCase string) string {
	t.Helper()

	src, err := renderRoot(t, templates.SkeletonRootData{
		Name: "cased", Description: "a tool", Org: "acme", RepoName: "cased", ConfigKeyCase: keyCase,
	})
	require.NoError(t, err)

	return src
}

// Spec 0209 D8: only an exact tool's root carries the field, so a folded
// tool regenerates exactly as it did before the setting existed.
func TestConfigKeyCase_EmittedOnlyWhenExact(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderRootWithKeyCase(t, "exact"), "props.ConfigSpec{KeyCase: props.KeyCaseExact}")
	assert.NotContains(t, renderRootWithKeyCase(t, "folded"), "KeyCase")
	assert.NotContains(t, renderRootWithKeyCase(t, ""), "KeyCase")
}

func TestConfigKeyCase_SurvivesTheManifestRoundTrip(t *testing.T) {
	t.Parallel()

	const doc = `properties:
  name: cased
  description: a tool
  config:
    key_case: exact
release_source:
  type: github
  host: github.com
  org: acme
  repo: cased
`

	var m Manifest
	require.NoError(t, yaml.Unmarshal([]byte(doc), &m))
	require.Equal(t, "exact", m.Properties.Config.KeyCase, "fixture assumption: the yaml tag binds config.key_case")

	src, err := renderRoot(t, buildSkeletonRootData(m, nil))
	require.NoError(t, err)
	assert.Contains(t, src, "KeyCase: props.KeyCaseExact")
}

// Spec 0209 D8: regenerate manifest reads the mode back from the root, both
// incrementally and from scratch, since both go through the props literal.
func TestExtractProperties_ReadsTheKeyCaseBack(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	for keyCase, want := range map[string]string{"exact": "exact", "": ""} {
		path := "/work/pkg/cmd/root/cmd.go"
		require.NoError(t, afero.WriteFile(fs, path, []byte(renderRootWithKeyCase(t, keyCase)), 0o644))

		properties, _, err := g.extractProjectProperties(path)
		require.NoError(t, err)
		assert.Equal(t, want, properties.Config.KeyCase, "rendered %q", keyCase)
	}
}

func TestValidateConfigKeyCase(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "folded", "exact"} {
		require.NoError(t, ValidateConfigKeyCase(ok), ok)
	}

	require.Error(t, ValidateConfigKeyCase("upper"))
}

// Spec 0209 D8, OQ2: gtb set records the mode, renders it, and warns that
// changing it changes how the tool's users' existing files read.
func TestSetSetting_ConfigKeyCaseRecordsRendersAndWarns(t *testing.T) {
	t.Parallel()

	g, fs, logs := newPerimeterTestProject(t, settingsManifest)
	root := g.config.Path
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte(renderRootWithKeyCase(t, "")), 0o644))

	require.NoError(t, g.SetSetting(context.Background(), "config.key_case", []string{"exact"}))

	manifest, _ := afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "config:\n    key_case: exact\n")

	rootGo, err := afero.ReadFile(fs, root+"/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(rootGo), "KeyCase: props.KeyCaseExact")

	assert.True(t, logs.ContainsLevel(logger.WarnLevel, "keys are now case-sensitive"),
		"expected a warning about existing files, got: %v", logs.Messages())

	require.Error(t, g.SetSetting(context.Background(), "config.key_case", []string{"upper"}))
}

// Flipping back warns in the other direction's terms.
func TestRegenerateRoot_WarnsWhenTheKeyCaseBecomesFolded(t *testing.T) {
	t.Parallel()

	g, fs, logs := newPerimeterTestProject(t, settingsManifest)
	require.NoError(t, afero.WriteFile(fs, g.config.Path+"/pkg/cmd/root/cmd.go", []byte(renderRootWithKeyCase(t, "exact")), 0o644))

	m, err := g.loadManifest()
	require.NoError(t, err)
	require.NoError(t, g.regenerateRootCommand(*m))

	assert.True(t, logs.ContainsLevel(logger.WarnLevel, "keys are now folded"),
		"expected a warning about existing files, got: %v", logs.Messages())
}

// An unchanged mode says nothing.
func TestRegenerateRoot_NoWarningWhenTheKeyCaseIsUnchanged(t *testing.T) {
	t.Parallel()

	g, _, logs := newPerimeterTestProject(t, settingsManifest)

	m, err := g.loadManifest()
	require.NoError(t, err)
	require.NoError(t, g.regenerateRootCommand(*m))

	assert.False(t, logs.ContainsLevel(logger.WarnLevel, "keys are now"), "got: %v", logs.Messages())
}
