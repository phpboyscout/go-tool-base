package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configini "gitlab.com/phpboyscout/go/config-ini"
	configtoml "gitlab.com/phpboyscout/go/config-toml"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// codecRegistry links the named formats on a fresh registry, the way their
// link packages would on the default one.
func codecRegistry(t *testing.T, formats map[string]config.Codec, exts map[string][]string) features.Set {
	t.Helper()

	reg := features.NewRegistry()
	for format, codec := range formats {
		require.NoError(t, reg.Declare(setup.ConfigFormatDescriptor(format)))
		setup.RegisterConfigCodecOn(reg, format, codec, exts[format]...)
	}

	set, err := features.Resolve(reg.Snapshot(), nil)
	require.NoError(t, err)

	return set
}

func TestConfigFormatDescriptor_IsALink(t *testing.T) {
	t.Parallel()

	d := setup.ConfigFormatDescriptor("toml")

	assert.Equal(t, props.FeatureID("config-format-toml"), d.ID)
	assert.Equal(t, props.KindLink, d.Kind)
	assert.True(t, d.Default, "a link's presence is its enablement")
}

func TestConfigCodecsIn_ReadsWhatIsLinked(t *testing.T) {
	t.Parallel()

	set := codecRegistry(t,
		map[string]config.Codec{"toml": configtoml.Codec{}},
		map[string][]string{"toml": {".toml"}})

	codecs := setup.ConfigCodecsIn(set)
	require.Len(t, codecs, 1)
	assert.Equal(t, "toml", codecs[0].Format)
	assert.Equal(t, []string{".toml"}, codecs[0].Extensions)
}

// Spec 0204 D2: a file's codec comes from its extension. YAML, with or
// without an extension, needs no link; any other format must be linked.
func TestConfigCodecFor(t *testing.T) {
	t.Parallel()

	codecs := setup.ConfigCodecsIn(codecRegistry(t,
		map[string]config.Codec{"toml": configtoml.Codec{}, "ini": configini.Codec{}},
		map[string][]string{"toml": {".toml"}, "ini": {".ini"}}))

	tests := []struct {
		path     string
		want     config.Codec
		writable bool
	}{
		{path: "/etc/tool/config.yaml", want: config.YAMLCodec{}, writable: true},
		{path: "/etc/tool/config.yml", want: config.YAMLCodec{}, writable: true},
		{path: "/etc/tool/config", want: config.YAMLCodec{}, writable: true},
		{path: "/home/me/.tool.conf", want: config.YAMLCodec{}, writable: true},
		{path: "/etc/tool/config.toml", want: configtoml.Codec{}, writable: true},
		{path: "/etc/tool/CONFIG.TOML", want: configtoml.Codec{}, writable: true},
		{path: "/etc/tool/config.ini", want: configini.Codec{}, writable: false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			got, err := setup.ConfigCodecFor(codecs, tc.path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			_, editing := got.(config.EditingCodec)
			assert.Equal(t, tc.writable, editing)
		})
	}
}

// An extension the tool has not linked is refused before any read, naming
// what the tool does accept.
func TestConfigCodecFor_RefusesAnUnlinkedFormat(t *testing.T) {
	t.Parallel()

	codecs := setup.ConfigCodecsIn(codecRegistry(t,
		map[string]config.Codec{"toml": configtoml.Codec{}},
		map[string][]string{"toml": {".toml"}}))

	_, err := setup.ConfigCodecFor(codecs, "/work/config.json")
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)
	assert.Contains(t, err.Error(), "config.json")
	assert.Contains(t, errors.FlattenHints(err), ".toml")
	assert.Contains(t, errors.FlattenHints(err), ".yaml")
	assert.Contains(t, errors.FlattenHints(err), "pkg/config/formats/json", "the refusal names the link that would read it")
}
