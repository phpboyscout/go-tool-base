package generator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Spec 0183 D8 put the layer declaration in the manifest, and spec 0204 D13
// moved it to properties.config.layers, where its order is the precedence.
//
// Spec 0183 D8 puts the layer declaration in the manifest rather than in the
// scaffolded main, because the manifest reconstructs byte-exactly from scratch.
// A hand-wired set would be a hole reconstruction cannot fill, so `regenerate`
// would silently emit a project wiring different layers from the one it ran
// against. These pin that the declaration survives the round trip and that a
// bad one is caught at the manifest rather than in generated source.

func TestValidateConfigLayers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		layers  []string
		wantErr string
	}{
		{name: "empty is valid — it means unstated"},
		{name: "every known layer", layers: []string{"defaults", "files", "project", "env", "flags"}},
		{name: "a subset is valid", layers: []string{"defaults", "files"}},
		{name: "unknown layer", layers: []string{"defaults", "bogus"}, wantErr: "unknown config layer"},
		{name: "duplicate layer", layers: []string{"env", "env"}, wantErr: "duplicate config layer"},
		{name: "the project file below the user's", layers: []string{"defaults", "project", "files", "env", "flags"}},
		{name: "defaults not lowest", layers: []string{"files", "defaults"}, wantErr: "defaults must be the lowest layer"},
		{name: "flags not highest", layers: []string{"flags", "env"}, wantErr: "flags must be the highest layer"},
		{name: "project above env", layers: []string{"env", "project"}, wantErr: "project must sit below env"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateConfigLayers(tc.layers)
			if tc.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			// rejectf renders as "invalid generator input"; the field and rule
			// travel as a hint, which is what the operator actually sees.
			assert.Contains(t, errors.FlattenHints(err), tc.wantErr)
		})
	}
}

// TestConfigLayers_EmittedAsConstants proves the generated root references
// props.Layer* constants rather than string literals, so a layer that stops
// existing fails the generated project's build instead of resolving to nothing
// at runtime.
func TestConfigLayers_EmittedAsConstants(t *testing.T) {
	t.Parallel()

	src, err := renderRoot(t, templates.SkeletonRootData{
		Name:         "layered",
		Description:  "a tool",
		Org:          "acme",
		RepoName:     "layered",
		ConfigLayers: []string{"defaults", "files", "env"},
	})
	require.NoError(t, err)

	assert.Contains(t, src, "props.ConfigSpec{Layers: []props.ConfigLayer{props.LayerDefaults, props.LayerFiles, props.LayerEnv}}")
	assert.NotContains(t, src, "ConfigLayers:", "the deprecated field is not emitted")
	assert.NotContains(t, src, `"defaults"`, "layers must not be emitted as bare strings")
	assert.NotContains(t, src, "props.LayerFlags", "a layer the project declined must not be wired")
}

// TestConfigLayers_UnstatedEmitsNothing is the backwards-compatibility guard:
// a project that declares no layer set must produce exactly the output it did
// before the field existed.
func TestConfigLayers_UnstatedEmitsNothing(t *testing.T) {
	t.Parallel()

	src, err := renderRoot(t, templates.SkeletonRootData{
		Name:        "plain",
		Description: "a tool",
		Org:         "acme",
		RepoName:    "plain",
	})
	require.NoError(t, err)

	assert.NotContains(t, src, "ConfigSpec",
		"an unstated layer set must not emit a field")
}

