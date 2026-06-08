package openpgpkey

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testName  = "GTB Release"
	testEmail = "release@phpboyscout.uk"
)

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// opaqueSigner wraps an RSA key but exposes ONLY the crypto.Signer
// interface (its concrete type is not *rsa.PrivateKey). This simulates
// a KMS/HSM-backed signer: the public key is available, signing is a
// remote operation, and the private material is never a known
// go-crypto type. If the minting path works with this, it works with
// real KMS.
type opaqueSigner struct{ inner *rsa.PrivateKey }

func (s opaqueSigner) Public() crypto.PublicKey { return &s.inner.PublicKey }

func (s opaqueSigner) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return s.inner.Sign(r, digest, opts)
}

// parseEntity reads back the armored output and returns the OpenPGP
// entity for assertion. Fails the test on any parse error.
func parseEntity(t *testing.T, armored []byte) *openpgp.Entity {
	t.Helper()

	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	require.NoError(t, err, "reading armored key ring")
	require.Len(t, ring, 1, "expected exactly one entity in the armored output")

	return ring[0]
}

func TestArmoredPublicKey_ConcreteRSA_RoundTrips(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	pub, err := ArmoredPublicKey(key, testName, testEmail, fixedTime)
	require.NoError(t, err)

	// The output must be ASCII-armored.
	assert.True(t, bytes.HasPrefix(pub, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")),
		"output must be ASCII-armored")

	ent := parseEntity(t, pub)

	// The primary key carries an RSA public half.
	assert.Equal(t, packet.PubKeyAlgoRSA, ent.PrimaryKey.PubKeyAlgo,
		"primary key algorithm should be RSA")

	// Exactly one identity, with our UID.
	require.Len(t, ent.Identities, 1)

	var gotUID string
	for uid := range ent.Identities {
		gotUID = uid
	}

	assert.Equal(t, testName+" <"+testEmail+">", gotUID, "UID mismatch")

	// And the identity carries a positive-cert self-signature — the
	// thing the verifier rejects when missing ("v4 entity without any
	// identities").
	id := ent.Identities[gotUID]
	require.NotNil(t, id.SelfSignature, "identity must have a self-signature")
}

func TestArmoredPublicKey_OpaqueSigner_SimulatesKMS(t *testing.T) {
	t.Parallel()

	inner, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	// Minting must self-sign via the opaque signer (crypto.Signer path),
	// proving a KMS-backed RSA signer works without the private key
	// being a known go-crypto type.
	pub, err := ArmoredPublicKey(opaqueSigner{inner: inner}, testName, testEmail, fixedTime)
	require.NoError(t, err)

	ent := parseEntity(t, pub)
	require.Len(t, ent.Identities, 1)

	for _, id := range ent.Identities {
		require.NotNil(t, id.SelfSignature)
	}
}

func TestArmoredPublicKey_Ed25519_RoundTrips(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pub, err := ArmoredPublicKey(priv, testName, testEmail, fixedTime)
	require.NoError(t, err)

	assert.True(t, bytes.HasPrefix(pub, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")),
		"output must be ASCII-armored")

	ent := parseEntity(t, pub)
	assert.Equal(t, packet.PubKeyAlgoEd25519, ent.PrimaryKey.PubKeyAlgo,
		"primary key algo should be Ed25519 (RFC 9580 native, packet algorithm 27)")

	require.Len(t, ent.Identities, 1)

	for _, id := range ent.Identities {
		require.NotNil(t, id.SelfSignature, "Ed25519 entity must carry a positive-cert self-signature")
	}
}

// opaqueEd25519Signer reports an Ed25519 public half but is not a
// concrete ed25519.PrivateKey. Used to verify that the Ed25519 path
// refuses opaque signers in v0.1 with a clear error rather than
// silently producing a key it cannot sign with.
type opaqueEd25519Signer struct{ pub ed25519.PublicKey }

func (s opaqueEd25519Signer) Public() crypto.PublicKey { return s.pub }
func (s opaqueEd25519Signer) Sign(_ io.Reader, _ []byte, _ crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("would call KMS in real life")
}

func TestArmoredPublicKey_Ed25519_RefusesOpaqueSigners(t *testing.T) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	_, err = ArmoredPublicKey(opaqueEd25519Signer{pub: pub}, testName, testEmail, fixedTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedKeyType,
		"v0.1 Ed25519 path requires a concrete stdlib ed25519.PrivateKey; opaque signers must surface ErrUnsupportedKeyType")
	assert.Contains(t, err.Error(), "Ed25519",
		"error must name the algorithm so operators understand which path is restricted")
}

func TestArmoredPublicKey_RejectsUnsupportedKeyType(t *testing.T) {
	t.Parallel()

	// ECDSA P-256 is not in the supported set (RSA and Ed25519 only).
	// Use an opaque crypto.Signer whose Public() returns an ECDSA key
	// to exercise the default branch in publicKeyPacket.
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	_, err = ArmoredPublicKey(ecdsaKey, testName, testEmail, fixedTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedKeyType,
		"non-RSA / non-Ed25519 keys must surface ErrUnsupportedKeyType")
}

func TestArmoredPublicKey_Deterministic(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	a, err := ArmoredPublicKey(key, testName, testEmail, fixedTime)
	require.NoError(t, err)
	b, err := ArmoredPublicKey(key, testName, testEmail, fixedTime)
	require.NoError(t, err)

	// Same key + same creation time + same UID must produce the same
	// fingerprint (embed and WKD copies must agree under
	// CompositeResolver cross-check).
	entA := parseEntity(t, a)
	entB := parseEntity(t, b)
	assert.Equal(t, entA.PrimaryKey.Fingerprint, entB.PrimaryKey.Fingerprint,
		"fingerprint must be reproducible across mints of the same key + creation time")
}

func TestArmoredPublicKey_CreationTimeAffectsFingerprint(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	a, err := ArmoredPublicKey(key, testName, testEmail, fixedTime)
	require.NoError(t, err)
	b, err := ArmoredPublicKey(key, testName, testEmail, fixedTime.Add(time.Hour))
	require.NoError(t, err)

	// Different creation times against the same key produce different
	// fingerprints — this is the footgun the --created flag exists to
	// pin against on re-mints.
	entA := parseEntity(t, a)
	entB := parseEntity(t, b)
	assert.NotEqual(t, entA.PrimaryKey.Fingerprint, entB.PrimaryKey.Fingerprint,
		"different creation times must produce different fingerprints")
}

// failingSigner returns an RSA public half but errors on every Sign
// call. go-crypto defers the actual signing operation until
// Entity.Serialize runs (AddUserId stages a Signature that gets
// signed at serialise time), so the underlying error surfaces with
// the "serializing public key" wrap rather than the AddUserId wrap.
// Either way the consumer gets a non-nil error — which is what we
// care about.
type failingSigner struct{ inner *rsa.PrivateKey }

func (s failingSigner) Public() crypto.PublicKey { return &s.inner.PublicKey }

func (s failingSigner) Sign(_ io.Reader, _ []byte, _ crypto.SignerOpts) ([]byte, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestArmoredPublicKey_SignerErrorPropagates(t *testing.T) {
	t.Parallel()

	inner, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	_, err = ArmoredPublicKey(failingSigner{inner: inner}, testName, testEmail, fixedTime)
	require.Error(t, err, "Signer Sign() returning an error must propagate to the caller")
	assert.Contains(t, err.Error(), "serializing public key",
		"error wrap should identify the failing step")
}

// failingWriter rejects every write with the supplied error. Used to
// exercise the WriteArmoredPublicKey error paths around armor.Encode
// and the armor writer's Close — those paths are unreachable when the
// caller hands in a bytes.Buffer (which never returns errors), so the
// public API ArmoredPublicKey can't trip them.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteArmoredPublicKey_WriterErrorAtEncode(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	// armor.Encode does a small priming write of the BEGIN line — so a
	// writer that fails on the very first write surfaces an error out
	// of Encode/Serialize/Close depending on go-crypto's framing.
	err = WriteArmoredPublicKey(failingWriter{err: io.ErrClosedPipe}, key, testName, testEmail, fixedTime)
	require.Error(t, err, "failing writer must propagate an error")
	assert.ErrorIs(t, err, io.ErrClosedPipe,
		"the underlying writer error must be reachable via errors.Is")
}

func TestArmoredPublicKey_ArmorEnvelope(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	pub, err := ArmoredPublicKey(key, testName, testEmail, fixedTime)
	require.NoError(t, err)

	// Header and trailer of the standard armor envelope.
	s := string(pub)
	assert.True(t, strings.HasPrefix(s, "-----BEGIN PGP PUBLIC KEY BLOCK-----\n"))
	assert.True(t, strings.HasSuffix(strings.TrimRight(s, "\n"), "-----END PGP PUBLIC KEY BLOCK-----"))
}
