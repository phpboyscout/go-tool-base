package props_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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

	tool := props.Tool{ConfigLayers: []props.ConfigLayer{}}
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

			tool := props.Tool{ConfigLayers: tc.declared}
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
