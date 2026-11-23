package generator

import (
	"slices"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
)

// minGroupSize is the fewest different flags a group can constrain.
const minGroupSize = 2

const (
	groupMutuallyExclusive = "mutually_exclusive"
	groupRequiredTogether  = "required_together"
)

// groupEdit is what happened to one recorded group. An empty After means the
// whole group went.
type groupEdit struct {
	Kind   string
	Before []string
	After  []string
}

// declaredGroups takes each flag the command does not declare out of the groups,
// and a group that leaves too small. Cobra panics on a group naming a missing
// flag, as soon as the command is constructed.
func declaredGroups(kind string, groups [][]string, declared map[string]bool) ([][]string, []groupEdit) {
	var (
		kept  [][]string
		edits []groupEdit
	)

	for _, group := range groups {
		after := slices.DeleteFunc(slices.Clone(group), func(flag string) bool { return !declared[flag] })
		if len(after) == len(group) {
			kept = append(kept, group)

			continue
		}

		if len(slices.Compact(slices.Clone(after))) < minGroupSize {
			after = nil
		} else {
			kept = append(kept, after)
		}

		edits = append(edits, groupEdit{Kind: kind, Before: group, After: after})
	}

	return kept, edits
}

func renderDeclaredGroups(data *templates.CommandData) []groupEdit {
	declared := map[string]bool{}
	for _, f := range slices.Concat(data.Flags, data.PersistentFlags) {
		declared[f.Name] = true
	}

	var exclusive, together []groupEdit

	data.MutuallyExclusive, exclusive = declaredGroups(groupMutuallyExclusive, data.MutuallyExclusive, declared)
	data.RequiredTogether, together = declaredGroups(groupRequiredTogether, data.RequiredTogether, declared)

	return slices.Concat(exclusive, together)
}

func recordDeclaredGroups(cmd *ManifestCommand, flags []ManifestFlag) []groupEdit {
	declared := make(map[string]bool, len(flags))
	for _, f := range flags {
		declared[f.Name] = true
	}

	var exclusive, together []groupEdit

	cmd.MutuallyExclusive, exclusive = declaredGroups(groupMutuallyExclusive, cmd.MutuallyExclusive, declared)
	cmd.RequiredTogether, together = declaredGroups(groupRequiredTogether, cmd.RequiredTogether, declared)

	return slices.Concat(exclusive, together)
}

func (g *Generator) warnGroupsRendered(edits []groupEdit) {
	for _, e := range edits {
		g.props.Logger.Warn("cmd.go leaves out of a flag group the flags the command does not have",
			"command", g.config.Name, "group", e.Kind, "recorded", e.Before, "rendered", e.After)
	}
}

func (g *Generator) warnGroupsRecorded(edits []groupEdit) {
	for _, e := range edits {
		if len(e.After) == 0 {
			g.props.Logger.Warn("dropped a recorded flag group, too few of its flags are left",
				"command", g.config.Name, "group", e.Kind, "before", e.Before)

			continue
		}

		g.props.Logger.Warn("took the flags the command no longer has out of a recorded flag group",
			"command", g.config.Name, "group", e.Kind, "before", e.Before, "after", e.After)
	}
}
