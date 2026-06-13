package keys

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openpgpkey"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

const (
	algoEd25519 = "ed25519"
	algoRSA     = "rsa"

	defaultRSABits = 4096
)

// NewCmdKeysGenerate returns the `gtb keys generate` subcommand.
// Generates a fresh keypair entirely in-process (no shell-out, no
// external dependencies) and writes both halves to disk. Used during
// onboarding for the rotation-authority key and the tutorial / local
// signing key.
func NewCmdKeysGenerate(p *props.Props) *setup.Command {
	var (
		algorithm     string
		rsaBits       int
		name          string
		email         string
		output        string
		privateOutput string
		createdRaw    string
		force         bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a fresh keypair locally (Ed25519 or RSA) and emit both halves",
		Long: `Generate a fresh keypair entirely inside the gtb process. The private
half is written to disk in a format chosen per algorithm:

  --algorithm ed25519   Public half: armored OpenPGP. Private half:
                        armored OpenPGP secret-key block (the same
                        wire format gpg --export-secret-keys uses).
                        Used for rotation-authority keys. Move the
                        private half to offline storage immediately.

  --algorithm rsa       Public half: armored OpenPGP. Private half:
                        PKCS#1 PEM. Pairs with
                        ` + "`gtb keys mint --backend local`" + ` for the tutorial
                        path. For production signing, use AWS KMS via
                        ` + "`gtb keys mint --backend aws-kms`" + ` instead.

Encrypted on-disk private halves are not produced in v0.1 — the
standard library doesn't expose PKCS#8 encryption and a clean
OpenPGP s2k path is additive. Use filesystem-level encryption (LUKS,
FileVault, age) until v0.2 adds in-tool encryption.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGenerate(cmd, p, algorithm, rsaBits, name, email, output, privateOutput, createdRaw, force)
		},
	}

	cmd.Flags().StringVar(&algorithm, "algorithm", "",
		fmt.Sprintf("Key algorithm (required): %q or %q", algoEd25519, algoRSA))
	cmd.Flags().IntVar(&rsaBits, "rsa-bits", defaultRSABits,
		"RSA modulus size in bits. Ignored for --algorithm ed25519. 2048/3072/4096 accepted; 4096 is the default.")
	cmd.Flags().StringVar(&name, "name", "",
		`OpenPGP user-id real name (required).`)
	cmd.Flags().StringVar(&email, "email", "",
		"OpenPGP user-id email (required).")
	cmd.Flags().StringVar(&output, "output", "",
		"Output file path for the armored public key. Default: <algorithm>.asc.")
	cmd.Flags().StringVar(&privateOutput, "private-output", "",
		"Output file path for the private half. Default: derived from --output by replacing the extension (.asc → .priv.asc for Ed25519, .asc → .pem for RSA).")
	cmd.Flags().StringVar(&createdRaw, "created", "",
		"Creation time RFC3339 (default now). Pin only when re-deriving an existing key — different creation times produce different fingerprints.")
	cmd.Flags().BoolVar(&force, "force", false,
		"Overwrite existing output files. Without this, generate refuses to clobber an existing key to avoid destroying a private key in place.")

	for _, who := range []string{"algorithm", "name", "email"} {
		_ = cmd.MarkFlagRequired(who)
	}

	return setup.Wrap("", cmd)
}

func runGenerate(_ *cobra.Command, p *props.Props, algorithm string, rsaBits int, name, email, output, privateOutput, createdRaw string, force bool) error {
	creationTime, err := parseCreatedTime(createdRaw)
	if err != nil {
		return err
	}

	pubPath, privPath, err := resolveOutputs(algorithm, output, privateOutput)
	if err != nil {
		return err
	}

	// Build the OpenPGP entity. Two paths:
	//
	//   - RSA: generate a stdlib *rsa.PrivateKey and wrap it via
	//     pkg/openpgpkey.Entity. This shares the same code path as
	//     `gtb keys mint`, ensuring the resulting entity is
	//     bit-for-bit comparable with a re-mint.
	//
	//   - Ed25519: use openpgp.NewEntity with PubKeyAlgoEdDSA. This
	//     produces a v4-EdDSA entity (algorithm 22) that GnuPG 2.4
	//     and older can import. go-crypto's `internal/ecc` package
	//     is unreachable from outside the module, so we cannot
	//     construct a v4-EdDSA entity from an externally-generated
	//     ed25519.PrivateKey; letting NewEntity generate the key
	//     itself is the only practical route.
	ent, signer, err := buildEntity(algorithm, rsaBits, name, email, creationTime)
	if err != nil {
		return err
	}

	pubArmored, err := serializeArmored(ent, openpgp.PublicKeyType, false)
	if err != nil {
		return errors.Wrap(err, "armoring public half")
	}

	// Encode the private half before writing anything so a failure there does
	// not leave a lone public key behind. Write the private half first under
	// the no-clobber guard: it is the irreplaceable artefact, so refusing to
	// overwrite it is the highest-value protection.
	privBytes, err := encodePrivateHalf(algorithm, ent, signer)
	if err != nil {
		return err
	}

	if err := writeKeyFile(privPath, privBytes, privateKeyFilePerm, force); err != nil {
		return errors.Wrap(err, "writing private-half output")
	}

	if err := writeKeyFile(pubPath, pubArmored, publicKeyFilePerm, force); err != nil {
		return errors.Wrap(err, "writing public-half output")
	}

	fp := fmt.Sprintf("%X", ent.PrimaryKey.Fingerprint)

	p.Logger.Info("Generated OpenPGP keypair",
		"algorithm", algorithm,
		"public_output", pubPath,
		"private_output", privPath,
		"creation_time", creationTime.Format(time.RFC3339),
		"fingerprint", fp,
	)
	p.Logger.Warn("Move the private-half file to offline storage now.",
		"private_output", privPath,
	)

	return nil
}

// buildEntity dispatches keypair generation by algorithm. Returns
// the OpenPGP entity plus the underlying signer (used by the RSA path
// to produce a PKCS#1 PEM for the on-disk private half).
func buildEntity(algorithm string, rsaBits int, name, email string, creationTime time.Time) (*openpgp.Entity, crypto.Signer, error) {
	switch algorithm {
	case algoRSA:
		if rsaBits != 2048 && rsaBits != 3072 && rsaBits != 4096 {
			return nil, nil, errors.Newf("--rsa-bits must be 2048, 3072, or 4096 (got %d)", rsaBits)
		}

		priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
		if err != nil {
			return nil, nil, errors.Wrap(err, "generating rsa key")
		}

		ent, err := openpgpkey.Entity(priv, name, email, creationTime)
		if err != nil {
			return nil, nil, errors.Wrap(err, "building openpgp entity")
		}

		return ent, priv, nil

	case algoEd25519:
		// PubKeyAlgoEdDSA + default curve (Curve25519) = v4 EdDSA,
		// algorithm 22 in OpenPGP. GnuPG 2.4 imports this cleanly.
		// NewEntity generates the key internally; we don't have a
		// stdlib *ed25519.PrivateKey for the result.
		cfg := &packet.Config{
			Algorithm: packet.PubKeyAlgoEdDSA,
			Time:      func() time.Time { return creationTime },
		}

		ent, err := openpgp.NewEntity(name, "", email, cfg)
		if err != nil {
			return nil, nil, errors.Wrap(err, "generating v4 EdDSA entity")
		}

		return ent, nil, nil

	default:
		return nil, nil, errors.Newf("unknown algorithm %q (expected %q or %q)", algorithm, algoEd25519, algoRSA)
	}
}

// resolveOutputs computes the public and private output paths from
// the (possibly empty) --output and --private-output flags.
// Defaults:
//
//   - public:  "<algorithm>.asc"
//   - private: derived from public by replacing the extension —
//     ".asc" → ".priv.asc" for Ed25519 (armored OpenPGP),
//     ".asc" → ".pem" for RSA (PKCS#1 PEM).
//
// Refuses to let --output equal --private-output (would overwrite).
func resolveOutputs(algorithm, output, privateOutput string) (string, string, error) {
	if output == "" {
		output = algorithm + ".asc"
	}

	if privateOutput == "" {
		base := strings.TrimSuffix(output, filepath.Ext(output))

		switch algorithm {
		case algoEd25519:
			privateOutput = base + ".priv.asc"
		case algoRSA:
			privateOutput = base + ".pem"
		default:
			return "", "", errors.Newf("unknown algorithm %q (expected %q or %q)", algorithm, algoEd25519, algoRSA)
		}
	}

	if output == privateOutput {
		return "", "", errors.Newf("--output (%s) must differ from --private-output (%s)", output, privateOutput)
	}

	return output, privateOutput, nil
}

// encodePrivateHalf returns the on-disk encoding of the private half.
//
//   - Ed25519: ASCII-armored OpenPGP secret-key block via
//     Entity.SerializePrivate — compatible with `gpg --import`. signer
//     is nil because buildEntity's Ed25519 path generates the key
//     inside go-crypto; the secret material lives on the entity.
//   - RSA: PKCS#1 PEM, consumed by pkg/signing/local. signer holds
//     the same *rsa.PrivateKey that's inside the entity; using it
//     directly avoids round-tripping through OpenPGP packets.
func encodePrivateHalf(algorithm string, ent *openpgp.Entity, signer crypto.Signer) ([]byte, error) {
	switch algorithm {
	case algoEd25519:
		return serializeArmored(ent, openpgp.PrivateKeyType, true)
	case algoRSA:
		priv, ok := signer.(*rsa.PrivateKey)
		if !ok {
			// Unreachable — buildEntity's RSA branch always returns
			// *rsa.PrivateKey as the second return value.
			return nil, errors.Newf("expected *rsa.PrivateKey for RSA path, got %T", signer)
		}

		var out bytes.Buffer
		if err := pem.Encode(&out, &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(priv),
		}); err != nil {
			return nil, errors.Wrap(err, "encoding RSA PKCS#1 PEM")
		}

		return out.Bytes(), nil
	default:
		// Unreachable — buildEntity would have caught this.
		return nil, errors.Newf("unsupported algorithm %q for private encoding", algorithm)
	}
}

// serializeArmored writes an entity to an ASCII-armored buffer. When
// includePrivate is true the entity's secret material is serialised
// via Entity.SerializePrivate; otherwise only the public half is
// emitted via Entity.Serialize.
func serializeArmored(ent *openpgp.Entity, armorType string, includePrivate bool) ([]byte, error) {
	var buf bytes.Buffer

	enc, err := armor.Encode(&buf, armorType, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if includePrivate {
		if err := ent.SerializePrivate(enc, nil); err != nil {
			return nil, errors.Wrap(err, "serializing private half")
		}
	} else {
		if err := ent.Serialize(enc); err != nil {
			return nil, errors.Wrap(err, "serializing public half")
		}
	}

	if err := enc.Close(); err != nil {
		return nil, errors.WithStack(err)
	}

	return buf.Bytes(), nil
}
