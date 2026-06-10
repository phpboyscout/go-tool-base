// Package disable is the gtb-only `disable` command group, the inverse
// of internal/cmd/enable. It hosts verbs that turn off a generated
// tool's capabilities and rewrite the generated wiring to match.
//
// The first member is `gtb disable signing`, which turns off
// consumer-side release-signing verification: it flips the manifest
// signing block off, drops the Signing: field from the root command and
// removes the generated signing.go. It never deletes internal/trustkeys
// or author-added *.asc keys.
//
// See docs/development/specs/2026-06-10-signing-generator-feature.md.
package disable

import (
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdDisable returns the top-level `gtb disable` command group with
// its subcommands attached.
func NewCmdDisable(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable a capability on a generated project",
		Long: `Turn off a generated tool's capabilities, rewriting the generated wiring to match.

Available subcommands:
  signing   Turn off release-signing verification: drop props.Signing and the enforcement defaults. Keeps internal/trustkeys and any *.asc keys.`,
	}

	disableCmd := setup.Wrap("", cmd)
	disableCmd.Register(
		newCmdDisableSigning(p),
	)

	return disableCmd
}

type disableSigningOptions struct {
	Path string
}

// newCmdDisableSigning returns the `gtb disable signing` subcommand.
func newCmdDisableSigning(p *props.Props) *setup.Command {
	opts := disableSigningOptions{}

	cmd := &cobra.Command{
		Use:   "signing",
		Short: "Disable consumer-side release-signing verification",
		Long: `Turn off self-update signature verification for a generated project.

Sets properties.signing.enabled = false in .gtb/manifest.yaml, re-renders the
root command to drop the Signing: field, and removes the generated signing.go.

It deliberately leaves internal/trustkeys and any author-added *.asc keys in
place — the generator never deletes author content. Re-run 'gtb enable signing'
to turn verification back on with the same keys.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Path = icmd.ResolveProjectPath(p, opts.Path)

			gen := generator.New(p, &generator.Config{Path: opts.Path, Overwrite: "allow"})
			if err := gen.DisableSigning(cmd.Context()); err != nil {
				return err
			}

			p.Logger.Info("Signing disabled. Removed signing.go and dropped the Signing field from the root command.")
			p.Logger.Info("internal/trustkeys and any *.asc keys were left in place.")

			return nil
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}
