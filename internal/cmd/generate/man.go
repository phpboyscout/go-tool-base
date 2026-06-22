package generate

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/docs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ManOptions holds the flags for "gtb generate man".
type ManOptions struct {
	Dir     string
	Section string
	Source  string
	Manual  string
	Date    string
}

// NewCmdMan returns the "gtb generate man" subcommand. It renders roff man
// pages for the running binary's own command tree into <dir>/man<section>/, the
// artefact path Linux packagers and CI consume. It documents gtb's own tree;
// a scaffolded downstream tool runs its own "man" command instead.
func NewCmdMan(p *props.Props) *cobra.Command {
	opts := ManOptions{}

	cmd := &cobra.Command{
		Use:   "man",
		Short: "Generate roff man pages for the command tree",
		Long: `Render standards-compliant roff man pages for the entire command tree,
one file per command, written as <dir>/man1/<command-path>.1.

Output is reproducible by default (no date trailer); pass --date to stamp a
build/release date into the .TH header. The pages are the artefact Linux
packages (.deb/.rpm) and Homebrew formulae install under /usr/share/man.

Examples:
  gtb generate man                       # writes ./man/man1/*.1
  gtb generate man --dir dist/man        # custom output directory
  gtb generate man --date 2026-06-21     # stamp a release date
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.Run(cmd, p)
		},
	}

	cmd.Flags().StringVar(&opts.Dir, "dir", "./man", "output directory (pages written under <dir>/man<section>)")
	cmd.Flags().StringVar(&opts.Section, "section", "1", "man section number")
	cmd.Flags().StringVar(&opts.Source, "source", "", "TH source footer (default: <tool> <version>)")
	cmd.Flags().StringVar(&opts.Manual, "manual", "", "TH manual title (default: <Tool> Manual)")
	cmd.Flags().StringVar(&opts.Date, "date", "",
		"stamp this date (YYYY-MM-DD or RFC3339) into the .TH header; default is reproducible with no date trailer")

	return cmd
}

// Run renders the man tree (or lists intended files under --dry-run).
func (o *ManOptions) Run(cmd *cobra.Command, p *props.Props) error {
	root := cmd.Root()

	section := o.Section
	if section == "" {
		section = "1"
	}

	if dryRun {
		return listManFiles(cmd.OutOrStdout(), root, o.Dir, section)
	}

	date, err := parseManDate(o.Date)
	if err != nil {
		return err
	}

	source := o.Source
	if source == "" {
		source = docs.ManSource(root.Name(), p)
	}

	if err := docs.GenerateManTree(root, docs.ManOptions{
		Dir:     o.Dir,
		Section: section,
		Source:  source,
		Manual:  o.Manual,
		Date:    date,
	}); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote man pages to %s\n", filepath.Join(o.Dir, "man"+section))

	return nil
}

// parseManDate parses the --date flag (YYYY-MM-DD or RFC3339). An empty value
// yields nil, which GenerateManTree treats as reproducible-no-date.
func parseManDate(val string) (*time.Time, error) {
	if val == "" {
		return nil, nil //nolint:nilnil // (nil, nil) is the documented "no date" signal.
	}

	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if t, err := time.Parse(layout, val); err == nil {
			return &t, nil
		}
	}

	return nil, errors.Newf("invalid --date %q: use YYYY-MM-DD or RFC3339", val)
}

// listManFiles prints the man-page paths that would be written, for --dry-run.
// Path prediction lives in pkg/docs.ManPagePaths so it stays aligned with the
// real GenerateManTree output.
func listManFiles(w io.Writer, root *cobra.Command, dir, section string) error {
	for _, path := range docs.ManPagePaths(root, docs.ManOptions{Dir: dir, Section: section}) {
		_, _ = fmt.Fprintln(w, path)
	}

	return nil
}
