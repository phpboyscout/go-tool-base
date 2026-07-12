package keys

import (
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go/signing"
	"gitlab.com/phpboyscout/go/signing/openpgpkey"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdKeysMint returns the `gtb keys mint` subcommand. Wraps an
// existing signer (KMS or local PEM file) in OpenPGP framing and
// writes the armored public half to a file.
func NewCmdKeysMint(p *props.Props) *setup.Command {
	var (
		backendName string
		keyID       string
		name        string
		email       string
		output      string
		createdRaw  string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "mint",
		Short: "Mint an ASCII-armored OpenPGP public key from an existing signer",
		Long: `Wrap an existing signer (HSM-held key or on-disk PEM private key) in
OpenPGP framing and write the armored public half to a file. The
private half never touches this tool — every signing operation flows
through the chosen backend.

Backends are activated at build time by blank-imports in the gtb
binary's main package. The standard gtb binary ships with:

  --backend aws-kms   AWS KMS RSA SIGN_VERIFY keys. --key-id is the
                      KMS key ID, ARN, or alias. Uses the AWS SDK
                      default credential chain.
  --backend local     On-disk PEM-encoded RSA private key.
                      --key-id is the file path.

Other backends (GCP KMS, Vault, etc.) can be added by anyone consuming
pkg/signing. See docs/how-to/add-signing-backend.md.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMint(cmd, p, backendName, keyID, name, email, output, createdRaw, force)
		},
	}

	available := strings.Join(signing.Names(), ", ")

	cmd.Flags().StringVar(&backendName, "backend", "",
		fmt.Sprintf("Signing backend name (required). Compiled-in backends: %s", available))
	cmd.Flags().StringVar(&keyID, "key-id", "",
		"Backend-specific key identifier (required). For aws-kms: a KMS key ID, ARN, or alias. For local: a PEM file path.")
	cmd.Flags().StringVar(&name, "name", "",
		`OpenPGP user-id real name (required; e.g. "GTB Release").`)
	cmd.Flags().StringVar(&email, "email", "",
		"OpenPGP user-id email (required; e.g. release@example.com).")
	cmd.Flags().StringVar(&output, "output", "release.asc",
		"Output file path for the armored public key.")
	cmd.Flags().StringVar(&createdRaw, "created", "",
		"Creation time RFC3339 (default now). Pin only on re-mint of an existing key — different creation times produce different fingerprints.")
	cmd.Flags().BoolVar(&force, "force", false,
		"Overwrite an existing output file. Without this, mint refuses to clobber an existing file.")

	for _, who := range []string{"backend", "key-id", "name", "email"} {
		_ = cmd.MarkFlagRequired(who)
	}

	// Each registered backend declares its own flags onto the same
	// flag set so they appear in `--help` and parse alongside the
	// generic flags. A backend whose flag conflicts with a flag from
	// another backend is a programmer error and surfaces here.
	for _, n := range signing.Names() {
		b, err := signing.Get(n)
		if err != nil {
			panic(err) // unreachable — Names() returned this entry
		}

		// Backends that need per-backend flags implement this optional
		// interface (the signing.Backend contract is CLI-agnostic).
		if fr, ok := b.(interface{ RegisterFlags(*pflag.FlagSet) }); ok {
			fr.RegisterFlags(cmd.Flags())
		}
	}

	return setup.Wrap("", cmd)
}

func runMint(cmd *cobra.Command, p props.LoggerProvider, backendName, keyID, name, email, output, createdRaw string, force bool) error {
	b, err := signing.Get(backendName)
	if err != nil {
		return err
	}

	creationTime, err := parseCreatedTime(createdRaw)
	if err != nil {
		return err
	}

	if output == "-" {
		// Spec D7 / Resolution 3: defaulting to file, refusing stdout
		// avoids interleaving the armored bytes with log lines.
		return errors.New(`--output "-" (stdout) is not supported; pass a file path`)
	}

	signer, err := b.NewSigner(cmd.Context(), keyID)
	if err != nil {
		return errors.Wrap(err, "constructing signer")
	}

	armored, err := openpgpkey.ArmoredPublicKey(signer, name, email, creationTime)
	if err != nil {
		return errors.Wrap(err, "minting armored public key")
	}

	if err := writeKeyFile(output, armored, publicKeyFilePerm, force); err != nil {
		return errors.Wrap(err, "writing output")
	}

	fp, err := readFingerprint(armored)
	if err != nil {
		// Don't fail the mint — we wrote the file; the operator can
		// recover the fingerprint with gpg --show-key. But log the
		// inability to read it back ourselves.
		p.GetLogger().Warn("Wrote armored key but could not parse it back to read fingerprint", "error", err, "output", output)
	} else {
		p.GetLogger().Info("Minted OpenPGP key",
			"backend", backendName,
			"key_id", keyID,
			"output", output,
			"creation_time", creationTime.Format(time.RFC3339),
			"fingerprint", fp,
		)
	}

	return nil
}

// parseCreatedTime parses an RFC3339 timestamp from the --created
// flag's string value. An empty string returns time.Now().UTC().
func parseCreatedTime(s string) (time.Time, error) {
	if s == "" {
		return time.Now().UTC(), nil
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "parsing --created (RFC3339 expected)")
	}

	return t.UTC(), nil
}

// readFingerprint parses an armored public key and returns its
// primary key's fingerprint as a hex string. Returns an error if the
// armored bytes don't represent a single OpenPGP entity (i.e. the
// minter produced something unparseable — a regression).
func readFingerprint(armored []byte) (string, error) {
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(string(armored)))
	if err != nil {
		return "", err
	}

	if len(ring) != 1 {
		return "", errors.Newf("expected 1 entity, got %d", len(ring))
	}

	return fmt.Sprintf("%X", ring[0].PrimaryKey.Fingerprint), nil
}
