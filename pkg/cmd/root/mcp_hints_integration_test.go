package root_test

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// findCommand walks the tree by space-separated Use names.
func findCommand(t *testing.T, cmd *cobra.Command, path string) *cobra.Command {
	t.Helper()

	for name := range strings.FieldsSeq(path) {
		var next *cobra.Command

		for _, c := range cmd.Commands() {
			if c.Name() == name {
				next = c

				break
			}
		}

		require.NotNil(t, next, "command %q not found under %q", name, cmd.Name())
		cmd = next
	}

	return cmd
}

// TestBuiltinCommands_DeclareMCPHints is the spec 0201 D6 table: every
// framework-owned command a tool ships states what it does to an MCP client.
func TestBuiltinCommands_DeclareMCPHints(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "cmd")

	props := newTestProps(p.Enable(p.ConfigCmd), p.Enable(p.TelemetryCmd), p.Enable(p.AiCmd))
	rootCmd := root.NewCmdRoot(props)

	rows := []struct {
		path string
		want setup.MCPHints
	}{
		{"version", setup.MCPReadOnly()},
		{"docs", setup.MCPReadOnly()},
		{"changelog", setup.MCPReadOnly()},
		{"doctor", setup.MCPReadOnly()},
		{"config get", setup.MCPReadOnly()},
		{"config list", setup.MCPReadOnly()},
		{"config validate", setup.MCPReadOnly()},
		{"telemetry status", setup.MCPReadOnly()},
		{"config set", setup.MCPLocalWrite()},
		{"config unset", setup.MCPLocalWrite()},
		{"telemetry enable", setup.MCPLocalWrite()},
		{"telemetry disable", setup.MCPLocalWrite()},
		{"update", setup.MCPOpenWorld()},
		{"init", setup.MCPOpenWorld()},
		{"init ai", setup.MCPOpenWorld()},
		{"config migrate-credentials", setup.MCPDestructive()},
	}

	for _, row := range rows {
		t.Run(row.path, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, row.want, setup.MCPHintsOf(findCommand(t, rootCmd.Command, row.path)))
		})
	}

	assert.True(t, setup.MCPHintsOf(findCommand(t, rootCmd.Command, "mcp")).IsZero(), "the mcp subtree is off the surface and states nothing")
}
