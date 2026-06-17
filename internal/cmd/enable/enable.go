// Package enable is the gtb-only `enable` command. It turns on a capability of
// a generated tool and rewrites the existing generated wiring to match, as
// opposed to `generate`, which adds new user-facing surface (commands, flags,
// config fields).
//
// Simple on/off built-in features (ai, config, telemetry, init, update, mcp,
// docs, doctor, changelog) are a positional argument on `enable` itself —
// `gtb enable ai` flips properties.features in .gtb/manifest.yaml and re-renders
// the root command. Capabilities with their own configuration are scoped
// subcommands: `gtb enable signing` turns on consumer-side release-signing
// verification with its own flag set. cobra routes a first argument that
// matches a subcommand (signing) there, and otherwise falls through to the
// parent's positional feature handler.
//
// This package lives under internal/cmd/ because the commands belong to the
// framework author, not the framework's downstream consumers. See
// docs/development/specs/2026-06-16-enable-disable-features.md and
// docs/development/specs/2026-06-10-signing-generator-feature.md.
package enable

import (
	"strings"

	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdEnable returns the top-level `gtb enable [feature]` command. A built-in
// feature passed positionally is toggled on; a first argument matching a scoped
// subcommand (signing) routes there instead; no argument opens the feature
// picker.
func NewCmdEnable(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "enable [feature...]",
		Short: "Enable a capability on a generated project",
		Long: `Turn on a capability of a generated project, re-rendering the generated
wiring to match.

Pass one or more built-in features as positional arguments to toggle them on:
  ` + strings.Join(generator.ToggleableFeatures, ", ") + `
This flips properties.features in .gtb/manifest.yaml and re-renders the root
command's props.SetFeatures(...). Because the change lives in the manifest it
survives 'gtb regenerate project'. With no feature, an interactive multi-select
of the currently-disabled features is shown (a name is required in CI).

Capabilities with their own configuration are scoped subcommands:
  signing   Turn on consumer-side release-signing verification: scaffold
            internal/trustkeys, wire props.Signing, and emit the enforcement
            defaults (run 'gtb enable signing --help').`,
		Example: `  gtb enable ai                       # turn on the AI feature
  gtb enable ai config telemetry      # turn on several at once
  gtb enable                          # pick features interactively
  gtb enable signing --email release@example.com`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return icmd.RunFeatureToggle(cmd, p, path, args, true)
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	enableCmd := setup.Wrap("", cmd)
	enableCmd.Register(
		NewCmdEnableSigning(p),
	)

	return enableCmd
}
