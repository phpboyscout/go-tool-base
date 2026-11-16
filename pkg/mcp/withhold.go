package mcp

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ErrWithheldPathNotFound reports a path on the tool's MCP withhold list that
// names no command, so it guards nothing (spec 0213 D8).
var ErrWithheldPathNotFound = errors.NewSentinel("gtb.mcp.withheld_path_not_found", "a withheld MCP path names no command")

// servingCommands are the go/mcp subcommands that bind the tree.
var servingCommands = map[string]bool{"start": true, "stream": true, "tools": true}

// withholding reports whether a command's path, or an ancestor's, is listed.
func withholding(list []string) func(*cobra.Command) bool {
	return func(cmd *cobra.Command) bool {
		path := pathFromRoot(cmd)

		return slices.ContainsFunc(list, func(listed string) bool {
			return path == listed || strings.HasPrefix(path, listed+"/")
		})
	}
}

func pathFromRoot(cmd *cobra.Command) string {
	var segments []string

	for ; cmd.HasParent(); cmd = cmd.Parent() {
		segments = append(segments, cmd.Name())
	}

	slices.Reverse(segments)

	return strings.Join(segments, "/")
}

// guardServing makes a serving subcommand check the list before it runs,
// whichever of Run and RunE go/mcp sets.
func guardServing(p *props.Props, sub *cobra.Command) {
	serve := sub.RunE

	if serve == nil && sub.Run != nil {
		run := sub.Run
		serve = func(cmd *cobra.Command, args []string) error {
			run(cmd, args)

			return nil
		}
	}

	if serve == nil {
		return
	}

	sub.Run = nil
	sub.RunE = refusingStalePaths(p, serve)
}

func refusingStalePaths(p *props.Props, serve func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if stale := stalePaths(cmd.Root(), p); len(stale) > 0 {
			return errors.WithHintf(errors.Wrapf(ErrWithheldPathNotFound, "%s", strings.Join(stale, ", ")),
				"%s is built to withhold these from MCP, and a path that names no command withholds nothing. "+
					"Correct a mistyped path, or take it off with `gtb enable mcp <path>`, then rebuild. "+
					"If a feature that is off adds the command, as ai adds docs/ask, keep the path and turn the feature on: "+
					"taking it off would expose the command once the feature is on.", p.Tool.Name)
		}

		return serve(cmd, args)
	}
}

// stalePaths returns the listed paths that name no command under root. A path
// whose first segment is absent because its feature is off is left out: the
// root registers nothing for it. A deeper command a feature adds cannot be
// traced to it, so it is reported (spec 0213 D8).
func stalePaths(root *cobra.Command, p *props.Props) []string {
	var stale []string

	for _, path := range p.Tool.MCP.Withhold {
		segments := strings.Split(path, "/")

		if namesACommand(root, segments) {
			continue
		}

		if !namesACommand(root, segments[:1]) && isFeatureThatIsOff(p, segments[0]) {
			continue
		}

		stale = append(stale, path)
	}

	return stale
}

func namesACommand(cmd *cobra.Command, segments []string) bool {
	for _, name := range segments {
		children := cmd.Commands()

		child := slices.IndexFunc(children, func(c *cobra.Command) bool { return c.Name() == name })
		if child < 0 {
			return false
		}

		cmd = children[child]
	}

	return true
}

// isFeatureThatIsOff takes a feature's ID to be its root command's name, which
// holds for every built-in. One named otherwise reads as stale while it is off.
func isFeatureThatIsOff(p *props.Props, name string) bool {
	set := p.GetFeatures()

	for _, descriptor := range set.Descriptors() {
		if id := descriptor.FeatureID(); string(id) == name {
			return !set.Enabled(id)
		}
	}

	return false
}
