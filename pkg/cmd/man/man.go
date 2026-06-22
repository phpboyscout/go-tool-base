// Package man implements the hidden, opt-in "man" command that emits roff man
// pages for a tool's own command tree at runtime — for packaging postinstall
// scripts or ad-hoc preview — without re-running the source-tree generator.
// It is gated behind the default-off props.ManCmd feature.
package man

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/docs"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdMan returns the hidden runtime "man" command, gated behind the
// default-off ManCmd feature. With --dir it writes the full tree under
// <dir>/man1; without it, the running tool's top-level page is printed to
// stdout for preview (e.g. "mytool man | man -l -").
func NewCmdMan(props *p.Props) *setup.Command {
	var dir string

	cmd := &cobra.Command{
		Use:    "man",
		Short:  "Generate or preview roff man pages for this tool",
		Hidden: true,
		Long: `Emit roff man pages for this tool's own command tree.

With --dir, the full tree is written under <dir>/man1/<command-path>.1 (for a
packaging postinstall step). Without --dir, the tool's top-level page is printed
to stdout so it can be previewed, e.g. "mytool man | man -l -".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := cmd.Root()
			opts := docs.ManOptions{Source: docs.ManSource(root.Name(), props)}

			if dir != "" {
				opts.Dir = dir

				return docs.GenerateManTree(root, opts)
			}

			return docs.GenerateManPage(cmd.OutOrStdout(), root, opts)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"write the full man tree under <dir>/man1 instead of printing to stdout")

	return setup.Wrap(p.ManCmd, cmd)
}
