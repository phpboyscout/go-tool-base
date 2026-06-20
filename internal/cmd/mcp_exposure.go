package cmd

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// RunMCPCommand implements the dual behaviour of `gtb enable/disable mcp`:
//
//   - With no command-path arguments it toggles the mcp FEATURE (the MCP server
//     subsystem) via RunFeatureToggle, exactly as `gtb enable/disable mcp` would
//     if no dedicated subcommand existed.
//   - With one or more slash-separated command paths it sets each command's
//     per-command MCP-exposure decision and re-renders its cmd.go (the
//     setup.ExcludeFromMCP / IncludeInMCP marker).
//
// enable selects expose (true) vs. exclude (false) for both behaviours.
func RunMCPCommand(cmd *cobra.Command, p *props.Props, path string, commandPaths []string, enable bool) error {
	if len(commandPaths) == 0 {
		return RunFeatureToggle(cmd, p, path, []string{"mcp"}, enable)
	}

	resolved := ResolveProjectPath(p, path)
	gen := generator.New(p, &generator.Config{Path: resolved})

	for _, cp := range commandPaths {
		if err := gen.SetMCPEnabled(cmd.Context(), cp, enable); err != nil {
			return err
		}
	}

	surface := "withheld from"
	if enable {
		surface = "exposed on"
	}

	p.Logger.Infof("%d command(s) %s the MCP tool surface (still runnable on the CLI).", len(commandPaths), surface)

	return nil
}
