package sign

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/signing/openpgpkey"
	"gitlab.com/phpboyscout/go/signing/verify"
)

// trustSetMinBits is the minimum RSA modulus verify.LoadTrustSet will
// accept. The shared fixture() helper mints 2048-bit keys, which are
// fine for the sign-side tests but are refused by the verify side —
// so these contract tests need their own stronger fixture.
const trustSetMinBits = 3072

// contractKeyPair holds the two identities these tests need: the one
// the trust set is built from, and one outside it.
type contractKeyPair struct {
	signer   *rsa.PrivateKey
	stranger *rsa.PrivateKey
}

// contractKeys caches both keys for the whole package. Keygen at 3072
// bits costs a second or so each, so minting them per test would
// dominate the runtime. Generated once and only read afterwards, so
// there is no shared-mutable-state hazard.
var contractKeys = sync.OnceValues(func() (contractKeyPair, error) {
	signer, err := rsa.GenerateKey(rand.Reader, trustSetMinBits)
	if err != nil {
		return contractKeyPair{}, err
	}

	stranger, err := rsa.GenerateKey(rand.Reader, trustSetMinBits)
	if err != nil {
		return contractKeyPair{}, err
	}

	return contractKeyPair{signer: signer, stranger: stranger}, nil
})

// armoredPublicKeyFile writes priv's armored public half under dir and
// returns the path, mirroring what fixture() does for the 2048-bit case.
func armoredPublicKeyFile(t *testing.T, dir string, priv *rsa.PrivateKey) string {
	t.Helper()

	armored, err := openpgpkey.ArmoredPublicKey(priv, "Contract Signer", "contract@example.org", fixtureKeyTime)
	require.NoError(t, err)

	path := filepath.Join(dir, "release.asc")
	require.NoError(t, os.WriteFile(path, armored, 0o600))

	return path
}

// signManifest signs body with priv and returns the manifest bytes, the
// armored detached signature, and the armored public key.
//
// It drives the real runSign path rather than calling the signing
// library directly — the point of these tests is the CLI's output, not
// the library's internals.
func signManifest(t *testing.T, priv *rsa.PrivateKey, body string) (manifest, signature, publicKey []byte) {
	t.Helper()

	tmp := t.TempDir()
	pubPath := armoredPublicKeyFile(t, tmp, priv)

	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: priv})

	manifestPath := filepath.Join(tmp, "checksums.txt")
	manifest = []byte(body)
	require.NoError(t, os.WriteFile(manifestPath, manifest, 0o600))

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	require.NoError(t, runSign(cmd, newTestProps(),
		"fake", "ignored", pubPath, "", "",
		manifestPath,
		false,
	))

	signature, err := os.ReadFile(manifestPath + ".sig")
	require.NoError(t, err)

	publicKey, err = os.ReadFile(pubPath)
	require.NoError(t, err)

	return manifest, signature, publicKey
}

// TestSignVerifyContract_SignatureIsAcceptedByTrustSet closes the
// build-side <-> verify-side contract: a signature produced by `gtb sign`
// must be accepted by the same TrustSet that self-update enforces before
// it will install an update.
//
// This is the coverage that was lost when signing moved to go/signing —
// the previous end-to-end test drove scripts/sign-release.sh with gpg,
// but that script is now KMS-only and cannot run without AWS
// credentials. Asserting the contract at the CLI boundary keeps the
// security-critical half testable with no infrastructure.
//
// Serial (no t.Parallel): withRegisteredBackend mutates the
// process-global signing registry.
func TestSignVerifyContract_SignatureIsAcceptedByTrustSet(t *testing.T) {
	keys, err := contractKeys()
	require.NoError(t, err)

	manifest, signature, publicKey := signManifest(t, keys.signer,
		"deadbeef  gtb_Linux_x86_64.tar.gz\ncafebabe  checksums.txt\n")

	ts, err := verify.LoadTrustSet(publicKey)
	require.NoError(t, err)

	require.NoError(t, ts.VerifyManifestSignature(manifest, signature),
		"a signature from `gtb sign` must verify against the release trust set")
}

// TestSignVerifyContract_TamperedManifestIsRejected proves the
// signature is actually bound to the manifest contents. Without this,
// a verifier that accepted anything would still pass the happy-path
// test above.
//
// Serial (no t.Parallel): see above.
func TestSignVerifyContract_TamperedManifestIsRejected(t *testing.T) {
	keys, err := contractKeys()
	require.NoError(t, err)

	manifest, signature, publicKey := signManifest(t, keys.signer,
		"deadbeef  gtb_Linux_x86_64.tar.gz\ncafebabe  checksums.txt\n")

	ts, err := verify.LoadTrustSet(publicKey)
	require.NoError(t, err)

	// Flip the first checksum; everything else is byte-identical.
	tampered := []byte("00000000  gtb_Linux_x86_64.tar.gz\ncafebabe  checksums.txt\n")
	require.NotEqual(t, manifest, tampered)

	err = ts.VerifyManifestSignature(tampered, signature)
	require.Error(t, err, "a tampered manifest must not verify")
	assert.ErrorIs(t, err, verify.ErrSignatureInvalid)
}

// TestSignVerifyContract_UntrustedKeyIsRejected proves the trust set is
// doing identity work rather than merely checking the signature is
// well-formed: a signature from a key outside the trust set must fail
// even though it is internally valid.
//
// Serial (no t.Parallel): see above.
func TestSignVerifyContract_UntrustedKeyIsRejected(t *testing.T) {
	keys, err := contractKeys()
	require.NoError(t, err)

	manifest, signature, _ := signManifest(t, keys.signer,
		"deadbeef  gtb_Linux_x86_64.tar.gz\ncafebabe  checksums.txt\n")

	// A trust set built from a different key entirely.
	_, _, strangerPub := signManifest(t, keys.stranger, "unrelated\n")

	ts, err := verify.LoadTrustSet(strangerPub)
	require.NoError(t, err)

	err = ts.VerifyManifestSignature(manifest, signature)
	require.Error(t, err, "a signature from an untrusted key must not verify")
	assert.ErrorIs(t, err, verify.ErrSignatureInvalid)
}
