// Package detach is the gtb-only `detach` command — the inverse of `attach`. It
// removes a declarative external-command attachment from a generated project's
// manifest and re-renders the root so its wiring is dropped; on a real
// filesystem `go mod tidy` then prunes the now-unused module require.
//
// This package lives under internal/cmd/ because the commands belong to the
// framework author, not downstream consumers. See
// docs/development/specs/2026-07-29-external-command-attachment.md.
package detach

import (
	"fmt"

	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdDetach returns the top-level `gtb detach` command with its command
// subcommand.
func NewCmdDetach(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "detach",
		Short: "Detach external command trees from a generated project's root",
		Long: `Remove a declarative external-command attachment. The manifest entry is dropped
and the root is re-rendered without its wiring; on a real filesystem 'go mod
tidy' then prunes the now-unused module require.

Available subcommands:
  command   Detach an external module's attachment by module path.`,
	}

	group := setup.Wrap("", cmd)
	group.Register(newCmdDetachCommand(p))

	return group
}

func newCmdDetachCommand(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:     "command <module>",
		Short:   "Detach an external module's attachment",
		Example: "  gtb detach command gitlab.com/phpboyscout/go/signing-cli",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			gen := generator.New(p, &generator.Config{
				Path:      icmd.ResolveProjectPath(p, path),
				Overwrite: "allow",
			})

			if err := gen.DetachExternalCommand(cmd.Context(), cmdArgs[0]); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "detached %s\n", cmdArgs[0])

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}
