package generator

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
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
	// Kept are the paths whose change is recorded in the manifest but not
	// applied, because the cmd.go that carries it was kept.
	Kept []string
}

// SetMCPExposure withholds the commands at paths from the tool's MCP surface,
// or exposes them again. A generated command keeps its own mcp_enabled entry;
// any other path goes on properties.mcp.withhold, which the root renders into
// props.Tool.MCP (spec 0213 D3). Every path is checked before anything is
// written. The whole withhold list is then written before any generated
// command is re-rendered, so a render failure leaves the list and the earlier
// commands changed. A kept cmd.go stops nothing: the rest are done, and the
// report comes back with ErrCommandFileKept.
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

	report := plan.report

	if err := g.applyMCPList(ctx, m, plan, &report, exposed); err != nil {
		return MCPExposureReport{}, err
	}

	if err := g.exposeGenerated(ctx, &report, exposed); err != nil {
		return MCPExposureReport{}, err
	}

	if len(report.Kept) > 0 {
		return report, errors.Wrap(ErrCommandFileKept, strings.Join(report.Kept, ", "))
	}

	return report, nil
}

// applyMCPList writes the withhold list where it changes, or where the root
// command does not carry it, and moves the paths an ignored root leaves as they
// were to report.Kept.
func (g *Generator) applyMCPList(ctx context.Context, m *Manifest, plan mcpExposurePlan, report *MCPExposureReport, exposed bool) error {
	stale, known := g.rootDisagrees(plan, exposed)

	if len(plan.listChanges) > 0 || (known && len(stale) > 0) {
		if err := g.writeMCPWithhold(ctx, m, plan.withhold); err != nil {
			return err
		}
	}

	if g.ignoreRules().IsIgnored(rootCommandRelPath) {
		if len(stale) > 0 {
			g.warnRootKeepsMCPList(stale)
			report.keep(stale)
		}

		return nil
	}

	if known {
		report.relist(stale)
	}

	return nil
}

// rootDisagrees are the paths the root command on disk does not treat as asked,
// which the manifest alone cannot say once a root has been ignored. It reports
// false for a root it cannot read, where every path the list decides counts.
func (g *Generator) rootDisagrees(plan mcpExposurePlan, exposed bool) ([]string, bool) {
	properties, _, err := g.extractProjectProperties(filepath.Join(g.config.Path, rootCommandRelPath))
	if err != nil {
		return plan.listed, false
	}

	withheld := make(map[string]bool, len(properties.MCP.Withhold))
	for _, path := range properties.MCP.Withhold {
		withheld[path] = true
	}

	// A root can withhold a generated command too, whatever its own entry says.
	paths := plan.listed
	if exposed {
		paths = plan.named
	}

	return slices.DeleteFunc(slices.Clone(paths), func(path string) bool {
		covered := withheld[path] || len(listedAncestors(withheld, path)) > 0

		return covered != exposed
	}), true
}

func (g *Generator) warnRootKeepsMCPList(changes []string) {
	g.props.Logger.Warn(fmt.Sprintf("%s is covered by .gtb/ignore, so what the tool publishes over MCP is unchanged for: %s. The manifest records the change. To apply it, take the file out of .gtb/ignore, then run gtb regenerate project.",
		rootCommandRelPath, strings.Join(changes, ", ")))
}

// exposeGenerated applies the change to each of the report's generated commands,
// leaving in report.Commands those whose cmd.go now carries it.
func (g *Generator) exposeGenerated(ctx context.Context, report *MCPExposureReport, exposed bool) error {
	planned := report.Commands
	report.Commands = nil

	for _, path := range planned {
		err := g.SetMCPEnabled(ctx, path, exposed)

		switch {
		case err != nil && !errors.Is(err, ErrCommandFileKept):
			return err
		case slices.Contains(report.Kept, path):
		case err != nil:
			report.Kept = append(report.Kept, path)
		default:
			report.Commands = append(report.Commands, path)
		}
	}

	return nil
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
	listChanges []string
	// named are the paths asked for, once each, and listed those of them the
	// withhold list decides.
	named, listed []string
}

// planMCPExposure sorts paths into a report and works out the withhold list
// the manifest should end up with. It changes nothing.
func planMCPExposure(m *Manifest, paths []string, exposed bool) (mcpExposurePlan, error) {
	listed := make(map[string]bool, len(m.Properties.MCP.Withhold))
	for _, path := range m.Properties.MCP.Withhold {
		listed[path] = true
	}

	var changes []string

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
		if listed[path] == exposed && (exposed || generated == nil) {
			changes = append(changes, path)
			listed[path] = !exposed
		}
	}

	if exposed {
		if err := stillWithheld(listed, named); err != nil {
			return mcpExposurePlan{}, err
		}
	}

	maps.DeleteFunc(listed, func(_ string, withheld bool) bool { return !withheld })

	return mcpExposurePlan{
		report:      report,
		withhold:    slices.Sorted(maps.Keys(listed)),
		listChanges: changes,
		named:       named,
		listed:      withoutPaths(named, withoutPaths(report.Commands, changes)),
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

// keep moves paths to Kept from wherever they were.
func (r *MCPExposureReport) keep(paths []string) {
	r.Kept = append(r.Kept, paths...)
	r.Listed = withoutPaths(r.Listed, paths)
	r.Unchanged = withoutPaths(r.Unchanged, paths)
}

// relist moves paths the list already had, and the root did not, to Listed.
func (r *MCPExposureReport) relist(paths []string) {
	for _, path := range paths {
		if slices.Contains(r.Unchanged, path) {
			r.Listed = append(r.Listed, path)
		}
	}

	r.Unchanged = withoutPaths(r.Unchanged, paths)
}

func withoutPaths(paths, removed []string) []string {
	var kept []string

	for _, path := range paths {
		if !slices.Contains(removed, path) {
			kept = append(kept, path)
		}
	}

	return kept
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
