package setup

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test-key fixtures are generated once per test binary via sync.Once.
// Generation uses the library defaults (crypto/rand); the keys are
// throwaway and not committed to the repository.
//
//nolint:gochecknoglobals // test fixtures shared across parallel tests
var (
	testKeysOnce sync.Once
	testKeysErr  error

	testEd25519 testSigningKey
	testRSA4096 testSigningKey
	testRSA1024 testSigningKey
	testECDSA   testSigningKey
)

// testSigningKey bundles a generated entity with its ASCII-armored
// public-key blob, so each test can either sign with the entity or load
// the public half into a TrustSet without re-armoring.
type testSigningKey struct {
	entity     *openpgp.Entity
	armoredPub []byte
}

// mustInitTestSigningKeys generates the four fixture keys exactly once
// per test binary and fails the calling test if any generation step
// errors. Subsequent calls are no-ops.
func mustInitTestSigningKeys(t *testing.T) {
	t.Helper()
	testKeysOnce.Do(func() {
		testEd25519, testKeysErr = generateTestSigningKey(&packet.Config{
			Algorithm: packet.PubKeyAlgoEdDSA,
			Curve:     packet.Curve25519,
		}, "Ed25519 Test", "ed25519@test.example")
		if testKeysErr != nil {
			return
		}
		testRSA4096, testKeysErr = generateTestSigningKey(&packet.Config{
			Algorithm: packet.PubKeyAlgoRSA,
			RSABits:   4096,
		}, "RSA 4096 Test", "rsa4096@test.example")
		if testKeysErr != nil {
			return
		}
		testRSA1024, testKeysErr = generateTestSigningKey(&packet.Config{
			Algorithm: packet.PubKeyAlgoRSA,
			RSABits:   1024,
		}, "RSA 1024 Test", "rsa1024@test.example")
		if testKeysErr != nil {
			return
		}
		testECDSA, testKeysErr = generateTestSigningKey(&packet.Config{
			Algorithm: packet.PubKeyAlgoECDSA,
			Curve:     packet.CurveNistP256,
		}, "ECDSA P-256 Test", "ecdsa@test.example")
	})
	require.NoError(t, testKeysErr, "test key generation must succeed")
}

func generateTestSigningKey(cfg *packet.Config, name, email string) (testSigningKey, error) {
	ent, err := openpgp.NewEntity(name, "", email, cfg)
	if err != nil {
		return testSigningKey{}, err
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return testSigningKey{}, err
	}
	if err := ent.Serialize(w); err != nil {
		return testSigningKey{}, err
	}
	if err := w.Close(); err != nil {
		return testSigningKey{}, err
	}
	return testSigningKey{entity: ent, armoredPub: buf.Bytes()}, nil
}

func detachSign(t *testing.T, ent *openpgp.Entity, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t,
		openpgp.ArmoredDetachSign(&buf, ent, bytes.NewReader(data), nil))
	return buf.Bytes()
}

func TestLoadTrustSet_StrengthPolicy(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	cases := []struct {
		name      string
		keyArmor  func() []byte
		wantErrIs error
	}{
		{"ed25519-accepted", func() []byte { return testEd25519.armoredPub }, nil},
		{"rsa-4096-accepted", func() []byte { return testRSA4096.armoredPub }, nil},
		{"rsa-1024-rejected", func() []byte { return testRSA1024.armoredPub }, ErrWeakKey},
		{"ecdsa-p256-rejected", func() []byte { return testECDSA.armoredPub }, ErrWeakKey},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts, err := LoadTrustSet(tc.keyArmor())
			if tc.wantErrIs == nil {
				require.NoError(t, err)
				require.NotNil(t, ts)
				require.Len(t, ts.Fingerprints(), 1)
			} else {
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.wantErrIs)
			}
		})
	}
}

func TestLoadTrustSet_MultipleKeys_AnyWeakFails(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	_, err := LoadTrustSet(testEd25519.armoredPub, testRSA1024.armoredPub)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWeakKey)
}

func TestLoadTrustSet_MultipleStrongKeys(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	ts, err := LoadTrustSet(testEd25519.armoredPub, testRSA4096.armoredPub)
	require.NoError(t, err)
	assert.Len(t, ts.Fingerprints(), 2)
}

func TestLoadTrustSet_NoKeys(t *testing.T) {
	t.Parallel()

	_, err := LoadTrustSet()
	require.Error(t, err)
}

func TestLoadTrustSet_MalformedArmor(t *testing.T) {
	t.Parallel()

	_, err := LoadTrustSet([]byte("not a real armored key"))
	require.Error(t, err)
}

func TestTrustSet_Fingerprints_SortedUppercaseHex(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	// Pass the keys in non-sorted order so the test exercises the sort.
	ts, err := LoadTrustSet(testRSA4096.armoredPub, testEd25519.armoredPub)
	require.NoError(t, err)

	fps := ts.Fingerprints()
	require.Len(t, fps, 2)
	for _, fp := range fps {
		assert.Len(t, fp, 40, "fingerprint must be 40 hex chars")
		assert.Equal(t, strings.ToUpper(fp), fp, "fingerprint must be uppercase")
	}
	assert.LessOrEqual(t, fps[0], fps[1], "fingerprints must be sorted ascending")
}

func TestVerifyManifestSignature_Valid(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	cases := []struct {
		name string
		key  func() testSigningKey
	}{
		{"ed25519", func() testSigningKey { return testEd25519 }},
		{"rsa-4096", func() testSigningKey { return testRSA4096 }},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := tc.key()

			manifest := []byte("abc123  artifact.tar.gz\n")
			sig := detachSign(t, k.entity, manifest)

			ts, err := LoadTrustSet(k.armoredPub)
			require.NoError(t, err)

			assert.NoError(t, ts.VerifyManifestSignature(manifest, sig))
		})
	}
}

func TestVerifyManifestSignature_TamperedManifest(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	manifest := []byte("abc123  artifact.tar.gz\n")
	sig := detachSign(t, testEd25519.entity, manifest)

	ts, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)

	tampered := []byte("abc123  artifact.tar.gz!") // one byte changed
	err = ts.VerifyManifestSignature(tampered, sig)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestVerifyManifestSignature_KeyNotInSet(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	manifest := []byte("abc123  artifact.tar.gz\n")
	// Sign with Ed25519; the trust set has only RSA-4096.
	sig := detachSign(t, testEd25519.entity, manifest)

	ts, err := LoadTrustSet(testRSA4096.armoredPub)
	require.NoError(t, err)

	err = ts.VerifyManifestSignature(manifest, sig)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestVerifyManifestSignature_MalformedSignature(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	ts, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)

	err = ts.VerifyManifestSignature([]byte("manifest"), []byte("not a signature"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestVerifyManifestSignature_EmptySignature(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	ts, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)

	err = ts.VerifyManifestSignature([]byte("manifest"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestVerifyManifestSignature_RotationOverlap(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	// Trust set with two strong keys (rotation overlap window): a
	// signature from either key must validate.
	ts, err := LoadTrustSet(testEd25519.armoredPub, testRSA4096.armoredPub)
	require.NoError(t, err)

	manifest := []byte("abc123  artifact.tar.gz\n")

	sigEd := detachSign(t, testEd25519.entity, manifest)
	sigRSA := detachSign(t, testRSA4096.entity, manifest)

	assert.NoError(t, ts.VerifyManifestSignature(manifest, sigEd),
		"ed25519 signature should validate against rotation-overlap set")
	assert.NoError(t, ts.VerifyManifestSignature(manifest, sigRSA),
		"rsa-4096 signature should validate against rotation-overlap set")
}
