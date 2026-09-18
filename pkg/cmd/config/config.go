// Package config implements the "config" CLI command and its subcommands for
// programmatic read/write access to individual configuration keys. It is
// intended for CI automation and scripted setup rather than interactive use
// (for interactive reconfiguration, use "init <subsystem>" instead).
package config

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// registryLiteralKeys is every plaintext credential key the tool's enabled
// features declare; the pointer keys beside them are deliberately not included.
func registryLiteralKeys(props *p.Props) []string {
	descriptors := credentialposture.DeclaredFor(props.GetFeatures())
	keys := make([]string, 0, len(descriptors))

	for _, d := range descriptors {
		keys = append(keys, d.LiteralKey)
	}

	return keys
}

// NewCmdConfig returns the top-level "config" command with all subcommands
// attached. The masker knows the credential registry's literal keys for the
// tool's enabled features; MaskerOptions extend the built-in sensitive key and
// value patterns, allowing tool authors to register their own credential formats.
func NewCmdConfig(props *p.Props, opts ...MaskerOption) *setup.Command {
	masker := NewMasker(append([]MaskerOption{WithLiteralKeys(registryLiteralKeys(props)...)}, opts...)...)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long: `View, modify, list, and validate configuration settings.

Primarily useful in CI pipelines and scripted workflows to read or write
individual configuration keys without directly editing YAML files.

For interactive guided reconfiguration of a subsystem (AI provider, GitHub
authentication, etc.), use "init <subsystem>" instead.`,
		RunE: setup.GroupRunE,
	}

	configCmd := setup.Wrap(p.ConfigCmd, cmd)
	configCmd.Register(
		setup.AnnotateMCP(setup.Wrap(p.ConfigCmd, NewCmdGet(props, masker)), setup.MCPReadOnly()),
		setup.AnnotateMCP(setup.Wrap(p.ConfigCmd, NewCmdList(props, masker)), setup.MCPReadOnly()),
		setup.AnnotateMCP(setup.Wrap(p.ConfigCmd, NewCmdSet(props)), setup.MCPLocalWrite()),
		setup.AnnotateMCP(setup.Wrap(p.ConfigCmd, NewCmdUnset(props)), setup.MCPLocalWrite()),
		setup.Wrap(p.ConfigCmd, NewCmdPath(props)),
		setup.Wrap(p.ConfigCmd, NewCmdEdit(props)),
		setup.AnnotateMCP(setup.Wrap(p.ConfigCmd, NewCmdValidate(props)), setup.MCPReadOnly()),
		setup.AnnotateMCP(setup.Wrap(p.ConfigCmd, NewCmdMigrate(props)), setup.MCPDestructive()),
		setup.Wrap(p.ConfigCmd, NewCmdTrust(props)),
	)

	return configCmd
}