// TestConfigLayers_SurviveTheManifestRoundTrip is the assertion the rest of
// this file only claims: that a declared layer set makes it out of a manifest
// and back into generated source.
//
// This is what D8 is actually for. The layer set lives in the manifest rather
// than in the scaffolded main precisely so `regenerate` can recover it — and if
// it could not, regenerate would quietly emit a project wiring the framework
// default over one that had declined a layer. Serialising through YAML rather
// than building the struct inline is deliberate: the field carries a yaml tag,
// and a tag that stops matching would not show up in a struct-only test.
func TestConfigLayers_SurviveTheManifestRoundTrip(t *testing.T) {
	t.Parallel()

	const doc = `properties:
  name: layered
  description: a tool
  config:
    layers:
      - defaults
      - files
      - env
release_source:
  type: github
  host: github.com
  org: acme
  repo: layered
`

	var m Manifest
	require.NoError(t, yaml.Unmarshal([]byte(doc), &m))
	require.Equal(t, []string{"defaults", "files", "env"}, m.Properties.Config.Layers,
		"fixture assumption: the yaml tag still binds config.layers")

	data := buildSkeletonRootData(m, nil)
	require.Equal(t, m.Properties.Config.Layers, data.ConfigLayers,
		"the declared set must reach the render data unchanged")

	src, err := renderRoot(t, data)
	require.NoError(t, err)

	assert.Contains(t, src, "props.LayerDefaults")
	assert.Contains(t, src, "props.LayerFiles")
	assert.Contains(t, src, "props.LayerEnv")
	assert.NotContains(t, src, "props.LayerProject",
		"a layer the project declined must not reappear on regenerate")
	assert.NotContains(t, src, "props.LayerFlags",
		"a layer the project declined must not reappear on regenerate")
}

// TestConfigLayerConst covers the manifest-name to constant-name mapping every
// emitted layer goes through.
func TestConfigLayerConst(t *testing.T) {
	t.Parallel()

	for _, l := range props.AllConfigLayers() {
		name := string(l)
		got := templates.ConfigLayerConstName(name)

		assert.Truef(t, strings.HasPrefix(got, "Layer"), "%q -> %q should be a Layer* constant", name, got)
		assert.Equalf(t, strings.ToUpper(name[:1]), got[5:6],
			"%q -> %q should capitalise the layer name", name, got)
	}
}

// Spec 0204 D13: the first regenerate moves a 0183 config_layers list into
// config.layers. Its order never reached the store, so it moves in the
// framework's order, and what the tool resolves does not change.
func TestRegenerateProject_MovesConfigLayersIntoConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	log := logger.NewBuffer()
	p := &props.Props{FS: fs, Logger: log, Config: emptyTestStore(t), Version: version.NewInfo("v1.0.0", "", "")}

	root := "/work"
	_ = fs.MkdirAll(root+"/.gtb", 0o755)
	_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(`properties:
  name: oldtool
  module_path: github.com/acme/oldtool
  config_layers:
    - flags
    - env
    - defaults
  features:
    - name: github
      enabled: true
    - name: changelog
      enabled: false
    - name: docs
      enabled: false
release_source:
  type: github
  backend: github
  host: github.com
  owner: acme
  repo: oldtool
version:
  gtb: v1.0.0
commands: []
`), 0o644)
	_ = afero.WriteFile(fs, root+"/go.mod", []byte("module github.com/acme/oldtool\n"), 0o644)
	_ = fs.MkdirAll(root+"/pkg/cmd/root", 0o755)
	_ = afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte("package root\nfunc NewCmdRoot(p interface{}) {}\n"), 0o644)

	g := New(p, &Config{Path: root, Overwrite: OverwriteAllow})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return []byte("done"), nil }

	require.NoError(t, g.RegenerateProject(context.Background()))

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, []string{"defaults", "env", "flags"}, m.Properties.Config.Layers)
	assert.Empty(t, m.Properties.LegacyConfigLayers)

	written := readGenerated(t, fs, root+"/.gtb/manifest.yaml")
	assert.NotContains(t, written, "config_layers")
	assert.Contains(t, log.String(), "recorded the derived values", "the move is logged")

	rootSrc := readGenerated(t, fs, root+"/pkg/cmd/root/cmd.go")
	assert.Contains(t, rootSrc, "Layers: []props.ConfigLayer{props.LayerDefaults, props.LayerEnv, props.LayerFlags}")
}

// renderRoot renders the generated root command to source for assertion.
func renderRoot(t *testing.T, data templates.SkeletonRootData) (string, error) {
	t.Helper()

	var buf bytes.Buffer
	if err := templates.SkeletonRoot(data).Render(&buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}
