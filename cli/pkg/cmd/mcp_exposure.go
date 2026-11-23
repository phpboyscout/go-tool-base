package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ErrOverwriteWithoutPaths is --overwrite on a feature toggle, which edits no
// command.
var ErrOverwriteWithoutPaths = errors.NewSentinel("gtb.cmd.overwrite_without_paths",
	"--overwrite needs a command path")

// OverwriteUsage is the --overwrite usage of the commands that edit one cmd.go.
const OverwriteUsage = "What to do with a cmd.go that was edited by hand: allow, deny, or ask"

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
//
// overwrite says what to do with a generated cmd.go that was edited by hand. The
// feature toggle has no use for it, so it is refused there.
func RunMCPCommand(cmd *cobra.Command, p *props.Props, path, overwrite string, commandPaths []string, enable bool) error {
	mode, err := generator.ParseOverwriteMode(overwrite)
	if err != nil {
		return err
	}

	if len(commandPaths) == 0 {
		if cmd.Flags().Changed("overwrite") {
			return ErrOverwriteWithoutPaths
		}

		return RunFeatureToggle(cmd, p, path, []string{"mcp"}, enable)
	}

	resolved := ResolveProjectPath(p, path)
	gen := generator.New(p, &generator.Config{Path: resolved, Overwrite: mode})

	report, err := gen.SetMCPExposure(cmd.Context(), commandPaths, enable)
	if err != nil && !errors.Is(err, generator.ErrCommandFileKept) {
		return err
	}

	reportMCPExposure(p, report, enable)

	return err
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
