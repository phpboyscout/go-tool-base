// Package disable is the gtb-only `disable` command, the inverse of
// internal/cmd/enable. It turns off a capability of a generated tool and
// rewrites the generated wiring to match.
//
// Simple on/off built-in features (ai, config, telemetry, init, update, mcp,
// docs, doctor, changelog) are a positional argument on `disable` itself —
// `gtb disable doctor` flips properties.features in .gtb/manifest.yaml and
// re-renders the root command. Capabilities with their own configuration are
// scoped subcommands: `gtb disable signing` turns off consumer-side
// release-signing verification (dropping the Signing field and signing.go,
// keeping internal/trustkeys and any *.asc keys).
//
// See docs/development/specs/2026-06-16-enable-disable-features.md and
// docs/development/specs/2026-06-10-signing-generator-feature.md.
package disable

import (
	"strings"

	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdDisable returns the top-level `gtb disable [feature]` command. A
// built-in feature passed positionally is toggled off; a first argument
// matching a scoped subcommand (signing) routes there instead; no argument
// opens the feature picker.
func NewCmdDisable(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "disable [feature...]",
		Short: "Disable a capability on a generated project",
		Long: `Turn off a capability of a generated project, re-rendering the generated
wiring to match.

Pass one or more built-in features as positional arguments to toggle them off:
  ` + strings.Join(generator.ToggleableFeatures, ", ") + `
This flips properties.features in .gtb/manifest.yaml and re-renders the root
command's props.SetFeatures(...). Because the change lives in the manifest it
survives 'gtb regenerate project'. With no feature, an interactive multi-select
of the currently-enabled features is shown (a name is required in CI).

Disabling 'update' removes the self-update subsystem, including the ForcedUpdate
check, so the tool no longer detects or applies new releases.

Capabilities with their own configuration are scoped subcommands:
  signing   Turn off release-signing verification: drop props.Signing and the
            enforcement defaults. Keeps internal/trustkeys and any *.asc keys
            (run 'gtb disable signing --help').
  mcp       With no argument, turn off the mcp feature. With one or more command
            paths, withhold those commands from the MCP tool surface — records
            mcp_enabled: false and re-renders their cmd.go; they stay runnable on
            the CLI (run 'gtb disable mcp --help').`,
		Example: `  gtb disable doctor                  # turn off the doctor feature
  gtb disable doctor docs             # turn off several at once
  gtb disable                         # pick features interactively
  gtb disable signing
  gtb disable mcp post                # withhold the 'post' command from MCP`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return icmd.RunFeatureToggle(cmd, p, path, args, false)
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	disableCmd := setup.Wrap("", cmd)
	disableCmd.Register(
		newCmdDisableSigning(p),
		NewCmdDisableMCP(p),
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
