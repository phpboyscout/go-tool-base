package keys

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
)

// fakeBackend lets the keys-command tests drive the mint flow against
// an in-process *rsa.PrivateKey without needing AWS or a real PEM
// file on disk.
type fakeBackend struct {
	name   string
	signer crypto.Signer
}

func (b *fakeBackend) Name() string                   { return b.name }
func (b *fakeBackend) RegisterFlags(_ *pflag.FlagSet) {}
func (b *fakeBackend) NewSigner(_ context.Context, _ string) (crypto.Signer, error) {
	return b.signer, nil
}

func newTestProps() *props.Props {
	return &props.Props{Logger: logger.NewBuffer()}
}

// withRegisteredBackend registers b for the duration of the test and
// resets the registry afterwards. These tests cannot run in parallel
// when they touch the registry — the rest can.
func withRegisteredBackend(t *testing.T, b signing.Backend) {
	t.Helper()

	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	signing.Register(b)
}

// ---- parseCreatedTime ------------------------------------------------------

func TestParseCreatedTime_Empty(t *testing.T) {
	t.Parallel()

	got, err := parseCreatedTime("")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), got, time.Minute,
		"empty --created should resolve to now (UTC)")
}

func TestParseCreatedTime_Valid(t *testing.T) {
	t.Parallel()

	got, err := parseCreatedTime("2026-06-08T12:34:56Z")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 6, 8, 12, 34, 56, 0, time.UTC), got)
}

func TestParseCreatedTime_Invalid(t *testing.T) {
	t.Parallel()

	_, err := parseCreatedTime("not-a-timestamp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RFC3339")
}

// ---- readFingerprint --------------------------------------------------------

func TestReadFingerprint_NotArmored(t *testing.T) {
	t.Parallel()

	_, err := readFingerprint([]byte("definitely not a PGP key block"))
	require.Error(t, err)
}

// ---- resolveOutputs ---------------------------------------------------------

func TestResolveOutputs_Defaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		algorithm string
		wantPub   string
		wantPriv  string
	}{
		{algoEd25519, "ed25519.asc", "ed25519.priv.asc"},
		{algoRSA, "rsa.asc", "rsa.pem"},
	}

	for _, tc := range tests {
		t.Run(tc.algorithm, func(t *testing.T) {
			t.Parallel()

			pub, priv, err := resolveOutputs(tc.algorithm, "", "")
			require.NoError(t, err)
			assert.Equal(t, tc.wantPub, pub)
			assert.Equal(t, tc.wantPriv, priv)
		})
	}
}

func TestResolveOutputs_ExplicitPublicDerivesPrivate(t *testing.T) {
	t.Parallel()

	pub, priv, err := resolveOutputs(algoRSA, "my-key.asc", "")
	require.NoError(t, err)
	assert.Equal(t, "my-key.asc", pub)
	assert.Equal(t, "my-key.pem", priv,
		"private path defaults to public-base + .pem for RSA")
}

func TestResolveOutputs_BothExplicit(t *testing.T) {
	t.Parallel()

	pub, priv, err := resolveOutputs(algoEd25519, "a.asc", "b.asc")
	require.NoError(t, err)
	assert.Equal(t, "a.asc", pub)
	assert.Equal(t, "b.asc", priv)
}

func TestResolveOutputs_PathsCollide(t *testing.T) {
	t.Parallel()

	_, _, err := resolveOutputs(algoRSA, "same.asc", "same.asc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must differ")
}

