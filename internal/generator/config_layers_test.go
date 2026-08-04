package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

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

	assert.Contains(t, src, "props.LayerDefaults")
	assert.Contains(t, src, "props.LayerFiles")
	assert.Contains(t, src, "props.LayerEnv")
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

	assert.NotContains(t, src, "ConfigLayers",
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
  config_layers:
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
	require.Equal(t, []string{"defaults", "files", "env"}, m.Properties.ConfigLayers,
		"fixture assumption: the yaml tag still binds config_layers")

	data := buildSkeletonRootData(m, nil)
	require.Equal(t, m.Properties.ConfigLayers, data.ConfigLayers,
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

// renderRoot renders the generated root command to source for assertion.
func renderRoot(t *testing.T, data templates.SkeletonRootData) (string, error) {
	t.Helper()

	var buf bytes.Buffer
	if err := templates.SkeletonRoot(data).Render(&buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}
