package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// RunMCPCommand implements the dual behaviour of `gtb enable/disable mcp`:
//
//   - With no command-path arguments it toggles the mcp FEATURE (the MCP server
//     subsystem) via RunFeatureToggle.
//   - With one or more slash-separated command paths it withholds those
//     commands from the MCP surface, or exposes them again. A generated
//     command keeps its own mcp_enabled entry and cmd.go marker; any other
//     path goes on properties.mcp.withhold (spec 0213).
//
// enable selects expose (true) vs. exclude (false) for both behaviours.
func RunMCPCommand(cmd *cobra.Command, p *props.Props, path string, commandPaths []string, enable bool) error {
	if len(commandPaths) == 0 {
		return RunFeatureToggle(cmd, p, path, []string{"mcp"}, enable)
	}

	resolved := ResolveProjectPath(p, path)
	gen := generator.New(p, &generator.Config{Path: resolved})

	report, err := gen.SetMCPExposure(cmd.Context(), commandPaths, enable)
	if err != nil {
		return err
	}

	reportMCPExposure(p, report, enable)

	return nil
}

func reportMCPExposure(p *props.Props, report generator.MCPExposureReport, enable bool) {
	surface, unchanged := "withheld from", "Already withheld"
	if enable {
		surface, unchanged = "exposed on", "Already exposed, nothing to change"
	}

	if changed := len(report.Commands) + len(report.Listed); changed > 0 {
		p.Logger.Info(fmt.Sprintf("%d command(s) %s the MCP tool surface (still runnable on the CLI).", changed, surface))
	}

	// gtb cannot see a built-in or attached command from the project, so the
	// built tool's own listing is the audit (spec 0213 D9).
	if !enable && len(report.Listed) > 0 {
		p.Logger.Info(fmt.Sprintf("Withheld by path: %s. Rebuild, then confirm with `%s mcp tools`.",
			strings.Join(report.Listed, ", "), report.Tool))
	}

	if len(report.Unchanged) > 0 {
		p.Logger.Info(fmt.Sprintf("%s: %s.", unchanged, strings.Join(report.Unchanged, ", ")))
	}
}
