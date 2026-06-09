package openpgpkey_test

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openpgpkey"
)

// signerOpaque wraps an *rsa.PrivateKey but exposes only the
// crypto.Signer interface — its concrete type is not *rsa.PrivateKey
// to outside callers. This simulates a KMS-backed signer: if
// DetachSign works through this, it works against real KMS.
type signerOpaque struct{ inner *rsa.PrivateKey }

func (s signerOpaque) Public() crypto.PublicKey { return &s.inner.PublicKey }
func (s signerOpaque) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return s.inner.Sign(r, digest, opts)
}

const (
	testSignerName  = "PHP Boy Scout Release"
	testSignerEmail = "release@phpboyscout.uk"
)

var (
	fixedKeyTime = time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	fixedSigTime = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
)

// buildSigningPair produces a concrete RSA key plus the armored
// public-key block that DetachSign and verifiers consume.
func buildSigningPair(t *testing.T) (priv *rsa.PrivateKey, pubArmored []byte) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubArmored, err = openpgpkey.ArmoredPublicKey(priv, testSignerName, testSignerEmail, fixedKeyTime)
	require.NoError(t, err)

	return priv, pubArmored
}

func TestDetachSign_ConcreteRSA_VerifiesAgainstMatchingKey(t *testing.T) {
	t.Parallel()

	priv, pubArmored := buildSigningPair(t)
	data := []byte("checksums file content under signature\n")

	sig, err := openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader(data), fixedSigTime)
	require.NoError(t, err)

	assert.True(t, bytes.HasPrefix(sig, []byte("-----BEGIN PGP SIGNATURE-----")),
		"signature must be ASCII-armored")

	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubArmored))
	require.NoError(t, err)

	signer, err := openpgp.CheckArmoredDetachedSignature(ring, bytes.NewReader(data), bytes.NewReader(sig), nil)
	require.NoError(t, err, "verifier must accept the signature")
	require.NotNil(t, signer)

	assert.Equal(t, ring[0].PrimaryKey.Fingerprint, signer.PrimaryKey.Fingerprint,
		"signature must name the same fingerprint as the published public key")
}

func TestDetachSign_OpaqueSigner_SimulatesKMS(t *testing.T) {
	t.Parallel()

	priv, pubArmored := buildSigningPair(t)
	data := []byte("payload signed via an opaque crypto.Signer")

	sig, err := openpgpkey.DetachSign(signerOpaque{inner: priv}, pubArmored, bytes.NewReader(data), fixedSigTime)
	require.NoError(t, err, "DetachSign must work with an opaque crypto.Signer (proves KMS-backed signer is sufficient)")

	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubArmored))
	require.NoError(t, err)

	_, err = openpgp.CheckArmoredDetachedSignature(ring, bytes.NewReader(data), bytes.NewReader(sig), nil)
	require.NoError(t, err, "signature from opaque signer must verify against the published public key")
}

func TestDetachSign_VerificationFails_ForTamperedData(t *testing.T) {
	t.Parallel()

	priv, pubArmored := buildSigningPair(t)
	original := []byte("the original payload\n")
	tampered := []byte("THE original payload\n") // one-byte change

	sig, err := openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader(original), fixedSigTime)
	require.NoError(t, err)

	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubArmored))
	require.NoError(t, err)

	_, err = openpgp.CheckArmoredDetachedSignature(ring, bytes.NewReader(original), bytes.NewReader(sig), nil)
	require.NoError(t, err)

	_, err = openpgp.CheckArmoredDetachedSignature(ring, bytes.NewReader(tampered), bytes.NewReader(sig), nil)
	assert.Error(t, err, "verifier must reject a tampered payload")
}

func TestDetachSign_RejectsNonRSASigner(t *testing.T) {
	t.Parallel()

	// Build a fake public key just so DetachSign has something to
	// parse — the RSA-type gate on signer.Public() rejects before we
	// reach the public-key match check, so the publicKey contents
	// can be anything armored that parses.
	_, pubArmored := buildSigningPair(t)

	t.Run("ed25519", func(t *testing.T) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		_, err = openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader([]byte("x")), fixedSigTime)
		require.Error(t, err)
		assert.ErrorIs(t, err, openpgpkey.ErrUnsupportedKeyType)
	})

	t.Run("ecdsa-p256", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		_, err = openpgpkey.DetachSign(key, pubArmored, bytes.NewReader([]byte("x")), fixedSigTime)
		require.Error(t, err)
		assert.ErrorIs(t, err, openpgpkey.ErrUnsupportedKeyType)
	})
}

func TestDetachSign_RejectsSignerNotMatchingPublicKey(t *testing.T) {
	t.Parallel()

	// Two completely different RSA keys: signing with one, claiming
	// the other's identity. The signature would name the second key's
	// fingerprint but the bytes wouldn't verify against it — a
	// confusion that would silently produce an unverifiable .sig.
	signer, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	_, otherPubArmored := buildSigningPair(t) // an unrelated key's published armored half

	_, err = openpgpkey.DetachSign(signer, otherPubArmored, bytes.NewReader([]byte("x")), fixedSigTime)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestDetachSign_PinnedSigTime_ProducesIdenticalSignatures(t *testing.T) {
	t.Parallel()

	priv, pubArmored := buildSigningPair(t)
	data := []byte("reproducible payload")

	a, err := openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader(data), fixedSigTime)
	require.NoError(t, err)
	b, err := openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader(data), fixedSigTime)
	require.NoError(t, err)

	assert.Equal(t, a, b,
		"PKCS#1 v1.5 + pinned sig creation time + deterministic flag must produce byte-identical signatures")
}

func TestDetachSign_DifferentSigTime_ProducesDifferentSignatures(t *testing.T) {
	t.Parallel()

	priv, pubArmored := buildSigningPair(t)
	data := []byte("payload")

	a, err := openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader(data), fixedSigTime)
	require.NoError(t, err)
	b, err := openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader(data), fixedSigTime.Add(time.Hour))
	require.NoError(t, err)

	assert.NotEqual(t, a, b,
		"changing the in-packet sig creation time must change the signature bytes")
}

func TestDetachSign_EmptyData_StillProducesValidSignature(t *testing.T) {
	t.Parallel()

	priv, pubArmored := buildSigningPair(t)

	sig, err := openpgpkey.DetachSign(priv, pubArmored, bytes.NewReader(nil), fixedSigTime)
	require.NoError(t, err)

	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubArmored))
	require.NoError(t, err)

	_, err = openpgp.CheckArmoredDetachedSignature(ring, bytes.NewReader(nil), bytes.NewReader(sig), nil)
	require.NoError(t, err, "signature over empty data must still verify")
}

func TestDetachSign_MalformedPublicKey_Rejected(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	_, err = openpgpkey.DetachSign(priv, []byte("not an armored OpenPGP key"), bytes.NewReader([]byte("x")), fixedSigTime)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing armored public key")
}

// failingReader simulates a transport error mid-read.
type failingReader struct{ err error }

func (r failingReader) Read(_ []byte) (int, error) { return 0, r.err }

func TestDetachSign_ReaderError_Propagates(t *testing.T) {
	t.Parallel()

	priv, pubArmored := buildSigningPair(t)

	_, err := openpgpkey.DetachSign(priv, pubArmored,
		failingReader{err: io.ErrUnexpectedEOF}, fixedSigTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
