package disable

import (
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type disableMCPOptions struct {
	Path string
}

// NewCmdDisableMCP returns the dual-purpose `gtb disable mcp` subcommand. With
// no arguments it turns the mcp feature off (the MCP server subsystem). With one
// or more slash-separated command paths it withholds those commands from the MCP
// tool surface — recording mcp_enabled: false and re-rendering each command's
// cmd.go with the setup.ExcludeFromMCP marker (which also withholds the
// subtree). The commands stay fully runnable on the CLI; only the MCP surface is
// narrowed. Use it for publish/spend/secret commands an assistant should not
// invoke unprompted.
func NewCmdDisableMCP(p *props.Props) *setup.Command {
	opts := disableMCPOptions{}

	cmd := &cobra.Command{
		Use:   "mcp [command-path...]",
		Short: "Disable the mcp feature, or withhold specific commands from MCP",
		Long: `Disable MCP at one of two levels.

With no arguments, turn off the mcp feature (the MCP server subsystem), exactly
like 'gtb disable mcp' as a feature toggle.

With one or more slash-separated command paths (e.g. 'post' or 'post/due'),
withhold those commands from the MCP tool surface: records mcp_enabled: false and
re-renders each command's cmd.go to carry setup.ExcludeFromMCP, so 'mcp tools' /
'mcp start' omit them. The commands stay runnable on the CLI. A protected command
is refused; unprotect it first.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return icmd.RunMCPCommand(cmd, p, opts.Path, args, false)
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}
