package kms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
)

// fakeKMS satisfies kmsClient with caller-provided behaviour. Both
// methods record their last input so assertions can pin down the
// algorithm + key-id flowing through the SDK.
type fakeKMS struct {
	getPublicKey func(*kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error)
	sign         func(*kms.SignInput) (*kms.SignOutput, error)

	lastSignInput *kms.SignInput
}

func (f *fakeKMS) GetPublicKey(_ context.Context, in *kms.GetPublicKeyInput, _ ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error) {
	return f.getPublicKey(in)
}

func (f *fakeKMS) Sign(_ context.Context, in *kms.SignInput, _ ...func(*kms.Options)) (*kms.SignOutput, error) {
	f.lastSignInput = in
	return f.sign(in)
}

// rsaPublicKeyDER is a test helper that produces a DER-encoded
// SubjectPublicKeyInfo for a freshly-generated RSA key. Mirrors what
// AWS KMS GetPublicKey returns.
func rsaPublicKeyDER(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)

	return der, priv
}

func ecdsaPublicKeyDER(t *testing.T) []byte {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)

	return der
}

func TestRegistration(t *testing.T) {
	// init() in kms.go registers the backend at package load. The
	// signing.Names() result is goroutine-safe.
	names := signing.Names()
	assert.Contains(t, names, "aws-kms",
		"importing pkg/signing/kms must register the aws-kms backend")

	b, err := signing.Get("aws-kms")
	require.NoError(t, err)
	assert.Equal(t, "aws-kms", b.Name())
}

func TestRegisterFlags_KMSRegion(t *testing.T) {
	b := &backend{region: defaultRegion}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	b.RegisterFlags(fs)

	f := fs.Lookup("kms-region")
	require.NotNil(t, f, "--kms-region must be registered")
	assert.Equal(t, defaultRegion, f.DefValue, "default region must match constant")

	require.NoError(t, fs.Parse([]string{"--kms-region=us-east-1"}))
	assert.Equal(t, "us-east-1", b.region, "parsed --kms-region must update the backend state")
}

func TestNewSigner_RSA_HappyPath(t *testing.T) {
	der, priv := rsaPublicKeyDER(t)

	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return &kms.GetPublicKeyOutput{PublicKey: der}, nil
		},
		sign: func(in *kms.SignInput) (*kms.SignOutput, error) {
			// Echo back a deterministic non-empty signature so the
			// caller can assert the wire-up worked. Real signature
			// validation is exercised end-to-end in the
			// internal/cmd/keys BDD scenarios.
			return &kms.SignOutput{Signature: []byte("test-signature")}, nil
		},
	}

	signer, err := newSigner(context.Background(), fk, "alias/test-key")
	require.NoError(t, err)

	pub, ok := signer.Public().(*rsa.PublicKey)
	require.True(t, ok, "Public() must return *rsa.PublicKey")
	assert.Equal(t, priv.N.Bytes(), pub.N.Bytes(),
		"public modulus must match the KMS-returned public key")

	// Verify Sign maps SHA-256 → SigningAlgorithmSpecRsassaPkcs1V15Sha256.
	digest := make([]byte, 32)
	sig, err := signer.Sign(rand.Reader, digest, crypto.SHA256)
	require.NoError(t, err)
	assert.Equal(t, []byte("test-signature"), sig)
	assert.Equal(t, types.SigningAlgorithmSpecRsassaPkcs1V15Sha256, fk.lastSignInput.SigningAlgorithm)
	assert.Equal(t, types.MessageTypeDigest, fk.lastSignInput.MessageType)
}

func TestNewSigner_NonRSAKey_ReturnsErrUnsupportedKMSKeyType(t *testing.T) {
	der := ecdsaPublicKeyDER(t)

	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return &kms.GetPublicKeyOutput{PublicKey: der}, nil
		},
	}

	_, err := newSigner(context.Background(), fk, "alias/ecdsa-key")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedKMSKeyType,
		"a non-RSA KMS key must surface ErrUnsupportedKMSKeyType")
}

func TestNewSigner_GetPublicKeyError(t *testing.T) {
	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return nil, errors.New("AccessDenied")
		},
	}

	_, err := newSigner(context.Background(), fk, "alias/no-access")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GetPublicKey")
	assert.Contains(t, err.Error(), "AccessDenied")
}

