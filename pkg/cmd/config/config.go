// Package config implements the "config" CLI command and its subcommands for
// programmatic read/write access to individual configuration keys. It is
// intended for CI automation and scripted setup rather than interactive use
// (for interactive reconfiguration, use "init <subsystem>" instead).
package config

import (
	"github.com/spf13/cobra"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdConfig returns the top-level "config" command with all subcommands
// attached. MaskerOptions extend the built-in sensitive key and value patterns,
// allowing tool authors to register their own credential formats.
func NewCmdConfig(props *p.Props, opts ...MaskerOption) *setup.Command {
	masker := NewMasker(opts...)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long: `View, modify, list, and validate configuration settings.

Primarily useful in CI pipelines and scripted workflows to read or write
individual configuration keys without directly editing YAML files.

For interactive guided reconfiguration of a subsystem (AI provider, GitHub
authentication, etc.), use "init <subsystem>" instead.`,
	}

	configCmd := setup.Wrap(p.ConfigCmd, cmd)
	configCmd.Register(
		setup.Wrap(p.ConfigCmd, NewCmdGet(props, masker)),
		setup.Wrap(p.ConfigCmd, NewCmdList(props, masker)),
		setup.Wrap(p.ConfigCmd, NewCmdSet(props)),
		setup.Wrap(p.ConfigCmd, NewCmdUnset(props)),
		setup.Wrap(p.ConfigCmd, NewCmdPath(props)),
		setup.Wrap(p.ConfigCmd, NewCmdEdit(props)),
		setup.Wrap(p.ConfigCmd, NewCmdValidate(props)),
		setup.Wrap(p.ConfigCmd, NewCmdMigrate(props)),
	)

	return configCmd
}
