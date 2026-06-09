// Package keys is the gtb-only `keys` command group. Houses
// subcommands for the cryptographic operations a tool author runs
// during release-binary signing setup: mint an OpenPGP key from an
// existing signer, generate a fresh keypair locally, and (in future
// revisions) verify, fingerprint, import-from-wkd, etc.
//
// This package lives under `internal/cmd/` rather than `pkg/cmd/`
// because the commands belong to the framework author, not the
// framework's downstream consumers — a scaffolded `mytool` whose
// users build a CLI for managing customer databases has no reason
// to expose `mytool keys mint`.
//
// See docs/development/specs/2026-06-08-keys-mint-command.md.
package keys

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// File permissions for command outputs. Public-key (armored OpenPGP)
// files are world-readable because they are explicitly not sensitive
// — distributing them is the whole point. Private-key files are
// owner-read/write only.
const (
	publicKeyFilePerm  = 0o644
	privateKeyFilePerm = 0o600
)

// NewCmdKeys returns the top-level `gtb keys` command group with its
// subcommands attached. Mirrors the shape of internal/cmd/generate's
// constructor so the gtb root can compose it the same way.
func NewCmdKeys(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage OpenPGP keys for release-binary signing",
		Long: `Commands for the cryptographic operations a tool author runs during release-binary signing setup.

Available subcommands:
  mint      Wrap an existing signer (KMS or local PEM) in OpenPGP framing and emit the armored public half.
  generate  Generate a fresh keypair locally (Ed25519 or RSA) and emit both halves.
  wkd       Generate a Web Key Directory tree from one or more public keys (for static hosting).

See https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/docs/development/specs/2026-06-08-keys-mint-command.md for the full design.`,
	}

	keysCmd := setup.Wrap("", cmd)
	keysCmd.Register(
		NewCmdKeysMint(p),
		NewCmdKeysGenerate(p),
		NewCmdKeysWKD(p),
	)

	return keysCmd
}
