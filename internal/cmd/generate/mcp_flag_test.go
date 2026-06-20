package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// -- applyMCPExposureChoice (interactive confirm -> tri-state) -----------------

func TestApplyMCPExposureChoice_Expose_LeavesNil(t *testing.T) {
	t.Parallel()

	o := &CommandOptions{ExposeToMCP: true}
	o.applyMCPExposureChoice()
	assert.Nil(t, o.MCPEnabled, "expose is the default; no explicit value recorded")
}

func TestApplyMCPExposureChoice_Exclude_RecordsFalse(t *testing.T) {
	t.Parallel()

	o := &CommandOptions{ExposeToMCP: false}
	o.applyMCPExposureChoice()
	require.NotNil(t, o.MCPEnabled)
	assert.False(t, *o.MCPEnabled, "exclude records mcp_enabled=false")
}

// -- --mcp-enabled flag (non-interactive tri-state) ---------------------------

func TestMCPEnabledFlag_DefaultAndTriState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantChanged bool
		wantValue   bool
	}{
		{"omitted -> not changed, default true", nil, false, true},
		{"--mcp-enabled=false -> changed false", []string{"--mcp-enabled=false"}, true, false},
		{"--mcp-enabled -> changed true", []string{"--mcp-enabled"}, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := NewCmdCommand(&props.Props{})
			require.NoError(t, cmd.ParseFlags(tc.args))

			assert.Equal(t, tc.wantChanged, cmd.Flags().Changed("mcp-enabled"))

			v, err := cmd.Flags().GetBool("mcp-enabled")
			require.NoError(t, err)
			assert.Equal(t, tc.wantValue, v)
		})
	}
}
