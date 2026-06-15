// Package kms is the AWS KMS backend for pkg/signing. Blank-import to
// activate; the init() call registers the backend under the name
// "aws-kms" against pkg/signing's global registry.
//
//	import _ "gitlab.com/phpboyscout/go-tool-base/pkg/signing/kms"
//
// The backend wraps a KMS-held asymmetric RSA SIGN_VERIFY key as a
// crypto.Signer that pkg/openpgpkey can use to mint an OpenPGP-armored
// public key. The private half never leaves AWS — every signing
// operation is a remote kms:Sign call.
//
// Backend-specific CLI flag:
//
//	--kms-region <region>   AWS region. Default: eu-west-2.
//
// Credentials are resolved from the AWS SDK default chain
// (env vars / ~/.aws/credentials / IAM Roles Anywhere / OIDC web
// identity). Users with multiple profiles can either set
// AWS_PROFILE before invoking the minter or assume a role explicitly
// via `aws sts assume-role` and export the resulting credentials.
package kms

import (
	"context"
	"crypto"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
)

const (
	backendName   = "aws-kms"
	defaultRegion = "eu-west-2"
)

// Exported sentinel errors so callers (and tests) can errors.Is
// against specific failure modes.
var (
	// ErrUnsupportedKMSKeyType is returned by NewSigner when the
	// KMS key's public half is not RSA. AWS KMS does not expose
	// Ed25519 for asymmetric signing, so RSA is the only key type
	// this backend handles.
	ErrUnsupportedKMSKeyType = errors.New("KMS key is not RSA; only RSA SIGN_VERIFY keys are supported")

	// ErrUnsupportedHashFunc is returned by the signer when the
	// caller requests a hash function KMS RSA Sign does not map to.
	ErrUnsupportedHashFunc = errors.New("unsupported hash function; KMS RSA Sign accepts SHA-256 / 384 / 512 only")

	// ErrPSSUnsupported is returned by the signer when the caller requests
	// RSASSA-PSS via *rsa.PSSOptions. This backend only implements
	// RSASSA-PKCS1-v1_5; it refuses rather than silently downgrading the
	// signature scheme behind the caller's back.
	ErrPSSUnsupported = errors.New("RSASSA-PSS is not supported; this KMS signer only implements RSASSA-PKCS1-v1_5")
)

// NewSigner is the programmatic constructor for callers that don't
// want to go through the global pkg/signing registry. Most callers
// should use `signing.Get("aws-kms")` instead; this exists for
// integration tests and for tool authors who wire signing into their
// own command structure rather than `gtb keys mint`.
//
// region is the AWS region the key lives in. The AWS SDK default
// credential chain is used; callers manage credentials externally
// (env vars, AWS_PROFILE, assume-role).
func NewSigner(ctx context.Context, region, keyID string) (crypto.Signer, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, errors.Wrap(err, "loading AWS config")
	}

	return newSigner(ctx, kms.NewFromConfig(cfg), keyID)
}

// backend is the Backend implementation registered as "aws-kms".
//
// State held on the value:
//
//   - region is a CLI flag bound by RegisterFlags. The default
//     `eu-west-2` matches the `terraform-aws-signing-kms` module's
//     default region; downstream consumers in other regions
//     override.
//
// NewSigner per-call loads AWS credentials and constructs a fresh
// kms.Client — there is exactly one Sign call per mint, so the
// cost of doing this lazily on demand is negligible and it avoids
// holding AWS credentials longer than the operation requires.
type backend struct {
	region string
}

func (b *backend) Name() string { return backendName }

func (b *backend) RegisterFlags(fs *pflag.FlagSet) {
	fs.StringVar(&b.region, "kms-region", defaultRegion, "AWS region the KMS key lives in")
}

func (b *backend) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
	return NewSigner(ctx, b.region, keyID)
}

func init() {
	signing.Register(&backend{region: defaultRegion})
}
