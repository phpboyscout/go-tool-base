package generator

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrMCPPathStillWithheld is returned when a path asked to be exposed sits
// under a command that stays on the withhold list.
var ErrMCPPathStillWithheld = errors.NewSentinel("gtb.generator.mcp_path_still_withheld", "a withheld command still covers this path")

// MCPExposureReport says what SetMCPExposure did with each path it was given.
type MCPExposureReport struct {
	// Tool is the project's tool name.
	Tool string
	// Commands are generated commands, changed through their own manifest
	// entry and cmd.go.
	Commands []string
	// Listed are the paths put on, or taken off, properties.mcp.withhold.
	Listed []string
	// Unchanged are the paths already in the state asked for.
	Unchanged []string
}

// SetMCPExposure withholds the commands at paths from the tool's MCP surface,
// or exposes them again. A generated command keeps its own mcp_enabled entry;
// any other path goes on properties.mcp.withhold, which the root renders into
// props.Tool.MCP (spec 0213 D3). Every path is checked before anything is
// written. The whole withhold list is then written before any generated
// command is re-rendered, so a render failure leaves the list and the earlier
// commands changed.
func (g *Generator) SetMCPExposure(ctx context.Context, paths []string, exposed bool) (MCPExposureReport, error) {
	if err := validateMCPWithholdPaths(paths); err != nil {
		return MCPExposureReport{}, err
	}

	m, err := g.loadManifest()
	if err != nil {
		return MCPExposureReport{}, err
	}

	plan, err := planMCPExposure(m, paths, exposed)
	if err != nil {
		return MCPExposureReport{}, err
	}

	if plan.listChanged {
		if err := g.writeMCPWithhold(ctx, m, plan.withhold); err != nil {
			return MCPExposureReport{}, err
		}
	}

	for _, path := range plan.report.Commands {
		if err := g.SetMCPEnabled(ctx, path, exposed); err != nil {
			return MCPExposureReport{}, err
		}
	}

	return plan.report, nil
}

func (g *Generator) writeMCPWithhold(ctx context.Context, m *Manifest, withhold []string) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	m.Properties.MCP.Withhold = withhold

	if err := ValidateManifest(m); err != nil {
		return err
	}

	return g.commitSetting(ctx, m)
}

type mcpExposurePlan struct {
	report      MCPExposureReport
	withhold    []string
	listChanged bool
}

// planMCPExposure sorts paths into a report and works out the withhold list
// the manifest should end up with. It changes nothing.
func planMCPExposure(m *Manifest, paths []string, exposed bool) (mcpExposurePlan, error) {
	listed := make(map[string]bool, len(m.Properties.MCP.Withhold))
	for _, path := range m.Properties.MCP.Withhold {
		listed[path] = true
	}

	before := len(listed)
	named := withoutRepeats(paths)

	report := MCPExposureReport{Tool: m.Properties.Name}

	for _, path := range named {
		generated := walkCommandPath(m.Commands, strings.Split(path, "/"))
		if isProtected(generated) {
			return mcpExposurePlan{}, errors.Wrapf(ErrCommandProtected, "%s", path)
		}

		report.record(path, generated != nil, exposed == listed[path])

		// A generated command reaches the list only by a hand edit, and would
		// stay withheld there whatever its own entry says, so exposing it
		// takes it off.
		if exposed {
			delete(listed, path)
		} else if generated == nil {
			listed[path] = true
		}
	}

	if exposed {
		if err := stillWithheld(listed, named); err != nil {
			return mcpExposurePlan{}, err
		}
	}

	// One call only adds or only removes, so a changed size is a changed list.
	return mcpExposurePlan{
		report:      report,
		withhold:    slices.Sorted(maps.Keys(listed)),
		listChanged: len(listed) != before,
	}, nil
}

func (r *MCPExposureReport) record(path string, generated, changesList bool) {
	switch {
	case generated:
		r.Commands = append(r.Commands, path)
	case changesList:
		r.Listed = append(r.Listed, path)
	default:
		r.Unchanged = append(r.Unchanged, path)
	}
}

func isProtected(cmd *ManifestCommand) bool {
	return cmd != nil && cmd.Protected != nil && *cmd.Protected
}

func withoutRepeats(paths []string) []string {
	unique := make([]string, 0, len(paths))

	for _, path := range paths {
		if !slices.Contains(unique, path) {
			unique = append(unique, path)
		}
	}

	return unique
}

// stillWithheld refuses the first path that a listed ancestor covers: the list
// withholds a whole subtree, and one member cannot be exposed out of it.
func stillWithheld(listed map[string]bool, paths []string) error {
	for _, path := range paths {
		if covering := listedAncestors(listed, path); len(covering) > 0 {
			return errors.WithHint(
				errors.Wrapf(ErrMCPPathStillWithheld, "%s is under %s", path, strings.Join(covering, " and ")),
				fmt.Sprintf("A withheld command covers everything beneath it. Expose all of it with `gtb enable mcp %s`.", strings.Join(covering, " ")),
			)
		}
	}

	return nil
}

func listedAncestors(listed map[string]bool, path string) []string {
	var ancestors []string

	for i, r := range path {
		if r == '/' && listed[path[:i]] {
			ancestors = append(ancestors, path[:i])
		}
	}

	return ancestors
}
