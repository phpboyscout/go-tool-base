// Package local is the on-disk PEM-encoded RSA private key backend for
// pkg/signing. Blank-import to activate; the init() call registers
// the backend under the name "local" against pkg/signing's global
// registry.
//
//	import _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/local"
//
// The backend reads a PEM file from disk (path supplied via the
// command-line --key-id) and returns its *rsa.PrivateKey as a
// crypto.Signer. Pairs with `gtb keys generate --algorithm rsa` for
// the tutorial / no-cloud-KMS path — the generate command produces a
// PEM private half that this backend consumes.
//
// Supported PEM formats:
//
//   - Unencrypted PKCS#1 RSA private key (header
//     "-----BEGIN RSA PRIVATE KEY-----").
//   - Unencrypted PKCS#8 (header "-----BEGIN PRIVATE KEY-----").
//
// Encrypted-PKCS#8 PEMs are not supported in v0.1 — the standard
// library does not expose PKCS#8 decryption. Operators who need
// at-rest encryption should either use the aws-kms backend (recommended
// for production) or encrypt the PEM file at the filesystem layer
// (LUKS, FileVault, age). Adding encrypted-PKCS#8 support is additive
// and slated for v0.2 if a real consumer asks.
//
// Non-RSA keys in the PEM file are rejected — Ed25519 / ECDSA in PEM
// flowing through this backend is unsupported in v0.1 (the only
// consumer is `gtb keys mint`, which currently only mints RSA via
// this path).
package local

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
)

const backendName = "local"

// Exported sentinel errors so callers (and tests) can errors.Is
// against specific failure modes.
var (
	// ErrUnsupportedKeyType is returned when the PEM file contains
	// a non-RSA key (Ed25519, ECDSA, etc.). v0.1 only supports RSA
	// via the local backend.
	ErrUnsupportedKeyType = errors.New("PEM key is not RSA; only RSA private keys are supported")

	// ErrMissingPEMBlock is returned when the file decodes to zero
	// PEM blocks (e.g. the file is empty or contains only comments).
	ErrMissingPEMBlock = errors.New("no PEM block found in file")

	// ErrEncryptedPEMUnsupported is returned when the PEM block is
	// encrypted — the standard library does not expose a clean PKCS#8
	// decryption path. v0.1 operators who need encryption use the
	// aws-kms backend or filesystem-level encryption.
	ErrEncryptedPEMUnsupported = errors.New("encrypted PEM private keys are not supported in v0.1; decrypt out-of-band first or use the aws-kms backend")
)

// NewSigner is the programmatic constructor for callers that bypass
// the global pkg/signing registry. The path is the on-disk PEM file.
// Most callers should use `signing.Get("local")` instead; this exists
// for integration tests and ad-hoc programmatic use.
func NewSigner(_ context.Context, path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied; reading the file they pointed at is the whole point
	if err != nil {
		return nil, errors.Wrap(err, "reading PEM file")
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.Wrap(ErrMissingPEMBlock, path)
	}

	if block.Type == "ENCRYPTED PRIVATE KEY" {
		return nil, ErrEncryptedPEMUnsupported
	}

	return parseRSAKey(block)
}

// parseRSAKey decodes block.Bytes to an *rsa.PrivateKey. Handles
// PKCS#1 ("RSA PRIVATE KEY") and unencrypted PKCS#8 ("PRIVATE KEY")
// formats. Non-RSA keys in PKCS#8 yield ErrUnsupportedKeyType;
// unrecognised PEM types yield ErrMissingPEMBlock.
func parseRSAKey(block *pem.Block) (*rsa.PrivateKey, error) {
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.Wrap(err, "parsing PKCS#1 RSA key")
		}

		return key, nil

	case "PRIVATE KEY":
		anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.Wrap(err, "parsing PKCS#8 key")
		}

		rsaKey, ok := anyKey.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.Wrapf(ErrUnsupportedKeyType, "PKCS#8 holds %T", anyKey)
		}

		return rsaKey, nil

	default:
		return nil, errors.Wrapf(ErrMissingPEMBlock, "unsupported PEM block type %q", block.Type)
	}
}

// backend is the Backend implementation registered as "local".
// Stateless — the PEM file path is supplied per-call as keyID; this
// backend declares no CLI flags.
type backend struct{}

func (b *backend) Name() string                   { return backendName }
func (b *backend) RegisterFlags(_ *pflag.FlagSet) {}

func (b *backend) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
	return NewSigner(ctx, keyID)
}

func init() {
	signing.Register(&backend{})
}