func TestNewSigner_MalformedPublicKeyDER(t *testing.T) {
	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return &kms.GetPublicKeyOutput{PublicKey: []byte("not-a-der")}, nil
		},
	}

	_, err := newSigner(context.Background(), fk, "alias/bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DER")
}

func TestSign_NilOpts(t *testing.T) {
	der, _ := rsaPublicKeyDER(t)
	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return &kms.GetPublicKeyOutput{PublicKey: der}, nil
		},
	}

	s, err := newSigner(context.Background(), fk, "alias/test")
	require.NoError(t, err)

	_, err = s.Sign(rand.Reader, []byte{0}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-nil SignerOpts")
}

func TestSign_UnsupportedHashFunc(t *testing.T) {
	der, _ := rsaPublicKeyDER(t)
	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return &kms.GetPublicKeyOutput{PublicKey: der}, nil
		},
	}

	s, err := newSigner(context.Background(), fk, "alias/test")
	require.NoError(t, err)

	// MD5 — clearly not in the supported set.
	_, err = s.Sign(rand.Reader, []byte{0}, crypto.MD5)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedHashFunc)
}

func TestSign_PSSRequested_ReturnsErrPSSUnsupported(t *testing.T) {
	der, _ := rsaPublicKeyDER(t)
	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return &kms.GetPublicKeyOutput{PublicKey: der}, nil
		},
		sign: func(_ *kms.SignInput) (*kms.SignOutput, error) {
			return &kms.SignOutput{Signature: []byte("should-not-be-reached")}, nil
		},
	}

	s, err := newSigner(context.Background(), fk, "alias/test")
	require.NoError(t, err)

	// Requesting PSS must error explicitly rather than silently signing
	// PKCS#1 v1.5 — a silent scheme downgrade in a crypto.Signer.
	opts := &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto, Hash: crypto.SHA256}
	_, err = s.Sign(rand.Reader, make([]byte, 32), opts)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPSSUnsupported)
	assert.Nil(t, fk.lastSignInput, "KMS Sign must not be called when PSS is requested")
}

func TestSign_HashAlgorithmMapping(t *testing.T) {
	der, _ := rsaPublicKeyDER(t)

	tests := []struct {
		hash crypto.Hash
		want types.SigningAlgorithmSpec
	}{
		{crypto.SHA256, types.SigningAlgorithmSpecRsassaPkcs1V15Sha256},
		{crypto.SHA384, types.SigningAlgorithmSpecRsassaPkcs1V15Sha384},
		{crypto.SHA512, types.SigningAlgorithmSpecRsassaPkcs1V15Sha512},
	}

	for _, tc := range tests {
		t.Run(tc.hash.String(), func(t *testing.T) {
			fk := &fakeKMS{
				getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
					return &kms.GetPublicKeyOutput{PublicKey: der}, nil
				},
				sign: func(_ *kms.SignInput) (*kms.SignOutput, error) {
					return &kms.SignOutput{Signature: []byte("ok")}, nil
				},
			}

			s, err := newSigner(context.Background(), fk, "alias/test")
			require.NoError(t, err)

			_, err = s.Sign(rand.Reader, make([]byte, tc.hash.Size()), tc.hash)
			require.NoError(t, err)
			assert.Equal(t, tc.want, fk.lastSignInput.SigningAlgorithm)
		})
	}
}

func TestSign_KMSError_Wraps(t *testing.T) {
	der, _ := rsaPublicKeyDER(t)
	fk := &fakeKMS{
		getPublicKey: func(_ *kms.GetPublicKeyInput) (*kms.GetPublicKeyOutput, error) {
			return &kms.GetPublicKeyOutput{PublicKey: der}, nil
		},
		sign: func(_ *kms.SignInput) (*kms.SignOutput, error) {
			return nil, errors.New("ThrottlingException")
		},
	}

	s, err := newSigner(context.Background(), fk, "alias/test")
	require.NoError(t, err)

	_, err = s.Sign(rand.Reader, make([]byte, 32), crypto.SHA256)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kms Sign")
	assert.Contains(t, err.Error(), "ThrottlingException")
}
