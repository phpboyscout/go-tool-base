package generate

import (
	"slices"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// mcpCommandChoice is one command on the revisit wizard's surface page: its
// slash path and whether it is on the MCP surface today, resolved the way the
// generated markers resolve (an explicit mcp_enabled wins, else the parent's
// state, else exposed).
type mcpCommandChoice struct {
	Path    string
	Exposed bool
}

// mcpSurfaceChange is one command whose tick differs from its recorded state.
type mcpSurfaceChange struct {
	Path    string
	Exposed bool
}

// mcpCommandChoices lists a manifest's commands for the surface page. A
// protected command is left out: the exposure setter refuses it, so offering
// it would only refuse after the fact.
func mcpCommandChoices(commands []generator.ManifestCommand) []mcpCommandChoice {
	return collectMCPChoices(nil, commands, "", true)
}

func collectMCPChoices(out []mcpCommandChoice, commands []generator.ManifestCommand, parent string, parentExposed bool) []mcpCommandChoice {
	for _, c := range commands {
		path := c.Name
		if parent != "" {
			path = parent + "/" + c.Name
		}

		exposed := parentExposed
		if c.MCPEnabled != nil {
			exposed = *c.MCPEnabled
		}

		if c.Protected == nil || !*c.Protected {
			out = append(out, mcpCommandChoice{Path: path, Exposed: exposed})
		}

		out = collectMCPChoices(out, c.Commands, path, exposed)
	}

	return out
}

func (o *SkeletonOptions) mcpSelected() bool {
	return slices.Contains(o.Features, string(props.McpCmd))
}

// mcpGroup asks the publication mode when the feature is selected, and on a
// revisit which commands stay on the surface (spec 0202 D9). Hints stay with
// gtb annotate: five booleans per command are knobs, not a decision.
func (o *SkeletonOptions) mcpGroup() *huh.Group {
	fields := []huh.Field{
		huh.NewSelect[string]().Key("mcp-mode").Title("MCP publication mode").
			Description("Compact publishes three discovery tools a client searches through; direct publishes one native tool per command.").
			Options(
				huh.NewOption("compact (three discovery tools, the default)", string(props.MCPCompact)),
				huh.NewOption("direct (one tool per command)", string(props.MCPDirect)),
			).
			Value(&o.MCPMode),
	}

	if o.revisit && len(o.mcpCommands) > 0 {
		options := make([]huh.Option[string], 0, len(o.mcpCommands))
		for _, c := range o.mcpCommands {
			options = append(options, huh.NewOption(c.Path, c.Path).Selected(c.Exposed))
		}

		fields = append(fields, huh.NewMultiSelect[string]().Key("mcp-surface").Title("Commands on the MCP surface").
			Description("An unticked command stays runnable on the CLI and is withheld from the MCP tool surface. A protected command is not listed; unprotect it first.").
			Options(options...).
			Value(&o.MCPExposed))
	}

	return huh.NewGroup(fields...).
		Title("MCP").
		Description("Recorded under properties.mcp in the manifest; the surface is each command's mcp_enabled.\n").
		WithHideFunc(func() bool { return !o.mcpSelected() })
}

// mcpSurfaceChanges are the commands whose tick on the surface page differs
// from the manifest, each becoming one SetMCPEnabled call: the wizard writes
// the same marker as gtb enable mcp <path> and gtb disable mcp <path>.
func (o *SkeletonOptions) mcpSurfaceChanges() []mcpSurfaceChange {
	if !o.mcpSelected() {
		return nil
	}

	var out []mcpSurfaceChange

	for _, c := range o.mcpCommands {
		if ticked := slices.Contains(o.MCPExposed, c.Path); ticked != c.Exposed {
			out = append(out, mcpSurfaceChange{Path: c.Path, Exposed: ticked})
		}
	}

	return out
}

// describe renders the change for a dry run, beside the author-settings diff.
func (c mcpSurfaceChange) describe() string {
	if c.Exposed {
		return "mcp surface: " + c.Path + " exposed"
	}

	return "mcp surface: " + c.Path + " withheld"
}
