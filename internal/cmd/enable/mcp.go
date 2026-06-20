package enable

import (
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type enableMCPOptions struct {
	Path string
}

// NewCmdEnableMCP returns the dual-purpose `gtb enable mcp` subcommand. With no
// arguments it turns the mcp feature on (the MCP server subsystem). With one or
// more slash-separated command paths it puts those commands back on the MCP
// tool surface — recording mcp_enabled: true (a deliberate, auditable
// re-enablement) and re-rendering each command's cmd.go with the
// setup.IncludeInMCP marker, which also overrides an excluded ancestor.
func NewCmdEnableMCP(p *props.Props) *setup.Command {
	opts := enableMCPOptions{}

	cmd := &cobra.Command{
		Use:   "mcp [command-path...]",
		Short: "Enable the mcp feature, or expose specific commands as MCP tools",
		Long: `Enable MCP at one of two levels.

With no arguments, turn on the mcp feature (the MCP server subsystem), exactly
like 'gtb enable mcp' as a feature toggle.

With one or more slash-separated command paths (e.g. 'post' or 'post/due'), mark
those commands exposed on the MCP tool surface: records mcp_enabled: true and
re-renders each command's cmd.go to carry setup.IncludeInMCP. A protected
command is refused; unprotect it first.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return icmd.RunMCPCommand(cmd, p, opts.Path, args, true)
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}
