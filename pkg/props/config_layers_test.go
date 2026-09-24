package props_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestResolveConfigLayers_UnstatedMeansTheFrameworkDefault is the
// backwards-compatibility guard for spec 0183 D9. Every tool that predates the
// declaration leaves the field empty, and must keep resolving exactly as it did.
func TestResolveConfigLayers_UnstatedMeansTheFrameworkDefault(t *testing.T) {
	t.Parallel()

	assert.Equal(t, props.DefaultConfigLayers(), props.Tool{}.ResolveConfigLayers(),
		"an unstated layer set must not change what a tool wires")
}

// TestResolveConfigLayers_EmptyIsNotAnOptOut pins the deliberate reading of an
// empty slice. A tool wanting no layers has nothing to configure and no reason
// to build a store, so treating empty as "none" would turn an omitted field
// into a silently broken tool.
func TestResolveConfigLayers_EmptyIsNotAnOptOut(t *testing.T) {
	t.Parallel()

	tool := props.Tool{Config: props.ConfigSpec{Layers: []props.ConfigLayer{}}}
	assert.Equal(t, props.DefaultConfigLayers(), tool.ResolveConfigLayers())
}

func TestWiresConfigLayer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		declared []props.ConfigLayer
		layer    props.ConfigLayer
		want     bool
	}{
		{"default wires env", nil, props.LayerEnv, true},
		{"default wires flags", nil, props.LayerFlags, true},
		{"default wires defaults", nil, props.LayerDefaults, true},
		{"default wires files", nil, props.LayerFiles, true},
		{"default wires project", nil, props.LayerProject, true},
		{
			name:     "a declaration omitting env declines it",
			declared: []props.ConfigLayer{props.LayerDefaults, props.LayerFiles},
			layer:    props.LayerEnv,
			want:     false,
		},
		{
			name:     "a declaration keeps what it names",
			declared: []props.ConfigLayer{props.LayerDefaults, props.LayerFiles},
			layer:    props.LayerFiles,
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tool := props.Tool{Config: props.ConfigSpec{Layers: tc.declared}}
			assert.Equal(t, tc.want, tool.WiresConfigLayer(tc.layer))
		})
	}
}

// TestDefaultConfigLayers_ExcludesKeychain guards spec 0183 D3: wiring the
// keychain is a decision the host binary makes by blank import, precisely so a
// regulated build can omit it and have the linker drop go-keyring. A default
// that switched it on would take that choice away.
func TestDefaultConfigLayers_ExcludesKeychain(t *testing.T) {
	t.Parallel()

	for _, l := range props.DefaultConfigLayers() {
		assert.NotEqual(t, props.ConfigLayer("keychain"), l,
			"the keychain must never be wired by default")
	}
}

func TestIsValidConfigLayer(t *testing.T) {
	t.Parallel()

	for _, l := range props.AllConfigLayers() {
		assert.Truef(t, props.IsValidConfigLayer(l), "%q should be valid", l)
	}

	assert.False(t, props.IsValidConfigLayer("bogus"))
}

// TestDefaultConfigLayers_IsNotAliased proves each call returns a fresh slice.
// The default is package-level truth; a caller appending to it would otherwise
// change what every later tool wires.
func TestDefaultConfigLayers_IsNotAliased(t *testing.T) {
	t.Parallel()

	first := props.DefaultConfigLayers()
	first[0] = "mutated"

	assert.Equal(t, props.LayerDefaults, props.DefaultConfigLayers()[0],
		"DefaultConfigLayers must not hand out a shared backing array")
}

// Spec 0204 D1: the declaration is the precedence order, lowest first, so a
// tool that moves the project file below its own config files gets exactly
// that.
func TestResolveConfigLayers_TheDeclaredOrderIsPrecedence(t *testing.T) {
	t.Parallel()

	declared := []props.ConfigLayer{props.LayerDefaults, props.LayerProject, props.LayerFiles, props.LayerEnv, props.LayerFlags}
	tool := props.Tool{Config: props.ConfigSpec{Layers: declared}}

	assert.Equal(t, declared, tool.ResolveConfigLayers())
}

// Before spec 0204 the order of the deprecated field was documentation, and
// the store wired the framework's order whatever it said. A tool still setting
// it keeps resolving exactly as it did (D13).
func TestResolveConfigLayers_TheDeprecatedFieldKeepsTheFrameworkOrder(t *testing.T) {
	t.Parallel()

	tool := props.Tool{ConfigLayers: []props.ConfigLayer{props.LayerEnv, props.LayerFiles, props.LayerDefaults}}

	assert.Equal(t, []props.ConfigLayer{props.LayerDefaults, props.LayerFiles, props.LayerEnv}, tool.ResolveConfigLayers())
	assert.False(t, tool.WiresConfigLayer(props.LayerFlags))
}

