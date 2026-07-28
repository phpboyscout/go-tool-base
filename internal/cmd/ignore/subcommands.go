package ignore

import (
	"fmt"
	"io"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// newCmdIgnoreAdd appends pattern(s) to .gtb/ignore, creating the file (with an
// explanatory header) when absent. Adding a pattern already present is a
// reported no-op. It does not regenerate.
func newCmdIgnoreAdd(p *props.Props) *setup.Command {
	var (
		path   string
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "add <pattern>...",
		Short: "Append pattern(s) to .gtb/ignore",
		Long: `Append one or more patterns to .gtb/ignore, creating the file with an
explanatory header when it does not yet exist.

Adding is idempotent: re-adding a pattern already present is a reported no-op
and never duplicates a line; existing comments and ordering are preserved. This
does not regenerate — the rule takes effect on the next 'gtb regenerate'.

With --dry-run the resulting file is printed and nothing is written.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)
			out := cmd.OutOrStdout()

			if dryRun {
				return dryRunAdd(out, p, path, args)
			}

			for _, pattern := range args {
				changed, err := generator.AppendIgnorePattern(p.FS, path, pattern)
				if err != nil {
					return err
				}

				if changed {
					_, _ = fmt.Fprintf(out, "added: %s\n", pattern)
				} else {
					_, _ = fmt.Fprintf(out, "already present (no-op): %s\n", pattern)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the resulting .gtb/ignore without writing it")

	return setup.Wrap("", cmd)
}

// dryRunAdd prints the .gtb/ignore that would result from adding patterns,
// composing them in memory so nothing is written.
func dryRunAdd(out io.Writer, p *props.Props, path string, patterns []string) error {
	content, err := generator.PreviewAppendIgnorePatterns(p.FS, path, patterns)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprint(out, content)

	return nil
}

// newCmdIgnoreRemove drops a literal rule line from .gtb/ignore. It matches the
// exact pattern line, not any path the glob happens to match.
func newCmdIgnoreRemove(p *props.Props) *setup.Command {
	var (
		path   string
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "remove <pattern>",
		Short: "Drop a literal rule line from .gtb/ignore",
		Long: `Remove the exact literal rule line matching <pattern> from .gtb/ignore,
preserving every other line (comments, blanks, ordering).

Matching is on the literal rule line, not on a path the pattern happens to
glob — so 'remove justfile' never touches an overlapping '*.yml'. It errors when
no such rule line exists. This does not regenerate.

With --dry-run the resulting file is printed and nothing is written.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)
			out := cmd.OutOrStdout()
			pattern := args[0]

			if dryRun {
				content, changed, err := generator.PreviewRemoveIgnorePattern(p.FS, path, pattern)
				if err != nil {
					return err
				}

				if !changed {
					return errors.Newf("no ignore rule %q found in .gtb/ignore", pattern)
				}

				_, _ = fmt.Fprint(out, content)

				return nil
			}

			changed, err := generator.RemoveIgnorePattern(p.FS, path, pattern)
			if err != nil {
				return err
			}

			if !changed {
				return errors.Newf("no ignore rule %q found in .gtb/ignore", pattern)
			}

			_, _ = fmt.Fprintf(out, "removed: %s\n", pattern)

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the resulting .gtb/ignore without writing it")

	return setup.Wrap("", cmd)
}

// newCmdIgnoreList resolves the active rules against the manifest's tracked
// files: which file each rule governs, and which rules match nothing (stale).
func newCmdIgnoreList(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Resolve rules against the manifest's tracked files",
		Long: `List the active .gtb/ignore rules and resolve them against the files tracked in
.gtb/manifest.yaml: each governed file is shown attributed to the winning rule,
and any rule that currently matches no tracked file is flagged as stale.

This is a read-only view; it never edits the ignore file or regenerates.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path = icmd.ResolveProjectPath(p, path)

			gen := generator.New(p, &generator.Config{Path: path})

			listing, err := gen.ListIgnoreRules()
			if err != nil {
				return err
			}

			printListing(cmd.OutOrStdout(), listing)

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// printListing writes the resolved rule/file view to out.
func printListing(out io.Writer, listing *generator.IgnoreListing) {
	if len(listing.Rules) == 0 {
		_, _ = fmt.Fprintln(out, "No ignore rules configured.")

		return
	}

	if len(listing.Entries) == 0 {
		_, _ = fmt.Fprintln(out, "No tracked files are governed by an ignore rule.")
	} else {
		for _, e := range listing.Entries {
			status := "not ignored"
			if e.Ignored {
				status = "ignored"
			}

			_, _ = fmt.Fprintf(out, "%-40s %-12s (rule: %s)\n", e.Path, status, e.Rule)
		}
	}

	for _, stale := range listing.StaleRules {
		_, _ = fmt.Fprintf(out, "stale rule (matches no tracked file): %s\n", stale)
	}
}

// newCmdIgnoreCheck reports whether path(s) are ignored, naming the rule that
// decides each — correct under last-match-wins + ! negation.
func newCmdIgnoreCheck(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "check <path>...",
		Short: "Report whether path(s) are ignored, and which rule decides",
		Long: `Report, for each given path, whether .gtb/ignore currently ignores it AND which
rule decided that — the winning rule under last-match-wins evaluation, including
a '!' negation that re-includes a file an earlier rule excluded.

This answers "why is this file still being overwritten?", which the flat ignore
file cannot. It reads the ignore file fresh and never edits anything.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)

			gen := generator.New(p, &generator.Config{Path: path})

			printCheck(cmd.OutOrStdout(), gen.CheckIgnorePaths(args))

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// printCheck writes each path's ignore decision and winning rule to out.
func printCheck(out io.Writer, results []generator.IgnoreCheckResult) {
	for _, r := range results {
		status := "not ignored"
		if r.Ignored {
			status = "ignored"
		}

		if !r.Matched {
			_, _ = fmt.Fprintf(out, "%-40s %-12s (no matching rule)\n", r.Path, status)

			continue
		}

		_, _ = fmt.Fprintf(out, "%-40s %-12s (rule: %s)\n", r.Path, status, r.Rule)
	}
}
