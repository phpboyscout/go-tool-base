package props

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBootstrapPolicy_ZeroValue(t *testing.T) {
	t.Parallel()

	var b BootstrapPolicy

	assert.False(t, b.AutoInitialise, "zero value must not auto-initialise")
	assert.Empty(t, b.SkipConfigCheck, "zero value must have no skip entries")
	assert.False(t, b.MatchesSkipList("studio", "krites studio"),
		"zero value must not match any command")
}

func TestBootstrapPolicy_MatchesSkipList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []string
		cmdName string
		cmdPath string
		want    bool
	}{
		{"bare name match", []string{"studio"}, "studio", "krites studio", true},
		{"full path match", []string{"krites studio"}, "studio", "krites studio", true},
		{"no match", []string{"other"}, "studio", "krites studio", false},
		{"empty list", nil, "studio", "krites studio", false},
		{"empty entry ignored", []string{""}, "studio", "krites studio", false},
		{
			"path disambiguates same-named leaf",
			[]string{"krites tools studio"},
			"studio", "krites studio",
			false,
		},
		{
			"path entry matches its own path",
			[]string{"krites tools studio"},
			"studio", "krites tools studio",
			true,
		},
		{"case sensitive", []string{"Studio"}, "studio", "krites studio", false},
		{"multiple entries, one matches", []string{"a", "studio", "b"}, "studio", "krites studio", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := BootstrapPolicy{SkipConfigCheck: tc.entries}
			assert.Equal(t, tc.want, b.MatchesSkipList(tc.cmdName, tc.cmdPath))
		})
	}
}

func TestBootstrapPolicy_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	in := BootstrapPolicy{AutoInitialise: true, SkipConfigCheck: []string{"studio"}}

	data, err := json.Marshal(in)
	require.NoError(t, err)

	var out BootstrapPolicy
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

func TestBootstrapPolicy_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	in := BootstrapPolicy{AutoInitialise: true, SkipConfigCheck: []string{"studio", "krites tools studio"}}

	data, err := yaml.Marshal(in)
	require.NoError(t, err)

	var out BootstrapPolicy
	require.NoError(t, yaml.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

func TestBootstrapPolicy_MatchesAuxiliaryList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []string
		cmdName string
		cmdPath string
		want    bool
	}{
		{"empty list never matches", nil, "help", "tool help", false},
		{"bare name matches", []string{"plumbing"}, "plumbing", "tool plumbing", true},
		{"full path matches", []string{"tool plumbing"}, "plumbing", "tool plumbing", true},
		{"unrelated command does not match", []string{"plumbing"}, "other", "tool other", false},
		{"empty entry never matches", []string{""}, "", "tool", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := BootstrapPolicy{AuxiliaryCommands: tc.entries}
			assert.Equal(t, tc.want, b.MatchesAuxiliaryList(tc.cmdName, tc.cmdPath))
		})
	}
}

func TestBootstrapPolicy_AuxiliaryCommands_RoundTrip(t *testing.T) {
	t.Parallel()

	in := BootstrapPolicy{AuxiliaryCommands: []string{"plumbing", "tool plumbing"}}

	data, err := yaml.Marshal(in)
	require.NoError(t, err)

	var out BootstrapPolicy
	require.NoError(t, yaml.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}
