package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// TestSignReleaseScript_VerifiesViaTrustSet proves the end-to-end loop
// without any KMS/WKD infra: scripts/sign-release.sh (gpg) produces an
// armored detached signature that the library's TrustSet accepts, and a
// tampered manifest is rejected. This is the build-side ↔ verify-side
// contract for Phase 2 signing.
//
// Gated as an integration test because it shells out to gpg and the
// signing script. Enable with INT_TEST=1 or INT_TEST_SIGNING=1.
func TestSignReleaseScript_VerifiesViaTrustSet(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "signing")

	gpgPath, err := exec.LookPath("gpg")
	if err != nil {
		t.Skip("gpg not installed; skipping signing-script integration test")
	}

	gnupgHome := t.TempDir()
	const signerEmail = "sign-test@phpboyscout.uk"

	// Isolated keyring so the test never touches the developer's keys.
	gpgEnv := append(os.Environ(), "GNUPGHOME="+gnupgHome)

	// Generate a throwaway, unprotected Ed25519 signing key.
	keyparams := filepath.Join(gnupgHome, "keyparams")
	require.NoError(t, os.WriteFile(keyparams, []byte(
		"%no-protection\n"+
			"Key-Type: eddsa\n"+
			"Key-Curve: ed25519\n"+
			"Key-Usage: sign\n"+
			"Name-Real: GTB Test Signer\n"+
			"Name-Email: "+signerEmail+"\n"+
			"Expire-Date: 0\n"+
			"%commit\n"), 0o600))

	runGPG := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(gpgPath, args...)
		cmd.Env = gpgEnv
		out, runErr := cmd.CombinedOutput()
		require.NoError(t, runErr, "gpg %v: %s", args, out)

		return out
	}

	runGPG("--batch", "--pinentry-mode", "loopback", "--generate-key", keyparams)

	// Sign a sample manifest via the release script.
	manifestPath := filepath.Join(t.TempDir(), "checksums.txt")
	manifest := []byte("deadbeef  gtb_Linux_x86_64.tar.gz\ncafebabe  checksums.txt\n")
	require.NoError(t, os.WriteFile(manifestPath, manifest, 0o600))

	sigPath := manifestPath + ".sig"
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "sign-release.sh"))
	require.NoError(t, err)

	signCmd := exec.Command(script, manifestPath, sigPath)
	signCmd.Env = append(slices.Clone(gpgEnv), "GTB_SIGNING_KEY="+signerEmail)
	signOut, err := signCmd.CombinedOutput()
	require.NoError(t, err, "sign-release.sh: %s", signOut)

	// Export the public half and build a TrustSet from it.
	pubArmored := runGPG("--armor", "--export", signerEmail)
	require.NotEmpty(t, pubArmored, "exported public key must not be empty")

	ts, err := LoadTrustSet(pubArmored)
	require.NoError(t, err)

	sig, err := os.ReadFile(sigPath)
	require.NoError(t, err)

	// The library accepts the gpg-produced armored detached signature.
	require.NoError(t, ts.VerifyManifestSignature(manifest, sig),
		"library must verify the signature produced by sign-release.sh")

	// A tampered manifest must be rejected.
	tampered := []byte("00000000  gtb_Linux_x86_64.tar.gz\ncafebabe  checksums.txt\n")
	err = ts.VerifyManifestSignature(tampered, sig)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}
