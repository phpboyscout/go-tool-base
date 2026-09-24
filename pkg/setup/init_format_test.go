package setup

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configtoml "gitlab.com/phpboyscout/go/config-toml"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// tomlToolProps is a tool whose own format is TOML, optionally linking it.
func tomlToolProps(t *testing.T, linked bool) *props.Props {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range props.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	if linked {
		require.NoError(t, reg.Declare(ConfigFormatDescriptor("toml")))
		RegisterConfigCodecOn(reg, "toml", configtoml.Codec{}, ".toml")
	}

	p, err := props.New(props.Tool{Name: "edtool", Config: props.ConfigSpec{Format: "toml"}},
		logger.NewNoop(), afero.NewMemMapFs(), props.WithFeatures(reg.Snapshot()), props.WithAssets(props.NewAssets()))
	require.NoError(t, err)

	return p
}

// Spec 0204 R4: every init fragment stays YAML, and init writes the merged
// result through the codec of the tool's own format, to config.<ext>.
func TestOpenConfigEditor_WritesTheOwnFormat(t *testing.T) {
	t.Parallel()

	p := tomlToolProps(t, true)

	editor, path, err := OpenConfigEditor(t.Context(), p, "/home/user/.edtool", false)
	require.NoError(t, err)
	assert.Equal(t, "/home/user/.edtool/config.toml", path)

	written, err := afero.ReadFile(p.FS, path)
	require.NoError(t, err)

	docs, err := configtoml.Codec{}.Decode(path, written)
	require.NoError(t, err, "the seed is written as TOML:\n%s", written)
	require.Len(t, docs, 1)
	assert.Equal(t, "info", docs[0]["log"].(map[string]any)["level"], "the framework's YAML fragment arrives in TOML")

	require.NoError(t, editor.Set("log.level", "debug"))

	written, err = afero.ReadFile(p.FS, path)
	require.NoError(t, err)
	assert.Contains(t, string(written), `level = "debug"`, "a wizard write lands as TOML")
}

// Re-running init on an existing file keeps the user's values, gains new
// template keys, and leaves it in its own format.
func TestOpenConfigEditor_MergesAnExistingOwnFormatFile(t *testing.T) {
	t.Parallel()

	p := tomlToolProps(t, true)
	path := "/home/user/.edtool/config.toml"
	require.NoError(t, afero.WriteFile(p.FS, path, []byte("[log]\nlevel = \"debug\"\n"), 0o600))

	editor, _, err := OpenConfigEditor(t.Context(), p, "/home/user/.edtool", false)
	require.NoError(t, err)

	assert.Equal(t, "debug", editor.View().GetString("log.level"), "the user's value survives")
	assert.True(t, editor.View().IsSet("update.policy"), "the template's keys are gained")

	written, err := afero.ReadFile(p.FS, path)
	require.NoError(t, err)

	_, err = configtoml.Codec{}.Decode(path, written)
	require.NoError(t, err, "the merged file is still TOML:\n%s", written)
}

// An own format the binary does not link cannot be written, and says why.
func TestOpenConfigEditor_RefusesAnUnlinkedOwnFormat(t *testing.T) {
	t.Parallel()

	_, _, err := OpenConfigEditor(t.Context(), tomlToolProps(t, false), "/home/user/.edtool", false)
	require.ErrorIs(t, err, ErrUnlinkedConfigFormat)
}

// A YAML tool's file is written exactly as before: the seed's own bytes.
func TestWriteInitialConfig_YAMLIsTheSeedVerbatim(t *testing.T) {
	t.Parallel()

	p := editorProps(t)
	require.NoError(t, p.FS.MkdirAll("/cfg", 0o755))
	require.NoError(t, writeInitialConfig(p, "/cfg/config.yaml", config.YAMLCodec{}, false))

	data, err := afero.ReadFile(p.FS, "/cfg/config.yaml")
	require.NoError(t, err)
	assert.Equal(t, string(AssetDocument(p, InitTemplateAssetPath)), string(data))
}
