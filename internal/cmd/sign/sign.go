// Package sign is the gtb-only `gtb sign` command. Produces an
// ASCII-armored OpenPGP detached signature over a single input file
// using a `crypto.Signer` from the pkg/signing backend registry and
// an existing armored OpenPGP public key as the identity source.
//
// Lives under `internal/cmd/` rather than `pkg/cmd/` because release-
// binary signing is a framework-author concern, not a framework-
// consumer concern.
//
// See docs/development/specs/2026-06-09-sign-command.md.
package sign

import (
	"bytes"
	"crypto"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openpgpkey"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
)

// signatureFilePerm is the on-disk mode for the produced .sig file.
// Signatures are explicitly non-sensitive (public, distributed
// alongside the signed artifact), so world-readable is correct.
const signatureFilePerm = 0o644

// NewCmdSign returns the top-level `gtb sign` command.
func NewCmdSign(p *props.Props) *setup.Command {
	var (
		backendName   string
		keyID         string
		publicKeyPath string
		output        string
		createdRaw    string
	)

	cmd := &cobra.Command{
		Use:   "sign <input-file>",
		Short: "Produce an ASCII-armored OpenPGP detached signature using a configured backend",
		Long: `Sign <input-file> with a signing backend (AWS KMS, local PEM, or any other
backend compiled into this gtb binary) and write the resulting
ASCII-armored OpenPGP detached signature to --output (or <input>.sig
by default).

The private key never leaves the backend — the signing operation is
one round-trip to the HSM/KMS via the crypto.Signer interface.

--public-key points at the armored OpenPGP public-key file (a .asc
previously produced by 'gtb keys mint' and published via WKD /
embedded in your tool's trustkeys). It is the source of truth for the
signing identity: the signature's fingerprint will match this file's
fingerprint. The backend-resolved RSA key must match the public half
in this file or gtb sign refuses to proceed.

Backends are activated at build time by blank-imports in the gtb
binary's main package:

  --backend aws-kms   AWS KMS RSA SIGN_VERIFY keys. --key-id is the
                      KMS key ID, ARN, or alias. Uses the AWS SDK
                      default credential chain — for CI/OIDC, the
                      pipeline sets AWS_ACCESS_KEY_ID /
                      AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN in
                      a before_script via assume-role-with-web-identity.
  --backend local     On-disk PEM-encoded RSA private key.
                      --key-id is the file path.

Examples:

  # Sign checksums.txt with the production KMS key
  gtb sign \
      --backend aws-kms \
      --kms-region eu-west-2 \
      --key-id alias/gtb-release-signing-v1 \
      --public-key release.asc \
      checksums.txt

  # Reproducible signature: pin --created to a fixed RFC3339 instant
  gtb sign \
      --backend local \
      --key-id ./release.pem \
      --public-key ./release.asc \
      --created 2026-01-01T00:00:00Z \
      --output build.txt.sig \
      build.txt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSign(cmd, p, backendName, keyID, publicKeyPath, output, createdRaw, args[0])
		},
	}

	available := strings.Join(signing.Names(), ", ")

	cmd.Flags().StringVar(&backendName, "backend", "",
		fmt.Sprintf("Signing backend name (required). Compiled-in backends: %s", available))
	cmd.Flags().StringVar(&keyID, "key-id", "",
		"Backend-specific key identifier (required). For aws-kms: a KMS key ID, ARN, or alias. For local: a PEM file path.")
	cmd.Flags().StringVar(&publicKeyPath, "public-key", "",
		"Path to the armored OpenPGP public-key file (.asc) that identifies the signing key. Must contain the public half of the key resolved by --backend / --key-id.")
	cmd.Flags().StringVar(&output, "output", "",
		"Output file path for the armored detached signature. Defaults to <input>.sig.")
	cmd.Flags().StringVar(&createdRaw, "created", "",
		"Signature creation time RFC3339 (default now, truncated to whole seconds). Pin to produce byte-identical signatures across re-runs.")

	for _, who := range []string{"backend", "key-id", "public-key"} {
		_ = cmd.MarkFlagRequired(who)
	}

	for _, n := range signing.Names() {
		b, err := signing.Get(n)
		if err != nil {
			panic(err) // unreachable
		}

		b.RegisterFlags(cmd.Flags())
	}

	return setup.Wrap("", cmd)
}

func runSign(cmd *cobra.Command, p props.LoggerProvider, backendName, keyID, publicKeyPath, output, createdRaw, input string) error {
	sigCreationTime, output, err := resolveSignOptions(input, output, createdRaw)
	if err != nil {
		return err
	}

	publicKey, signer, err := loadSignInputs(cmd, backendName, keyID, publicKeyPath)
	if err != nil {
		return err
	}

	sig, err := signFile(signer, publicKey, input, sigCreationTime)
	if err != nil {
		return err
	}

	if err := writeSignature(output, sig); err != nil {
		return err
	}

	logSignOutcome(p, backendName, keyID, publicKey, publicKeyPath, input, output, sigCreationTime)

	return nil
}

// resolveSignOptions normalises the option flags: parses --created,
// derives the default --output, and rejects an --output that would
// overwrite the input.
func resolveSignOptions(input, output, createdRaw string) (time.Time, string, error) {
	sigCreationTime, err := parseCreatedTime(createdRaw)
	if err != nil {
		return time.Time{}, "", err
	}

	if output == "" {
		output = input + ".sig"
	}

	if sameSignPath(output, input) {
		return time.Time{}, "", errors.Newf("refusing to write signature to %q which equals the input path %q", output, input)
	}

	return sigCreationTime, output, nil
}

// sameSignPath reports whether output and input name the same file, so
// the clobber guard isn't bypassed by a different spelling of the same
// path ("./x" vs "x" vs an absolute form, or two symlinks/hardlinks to
// one inode). It first compares cleaned strings (catches the common
// spelling cases without touching the filesystem), then falls back to
// os.SameFile when both paths already exist on disk (catches links and
// case/normalisation differences the string compare misses).
func sameSignPath(output, input string) bool {
	if filepath.Clean(output) == filepath.Clean(input) {
		return true
	}

	outInfo, outErr := os.Stat(output)
	inInfo, inErr := os.Stat(input)

	if outErr != nil || inErr != nil {
		return false
	}

	return os.SameFile(outInfo, inInfo)
}

// loadSignInputs reads the armored public-key file and constructs
// the backend-resolved signer.
func loadSignInputs(cmd *cobra.Command, backendName, keyID, publicKeyPath string) ([]byte, crypto.Signer, error) {
	//nolint:gosec // G304: operator-supplied --public-key path, cleaned before read; a fixed path would defeat the command
	publicKey, err := os.ReadFile(filepath.Clean(publicKeyPath))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "reading public key %s", publicKeyPath)
	}

	b, err := signing.Get(backendName)
	if err != nil {
		return nil, nil, err
	}

	signer, err := b.NewSigner(cmd.Context(), keyID)
	if err != nil {
		return nil, nil, errors.Wrap(err, "constructing signer")
	}

	return publicKey, signer, nil
}

// signFile streams the input file through openpgpkey.DetachSign.
func signFile(signer crypto.Signer, publicKey []byte, input string, sigCreationTime time.Time) ([]byte, error) {
	//nolint:gosec // G304: operator-supplied <input> path, cleaned before open; a fixed path would defeat the command
	in, err := os.Open(filepath.Clean(input))
	if err != nil {
		return nil, errors.Wrapf(err, "opening %s", input)
	}
	// Best-effort close on a read-only file: the read result is already
	// consumed by DetachSign, so a close error is not actionable.
	defer func() { _ = in.Close() }()

	sig, err := openpgpkey.DetachSign(signer, publicKey, in, sigCreationTime)
	if err != nil {
		return nil, errors.Wrap(err, "computing signature")
	}

	return sig, nil
}

// writeSignature persists the .sig bytes. Cleans the path so gosec is
// happy with the upstream taint chain (command-line argument →
// filesystem write).
func writeSignature(output string, sig []byte) error {
	if err := os.WriteFile(filepath.Clean(output), sig, signatureFilePerm); err != nil {
		return errors.Wrapf(err, "writing %s", output)
	}

	return nil
}

// logSignOutcome emits the structured INFO line operators rely on to
// confirm the signing identity without invoking gpg --verify.
func logSignOutcome(p props.LoggerProvider, backendName, keyID string, publicKey []byte, publicKeyPath, input, output string, sigCreationTime time.Time) {
	logArgs := []any{
		"backend", backendName,
		"key_id", keyID,
		"public_key", publicKeyPath,
		"input", input,
		"output", output,
		"sig_creation_time", sigCreationTime.Format(time.RFC3339),
	}

	if fp, fpErr := publicKeyFingerprint(publicKey); fpErr == nil {
		logArgs = append(logArgs, "fingerprint", fp)
	}

	p.GetLogger().Info("Signed file", logArgs...)
}

// parseCreatedTime parses --created. Empty defaults to now,
// truncated to whole seconds (OpenPGP v4 sig packets store creation
// time as uint32 seconds since epoch; surfacing the truncation in
// the log line lets operators see exactly what landed in the packet).
func parseCreatedTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Now().UTC().Truncate(time.Second), nil
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "parsing --created %q as RFC3339", raw)
	}

	return t.UTC().Truncate(time.Second), nil
}

// publicKeyFingerprint extracts the primary-key fingerprint from an
// armored OpenPGP public-key block. Used by runSign's log line so
// the operator sees the signing identity without a follow-up
// gpg --show-key call.
func publicKeyFingerprint(armored []byte) (string, error) {
	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	if err != nil {
		return "", err
	}

	if len(ring) != 1 {
		return "", errors.Newf("expected one entity, got %d", len(ring))
	}

	return strings.ToUpper(fmt.Sprintf("%X", ring[0].PrimaryKey.Fingerprint)), nil
}
