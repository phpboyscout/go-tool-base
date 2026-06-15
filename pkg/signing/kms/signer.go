package kms

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cockroachdb/errors"
)

// kmsClient is the subset of *kms.Client this package uses. Lifting
// it as an interface lets the test suite supply a fake without
// pulling in the full AWS SDK mock framework.
type kmsClient interface {
	GetPublicKey(ctx context.Context, in *kms.GetPublicKeyInput, opts ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
	Sign(ctx context.Context, in *kms.SignInput, opts ...func(*kms.Options)) (*kms.SignOutput, error)
}

// kmsSigner adapts AWS KMS Sign to crypto.Signer. The KMS-held private
// key is opaque — the only operations available are GetPublicKey (to
// resolve Public()) and Sign (to satisfy crypto.Signer.Sign).
//
// The shape matches what go-crypto/openpgp expects for the RSA path:
// Public() returns *rsa.PublicKey and Sign() takes a pre-hashed
// digest (because go-crypto pre-hashes via crypto.SignerOpts.HashFunc()
// and passes the digest in).
type kmsSigner struct {
	// ctx is the construction-time context, captured because the
	// crypto.Signer.Sign method this type satisfies has no context
	// parameter. It is used for the KMS Sign RPC in every subsequent
	// Sign call, so its lifetime must outlive the signer: pass a context
	// scoped to the whole signing operation (not a per-request or
	// already-cancelled one) into newSigner. A cancelled ctx fails all
	// later Sign calls.
	ctx    context.Context
	client kmsClient
	keyID  string
	pub    *rsa.PublicKey
}

// newSigner fetches the KMS key's public half via GetPublicKey and
// returns a crypto.Signer ready for use in pkg/openpgpkey.
//
// Only RSA SIGN_VERIFY keys are supported — that is the AWS KMS
// surface for asymmetric signing (Ed25519 is not exposed by KMS).
// Non-RSA public keys returned by KMS yield ErrUnsupportedKMSKeyType.
func newSigner(ctx context.Context, client kmsClient, keyID string) (*kmsSigner, error) {
	out, err := client.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: &keyID})
	if err != nil {
		return nil, errors.Wrap(err, "kms GetPublicKey")
	}

	// KMS returns SubjectPublicKeyInfo DER (X.509). Decode into a
	// typed Go public key.
	parsed, err := x509.ParsePKIXPublicKey(out.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "parsing KMS public-key DER")
	}

	rsaPub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.Wrapf(ErrUnsupportedKMSKeyType, "KMS key is %T", parsed)
	}

	return &kmsSigner{ctx: ctx, client: client, keyID: keyID, pub: rsaPub}, nil
}

// Public implements crypto.Signer.
func (s *kmsSigner) Public() crypto.PublicKey { return s.pub }

// Sign implements crypto.Signer. The `rand` parameter is ignored —
// KMS provides its own RNG. `digest` is the pre-hashed message;
// `opts.HashFunc()` tells us which SHA variant was used so we can
// map to the matching KMS SigningAlgorithmSpec.
//
// Only RSASSA-PKCS1-v1_5 (SHA-256/384/512) is mapped. That covers
// go-crypto/openpgp's RSA signing path — which is the only consumer
// of this signer (the minter only ever issues a single self-signature
// during ArmoredPublicKey). If a caller requests RSASSA-PSS by passing
// *rsa.PSSOptions, Sign returns ErrPSSUnsupported rather than silently
// signing PKCS#1 v1.5 — a silent scheme downgrade in an exported
// crypto.Signer is a contract violation.
func (s *kmsSigner) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if opts == nil {
		return nil, errors.New("kmsSigner.Sign requires non-nil SignerOpts (needs HashFunc to pick KMS algorithm)")
	}

	if _, isPSS := opts.(*rsa.PSSOptions); isPSS {
		return nil, errors.Wrapf(ErrPSSUnsupported, "requested PSS with hash %s", opts.HashFunc())
	}

	alg, err := signingAlgorithm(opts.HashFunc())
	if err != nil {
		return nil, err
	}

	out, err := s.client.Sign(s.ctx, &kms.SignInput{
		KeyId:            &s.keyID,
		Message:          digest,
		MessageType:      types.MessageTypeDigest,
		SigningAlgorithm: alg,
	})
	if err != nil {
		return nil, errors.Wrap(err, "kms Sign")
	}

	return out.Signature, nil
}

// signingAlgorithm maps a crypto.Hash to the matching KMS
// RSASSA-PKCS1-v1_5 SigningAlgorithmSpec. Other hashes return
// ErrUnsupportedHashFunc.
func signingAlgorithm(h crypto.Hash) (types.SigningAlgorithmSpec, error) {
	switch h {
	case crypto.SHA256:
		return types.SigningAlgorithmSpecRsassaPkcs1V15Sha256, nil
	case crypto.SHA384:
		return types.SigningAlgorithmSpecRsassaPkcs1V15Sha384, nil
	case crypto.SHA512:
		return types.SigningAlgorithmSpecRsassaPkcs1V15Sha512, nil
	default:
		return "", errors.Wrapf(ErrUnsupportedHashFunc, "got %s — KMS RSA Sign supports SHA-256 / 384 / 512 only", h)
	}
}
