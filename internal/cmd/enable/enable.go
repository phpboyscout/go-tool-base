// Package enable is the gtb-only `enable` command group. It hosts verbs
// that toggle a generated tool's capabilities and rewrite the existing
// generated wiring, as opposed to `generate`, which adds new user-facing
// surface (commands, flags, config fields).
//
// The first member is `gtb enable signing`, which turns on consumer-side
// release-signing verification: it flips the manifest signing block,
// scaffolds the internal/trustkeys embed package and the generated
// signing.go enforcement defaults, and re-renders the root command to add
// the Signing: field. `gtb disable signing` is the inverse.
//
// This package lives under internal/cmd/ because the commands belong to
// the framework author, not the framework's downstream consumers. See
// docs/development/specs/2026-06-10-signing-generator-feature.md.
package enable

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdEnable returns the top-level `gtb enable` command group with its
// subcommands attached. Mirrors the shape of internal/cmd/keys so the gtb
// root can compose it the same way.
func NewCmdEnable(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable a capability on a generated project",
		Long: `Toggle a generated tool's capabilities, rewriting the generated wiring to match.

Available subcommands:
  signing   Turn on consumer-side release-signing verification: scaffold internal/trustkeys, wire props.Signing, and emit the enforcement defaults.`,
	}

	enableCmd := setup.Wrap("", cmd)
	enableCmd.Register(
		NewCmdEnableSigning(p),
	)

	return enableCmd
}
