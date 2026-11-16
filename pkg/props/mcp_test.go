package props_test

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestMCPMode_ZeroAndCompactMeanCompact(t *testing.T) {
	t.Parallel()

	assert.False(t, props.MCPConfig{}.Direct())
	assert.False(t, props.MCPConfig{Mode: props.MCPCompact}.Direct())
	assert.True(t, props.MCPConfig{Mode: props.MCPDirect}.Direct())
	assert.Equal(t, props.MCPCompact, props.MCPMode("compact"))
	assert.Equal(t, props.MCPDirect, props.MCPMode("direct"))
}

func TestNew_RejectsAnUnknownMCPMode(t *testing.T) {
	t.Parallel()

	tool := props.Tool{Name: "t", MCP: props.MCPConfig{Mode: "sideways"}}

	_, err := props.New(tool, logger.NewNoop(), afero.NewMemMapFs())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sideways")

	for _, mode := range []props.MCPMode{"", props.MCPCompact, props.MCPDirect} {
		_, err := props.New(props.Tool{Name: "t", MCP: props.MCPConfig{Mode: mode}}, logger.NewNoop(), afero.NewMemMapFs())
		require.NoError(t, err, string(mode))
	}
}

func TestNew_ChecksEachWithheldMCPPath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"", "/sign", "sign/", "config//trust", "config /trust", "si\tgn"} {
		_, err := props.New(props.Tool{Name: "t", MCP: props.MCPConfig{Withhold: []string{"config/trust", path}}}, logger.NewNoop(), afero.NewMemMapFs())
		require.Error(t, err, "%q", path)
		assert.Contains(t, err.Error(), "Withhold", "%q", path)
	}

	p, err := props.New(props.Tool{Name: "t", MCP: props.MCPConfig{Withhold: []string{"sign", "config/trust", "keys/wkd/publish"}}}, logger.NewNoop(), afero.NewMemMapFs())
	require.NoError(t, err)
	assert.Equal(t, []string{"sign", "config/trust", "keys/wkd/publish"}, p.Tool.MCP.Withhold)
}
