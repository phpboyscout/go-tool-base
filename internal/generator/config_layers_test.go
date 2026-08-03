package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
