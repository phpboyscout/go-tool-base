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
	"os"

	"github.com/cockroachdb/errors"
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

// ErrKeyFileExists is returned when a key output file already exists and
// --force was not supplied. Overwriting a private key in place could silently
// destroy the only copy of a signing key, so the default is to refuse.
var ErrKeyFileExists = errors.New("output file already exists")

// writeKeyFile writes data to path with the given permissions, refusing to
// clobber an existing file unless force is set.
//
// The no-clobber guard uses O_EXCL so the existence check and the create are a
// single atomic syscall — there is no TOCTOU window between "does it exist?"
// and "create it". With force, the file is truncated and rewritten. Either way
// the file is created with mode perm (0o600 for private halves), so a fresh
// private key is never world-readable.
func writeKeyFile(path string, data []byte, perm os.FileMode, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	f, err := os.OpenFile(path, flags, perm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.Wrapf(ErrKeyFileExists, "%q (pass --force to overwrite)", path)
		}

		return errors.Wrapf(err, "opening %q for writing", path)
	}

	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return errors.Wrapf(err, "writing %q", path)
	}

	return nil
}

// NewCmdKeys returns the top-level `gtb keys` command group with its
// subcommands attached. Mirrors the shape of internal/cmd/generate's
// constructor so the gtb root can compose it the same way.
func NewCmdKeys(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage OpenPGP keys for release-binary signing",
		Long: `Commands for the cryptographic operations a tool author runs during
release-binary signing setup.

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