func TestResolveConfigLayers_TheSpecWinsOverTheDeprecatedField(t *testing.T) {
	t.Parallel()

	tool := props.Tool{
		ConfigLayers: []props.ConfigLayer{props.LayerDefaults},
		Config:       props.ConfigSpec{Layers: []props.ConfigLayer{props.LayerDefaults, props.LayerFlags}},
	}

	assert.Equal(t, []props.ConfigLayer{props.LayerDefaults, props.LayerFlags}, tool.ResolveConfigLayers())
}

func TestCanonicalConfigLayers(t *testing.T) {
	t.Parallel()

	in := []props.ConfigLayer{props.LayerFlags, props.LayerProject, props.LayerDefaults}
	assert.Equal(t, []props.ConfigLayer{props.LayerDefaults, props.LayerProject, props.LayerFlags}, props.CanonicalConfigLayers(in))
	assert.Equal(t, props.LayerFlags, in[0], "the input is not reordered in place")
}

// Each D1 constraint is a way to make a tool quietly unsafe, so each is
// refused by name.
func TestValidateConfigLayers(t *testing.T) {
	t.Parallel()

	const (
		d = props.LayerDefaults
		f = props.LayerFiles
		p = props.LayerProject
		e = props.LayerEnv
		x = props.LayerFlags
	)

	tests := []struct {
		name     string
		layers   []props.ConfigLayer
		want     error
		mentions string
	}{
		{name: "unstated", layers: nil},
		{name: "the framework default", layers: props.DefaultConfigLayers()},
		{name: "a subset", layers: []props.ConfigLayer{d, f}},
		{name: "the project file below the user's files", layers: []props.ConfigLayer{d, p, f, e, x}},
		{name: "env below the user's files", layers: []props.ConfigLayer{d, e, f, x}},
		{name: "no defaults or flags at all", layers: []props.ConfigLayer{f, e}},
		{name: "an unknown layer", layers: []props.ConfigLayer{d, "bogus"}, want: props.ErrUnknownConfigLayer, mentions: "bogus"},
		{name: "a duplicate", layers: []props.ConfigLayer{d, e, e}, want: props.ErrDuplicateConfigLayer, mentions: "env"},
		{name: "defaults above another layer", layers: []props.ConfigLayer{f, d, x}, want: props.ErrConfigLayerOrder, mentions: "defaults"},
		{name: "flags below another layer", layers: []props.ConfigLayer{d, x, e}, want: props.ErrConfigLayerOrder, mentions: "flags"},
		{name: "the project file above env", layers: []props.ConfigLayer{d, f, e, p, x}, want: props.ErrConfigLayerOrder, mentions: "project"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := props.ValidateConfigLayers(tc.layers)
			if tc.want == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tc.want)
			assert.Contains(t, err.Error(), tc.mentions)
			assert.NotEmpty(t, errors.FlattenHints(err), "a refusal says why the rule exists")
		})
	}
}

// Spec 0204 D23: the tool's own config file is named for its format.
func TestTool_ConfigFilename(t *testing.T) {
	t.Parallel()

	for format, want := range map[string]string{
		"":     "config.yaml",
		"yaml": "config.yaml",
		"toml": "config.toml",
		"json": "config.json",
		"hcl":  "config.hcl",
		"ini":  "config.yaml", // refused by props.New; never a name GTB cannot write
	} {
		tool := props.Tool{Config: props.ConfigSpec{Format: format}}
		assert.Equalf(t, want, tool.ConfigFilename(), "format %q", format)
	}
}

// The tool's own format is one init writes and config set edits, so it must
// be one of the four writable formats (spec 0204 D2).
func TestValidateConfigFormat(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "yaml", "toml", "json", "hcl"} {
		require.NoErrorf(t, props.ValidateConfigFormat(ok), "%q", ok)
	}

	for _, bad := range []string{"ini", "xml", "dotenv", "properties", "yml", "bogus"} {
		err := props.ValidateConfigFormat(bad)
		require.ErrorIsf(t, err, props.ErrConfigFormat, "%q", bad)
		assert.Contains(t, errors.FlattenHints(err), "toml")
	}
}
