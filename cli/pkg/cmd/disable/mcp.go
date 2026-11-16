package disable

import (
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type disableMCPOptions struct {
	Path string
}

// NewCmdDisableMCP returns the dual-purpose `gtb disable mcp` subcommand. With
// no arguments it turns the mcp feature off (the MCP server subsystem). With one
// or more slash-separated command paths it withholds those commands, and
// everything beneath them, from the MCP tool surface. A generated command gets
// mcp_enabled: false and the setup.ExcludeFromMCP marker in its cmd.go; any
// other path, a built-in or attached command, goes on properties.mcp.withhold
// (spec 0213). The commands stay fully runnable on the CLI; only the MCP surface
// is narrowed. Use it for publish/spend/secret commands an assistant should not
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
withhold those commands, and everything beneath them, from the MCP tool surface,
so 'mcp tools' / 'mcp start' omit them. The commands stay runnable on the CLI.

A command gtb generated records mcp_enabled: false and has its cmd.go
re-rendered to carry setup.ExcludeFromMCP. A protected command is refused;
unprotect it first.

Any other path (a built-in such as 'update' or 'config/trust', or a command
attached from a module such as 'sign') goes on properties.mcp.withhold in the
manifest and is compiled into the tool. Nothing a module stamps on its own
command overrides that list. gtb cannot see those commands from the project, so
it cannot check the path: confirm with '<tool> mcp tools' after the next build.
A listed path that names no command stops the tool's MCP server from starting,
so a typo is caught on the first run.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return icmd.RunMCPCommand(cmd, p, opts.Path, args, false)
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}
