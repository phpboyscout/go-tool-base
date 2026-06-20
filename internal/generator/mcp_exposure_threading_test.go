package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func boolPtr(b bool) *bool { return &b }

func TestManifestCommand_MCPEnabled_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		in          *bool
		wantKey     bool   // mcp_enabled key present in output
		wantLiteral string // expected literal in the YAML when present
	}{
		{name: "nil is omitted", in: nil, wantKey: false},
		{name: "false serialises", in: boolPtr(false), wantKey: true, wantLiteral: "mcp_enabled: false"},
		{name: "true serialises", in: boolPtr(true), wantKey: true, wantLiteral: "mcp_enabled: true"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := ManifestCommand{Name: "post", Description: "publish", MCPEnabled: tc.in}

			data, err := yaml.Marshal(cmd)
			require.NoError(t, err)

			if tc.wantKey {
				assert.Contains(t, string(data), tc.wantLiteral)
			} else {
				assert.NotContains(t, string(data), "mcp_enabled")
			}

			var out ManifestCommand
			require.NoError(t, yaml.Unmarshal(data, &out))

			if tc.in == nil {
				assert.Nil(t, out.MCPEnabled)
			} else {
				require.NotNil(t, out.MCPEnabled)
				assert.Equal(t, *tc.in, *out.MCPEnabled)
			}
		})
	}
}

func TestBuildCommandContext_CarriesMCPEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   *bool
	}{
		{"nil", nil},
		{"excluded", boolPtr(false)},
		{"exposed", boolPtr(true)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := ManifestCommand{Name: "post", MCPEnabled: tc.in}

			ctx := buildCommandContext("/proj", false, false, false, cmd, nil)
			assert.Equal(t, tc.in, ctx.MCPEnabled, "context carries the manifest decision")

			cfg := ctx.ToConfig()
			assert.Equal(t, tc.in, cfg.MCPEnabled, "ToConfig carries it to the generator config")
		})
	}
}

// TestManifestCommand_MCPEnabled_NotConfusedWithProtected guards the two
// independent tri-state pointers from cross-wiring during (de)serialisation.
func TestManifestCommand_MCPEnabled_NotConfusedWithProtected(t *testing.T) {
	t.Parallel()

	cmd := ManifestCommand{Name: "post", Description: "x", Protected: boolPtr(true), MCPEnabled: boolPtr(false)}

	data, err := yaml.Marshal(cmd)
	require.NoError(t, err)
	assert.Contains(t, string(data), "protected: true")
	assert.Contains(t, string(data), "mcp_enabled: false")

	var out ManifestCommand
	require.NoError(t, yaml.Unmarshal(data, &out))
	require.NotNil(t, out.Protected)
	require.NotNil(t, out.MCPEnabled)
	assert.True(t, *out.Protected, "protected decodes independently")
	assert.False(t, *out.MCPEnabled, "mcp_enabled decodes independently")
}