func TestResolveOutputs_UnknownAlgorithm(t *testing.T) {
	t.Parallel()

	_, _, err := resolveOutputs("blowfish", "key.asc", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown algorithm")
}

// ---- runMint end-to-end ----------------------------------------------------

func TestRunMint_HappyPath_LogsFingerprint(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	withRegisteredBackend(t, &fakeBackend{name: "test-backend", signer: priv})

	dir := t.TempDir()
	out := filepath.Join(dir, "out.asc")

	err = runMint(&cobra.Command{}, newTestProps(), "test-backend", "test-key",
		"TestName", "test@example.com", out, "2026-06-08T00:00:00Z", false)
	require.NoError(t, err)

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(data, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")),
		"mint output must be ASCII-armored")

	fp, err := readFingerprint(data)
	require.NoError(t, err)
	assert.Len(t, fp, 40, "RSA v4 fingerprint is 40 hex chars")
}

func TestRunMint_RejectsStdoutOutput(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	withRegisteredBackend(t, &fakeBackend{name: "test-backend", signer: priv})

	err = runMint(&cobra.Command{}, newTestProps(),
		"test-backend", "k", "n", "e@x", "-", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"-"`)
}

func TestRunMint_UnknownBackend(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	err := runMint(&cobra.Command{}, newTestProps(),
		"no-such-backend", "k", "n", "e@x", "out.asc", "", false)
	require.Error(t, err)
	assert.ErrorIs(t, err, signing.ErrUnknownBackend)
}

func TestRunMint_InvalidCreatedTime(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	withRegisteredBackend(t, &fakeBackend{name: "test-backend", signer: priv})

	err = runMint(&cobra.Command{}, newTestProps(),
		"test-backend", "k", "n", "e@x", "out.asc", "garbage", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RFC3339")
}

// ---- runGenerate end-to-end ------------------------------------------------

func TestRunGenerate_RSA_HappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubPath := filepath.Join(dir, "rsa.asc")
	privPath := filepath.Join(dir, "rsa.pem")

	err := runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "TestRSA", "rsa@example.com",
		pubPath, privPath, "2026-06-08T00:00:00Z", false)
	require.NoError(t, err)

	pubData, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(pubData, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")))

	privData, err := os.ReadFile(privPath)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(privData, []byte("-----BEGIN RSA PRIVATE KEY-----")),
		"RSA private half should be PKCS#1 PEM")

	block, _ := pem.Decode(privData)
	require.NotNil(t, block)
	_, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	require.NoError(t, err)
}

func TestRunGenerate_Ed25519_HappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubPath := filepath.Join(dir, "ed25519.asc")
	privPath := filepath.Join(dir, "ed25519.priv.asc")

	err := runGenerate(&cobra.Command{}, newTestProps(),
		algoEd25519, 0, "TestEd25519", "ed@example.com",
		pubPath, privPath, "", false)
	require.NoError(t, err)

	pubData, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(pubData, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")))

	privData, err := os.ReadFile(privPath)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(privData, []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----")),
		"Ed25519 private half should be armored OpenPGP")

	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubData))
	require.NoError(t, err)
	require.Len(t, ring, 1)
	assert.Equal(t, 22, int(ring[0].PrimaryKey.PubKeyAlgo),
		"Ed25519 must be algorithm 22 (v4 EdDSA) for GnuPG 2.4 compatibility")
}

func TestRunGenerate_RSA_BitsValidation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 1024, "N", "e@x",
		filepath.Join(dir, "out.asc"), filepath.Join(dir, "out.pem"), "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--rsa-bits")
}

func TestRunGenerate_UnknownAlgorithm(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := runGenerate(&cobra.Command{}, newTestProps(),
		"magic", 4096, "N", "e@x",
		filepath.Join(dir, "out.asc"), filepath.Join(dir, "out.pem"), "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown algorithm")
}

func TestRunGenerate_InvalidCreatedTime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "N", "e@x",
		filepath.Join(dir, "out.asc"), filepath.Join(dir, "out.pem"), "garbage", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RFC3339")
}

func TestRunGenerate_PathCollision(t *testing.T) {
	t.Parallel()

	err := runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "N", "e@x", "same.asc", "same.asc", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must differ")
}

func TestRunGenerate_FingerprintRoundTripsThroughLocalBackend(t *testing.T) {
	dir := t.TempDir()
	genPubPath := filepath.Join(dir, "gen.asc")
	genPrivPath := filepath.Join(dir, "gen.pem")

	created := "2026-06-08T00:00:00Z"
	err := runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "X", "x@y",
		genPubPath, genPrivPath, created, false)
	require.NoError(t, err)

	genPub, err := os.ReadFile(genPubPath)
	require.NoError(t, err)
	genFP, err := readFingerprint(genPub)
	require.NoError(t, err)

	// Now register a backend that re-uses the generated key, and mint
	// via it. The mint fingerprint must equal the generate fingerprint
	// when --created is pinned (proves the PEM round-trip preserves
	// identity).
	priv := loadPEM(t, genPrivPath)
	withRegisteredBackend(t, &fakeBackend{name: "local-stub", signer: priv})

	mintPath := filepath.Join(dir, "mint.asc")
	err = runMint(&cobra.Command{}, newTestProps(),
		"local-stub", "ignored", "X", "x@y", mintPath, created, false)
	require.NoError(t, err)

	mintPub, err := os.ReadFile(mintPath)
	require.NoError(t, err)
	mintFP, err := readFingerprint(mintPub)
	require.NoError(t, err)

	assert.Equal(t, genFP, mintFP,
		"generate + mint must produce the same fingerprint when --created is pinned and the same key is used")
}

// ---- no-clobber guard ------------------------------------------------------

// statMode returns the file's permission bits, failing the test on error.
func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)

	return info.Mode().Perm()
}

func TestRunGenerate_RefusesToClobberPrivateKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubPath := filepath.Join(dir, "rsa.asc")
	privPath := filepath.Join(dir, "rsa.pem")

	// First generation succeeds and writes the private half 0o600.
	require.NoError(t, runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "First", "first@example.com",
		pubPath, privPath, "2026-06-08T00:00:00Z", false))

	assert.Equal(t, os.FileMode(0o600), statMode(t, privPath),
		"freshly generated private key must be 0o600")

	original, err := os.ReadFile(privPath)
	require.NoError(t, err)

	// Second generation without --force must refuse and leave the original
	// private key untouched.
	err = runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "Second", "second@example.com",
		pubPath, privPath, "2026-06-09T00:00:00Z", false)
	require.ErrorIs(t, err, ErrKeyFileExists)

	after, err := os.ReadFile(privPath)
	require.NoError(t, err)
	assert.Equal(t, original, after,
		"a refused generate must not modify the existing private key")
}

func TestRunGenerate_ForceOverwritesPrivateKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubPath := filepath.Join(dir, "rsa.asc")
	privPath := filepath.Join(dir, "rsa.pem")

	require.NoError(t, runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "First", "first@example.com",
		pubPath, privPath, "2026-06-08T00:00:00Z", false))

	original, err := os.ReadFile(privPath)
	require.NoError(t, err)

	// --force overwrites and preserves 0o600 perms.
	require.NoError(t, runGenerate(&cobra.Command{}, newTestProps(),
		algoRSA, 2048, "Second", "second@example.com",
		pubPath, privPath, "2026-06-09T00:00:00Z", true))

	after, err := os.ReadFile(privPath)
	require.NoError(t, err)
	assert.NotEqual(t, original, after,
		"--force must overwrite the existing private key")
	assert.Equal(t, os.FileMode(0o600), statMode(t, privPath),
		"overwritten private key must remain 0o600")
}

func TestRunMint_RefusesToClobberOutput(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	withRegisteredBackend(t, &fakeBackend{name: "test-backend", signer: priv})

	dir := t.TempDir()
	out := filepath.Join(dir, "release.asc")

	require.NoError(t, runMint(&cobra.Command{}, newTestProps(),
		"test-backend", "test-key", "N", "e@x", out, "2026-06-08T00:00:00Z", false))

	original, err := os.ReadFile(out)
	require.NoError(t, err)

	// Without --force, a second mint to the same path must refuse.
	err = runMint(&cobra.Command{}, newTestProps(),
		"test-backend", "test-key", "N", "e@x", out, "2026-06-08T00:00:00Z", false)
	require.ErrorIs(t, err, ErrKeyFileExists)

	after, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, original, after, "a refused mint must not modify the existing file")

	// With --force, the mint succeeds.
	require.NoError(t, runMint(&cobra.Command{}, newTestProps(),
		"test-backend", "test-key", "N", "e@x", out, "2026-06-08T00:00:00Z", true))
}

func TestNewCmdKeysGenerate_HasForceFlag(t *testing.T) {
	t.Parallel()

	genCmd := NewCmdKeysGenerate(newTestProps())
	assert.NotNil(t, genCmd.Flags().Lookup("force"), "generate must expose --force")
}

// ---- Cobra wiring smoke ----------------------------------------------------

func TestNewCmdKeys_HasBothSubcommands(t *testing.T) {
	t.Parallel()

	keysCmd := NewCmdKeys(&props.Props{Logger: logger.NewBuffer()})

	got := make(map[string]bool)
	for _, c := range keysCmd.Commands() {
		got[c.Use] = true
	}

	assert.True(t, got["mint"], "keys command must register `mint`")
	assert.True(t, got["generate"], "keys command must register `generate`")
}

// flaggedBackend extends fakeBackend so we can verify NewCmdKeysMint
// invokes RegisterFlags on each registered backend.
type flaggedBackend struct {
	fakeBackend
	flagRegistered bool
}

func (b *flaggedBackend) RegisterFlags(fs *pflag.FlagSet) {
	b.flagRegistered = true
	fs.String("flagged-backend-test-flag", "", "test")
}

func TestNewCmdKeysMint_RegistersBackendFlags(t *testing.T) {
	// Mutates the registry; cannot run in parallel.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	b := &flaggedBackend{fakeBackend: fakeBackend{name: "flagged", signer: priv}}
	withRegisteredBackend(t, b)

	mintCmd := NewCmdKeysMint(newTestProps())

	assert.True(t, b.flagRegistered,
		"NewCmdKeysMint must call RegisterFlags on every registered backend")

	require.NotNil(t, mintCmd.Flags().Lookup("flagged-backend-test-flag"),
		"backend-specific flag must surface on the mint command's flag set")
}

func TestNewCmdKeysGenerate_HasExpectedFlags(t *testing.T) {
	t.Parallel()

	genCmd := NewCmdKeysGenerate(newTestProps())

	for _, name := range []string{"algorithm", "rsa-bits", "name", "email", "output", "private-output", "created"} {
		assert.NotNil(t, genCmd.Flags().Lookup(name), "expected flag --%s on `gtb keys generate`", name)
	}
}

// ---- helpers ---------------------------------------------------------------

func loadPEM(t *testing.T, path string) crypto.Signer {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	block, _ := pem.Decode(data)
	require.NotNil(t, block)

	if strings.HasPrefix(block.Type, "RSA") {
		priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		require.NoError(t, err)
		return priv
	}

	anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	return anyKey.(crypto.Signer)
}
