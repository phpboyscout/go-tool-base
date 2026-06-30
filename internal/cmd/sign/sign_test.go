package sign

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/signing"
	"gitlab.com/phpboyscout/signing/openpgpkey"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// fixtureKeyTime returns a pinned UTC instant for test reproducibility
// — keeps the fingerprints of test fixtures stable across runs.
var fixtureKeyTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fakeBackend serves a known *rsa.PrivateKey as the signer, mirroring
// the pattern in internal/cmd/keys/keys_test.go.
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

// withRegisteredBackend registers b for the duration of the test.
//
// Tests that call this (or otherwise touch the process-global signing
// registry via ResetForTesting/Register) must NOT call t.Parallel():
// the registry is shared mutable state, so one test's ResetForTesting
// would clear another parallel test's backend mid-run, surfacing as an
// intermittent ErrUnknownBackend / registry panic. They run serially
// instead — fast enough that the loss of parallelism is immaterial.
func withRegisteredBackend(t *testing.T, b signing.Backend) {
	t.Helper()

	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	signing.Register(b)
}

// fixture mints an RSA key + its armored public-key file and writes
// the public file under dir. Returns the private key + the path to
// the .asc file.
func fixture(t *testing.T, dir string) (*rsa.PrivateKey, string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	armored, err := openpgpkey.ArmoredPublicKey(priv, "Test Signer", "test@example.org", fixtureKeyTime)
	require.NoError(t, err)

	path := filepath.Join(dir, "release.asc")
	require.NoError(t, os.WriteFile(path, armored, 0o644)) //nolint:gosec // test fixture

	return priv, path
}

// writePEM writes priv as a PKCS#1 PEM file so the local backend
// could load it. Used by tests that need a complete on-disk fixture
// matching what the CLI's local backend reads.
func writePEM(t *testing.T, dir string, priv *rsa.PrivateKey) string {
	t.Helper()

	path := filepath.Join(dir, "release.pem")
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0o600))

	return path
}

func TestRunSign_HappyPath_LocalBackend(t *testing.T) {
	tmp := t.TempDir()
	priv, pubPath := fixture(t, tmp)

	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: priv})

	inputPath := filepath.Join(tmp, "payload.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("hello\n"), 0o644)) //nolint:gosec // test fixture

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSign(cmd, newTestProps(),
		"fake", "ignored", pubPath, "", "",
		inputPath,
	)
	require.NoError(t, err)

	// .sig must exist + be ASCII-armored.
	sig, err := os.ReadFile(inputPath + ".sig")
	require.NoError(t, err)
	assert.Contains(t, string(sig), "-----BEGIN PGP SIGNATURE-----")
}

func TestRunSign_RefusesOutputEqualsInput(t *testing.T) {
	tmp := t.TempDir()
	priv, pubPath := fixture(t, tmp)
	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: priv})

	inputPath := filepath.Join(tmp, "payload.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("x"), 0o644)) //nolint:gosec // test fixture

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSign(cmd, newTestProps(),
		"fake", "ignored", pubPath, inputPath, "",
		inputPath,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "equals the input path")
}

func TestRunSign_MissingPublicKey_Errors(t *testing.T) {
	tmp := t.TempDir()
	priv, _ := fixture(t, tmp)
	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: priv})

	inputPath := filepath.Join(tmp, "payload.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("x"), 0o644)) //nolint:gosec // test fixture

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSign(cmd, newTestProps(),
		"fake", "ignored", filepath.Join(tmp, "nonexistent.asc"), "", "",
		inputPath,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading public key")
}

func TestRunSign_UnknownBackend_Errors(t *testing.T) {
	tmp := t.TempDir()
	_, pubPath := fixture(t, tmp)
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	inputPath := filepath.Join(tmp, "payload.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("x"), 0o644)) //nolint:gosec // test fixture

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSign(cmd, newTestProps(),
		"does-not-exist", "ignored", pubPath, "", "",
		inputPath,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, signing.ErrUnknownBackend)
}

func TestRunSign_SignerMismatchPublicKey_Errors(t *testing.T) {
	// Two distinct keys: pubPath is the IDENTITY claimed; the backend
	// returns an unrelated signer. This is the safety check that
	// catches "wrong key configured" before producing an unverifiable
	// signature.
	tmp := t.TempDir()
	_, pubPath := fixture(t, tmp)

	otherPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: otherPriv})

	inputPath := filepath.Join(tmp, "payload.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("x"), 0o644)) //nolint:gosec // test fixture

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err = runSign(cmd, newTestProps(),
		"fake", "ignored", pubPath, "", "",
		inputPath,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestRunSign_InvalidCreatedFlag_Errors(t *testing.T) {
	tmp := t.TempDir()
	priv, pubPath := fixture(t, tmp)
	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: priv})

	inputPath := filepath.Join(tmp, "payload.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("x"), 0o644)) //nolint:gosec // test fixture

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSign(cmd, newTestProps(),
		"fake", "ignored", pubPath, "", "not-an-rfc3339-instant",
		inputPath,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RFC3339")
}

func TestRunSign_OutputDefaultsToInputDotSig(t *testing.T) {
	tmp := t.TempDir()
	priv, pubPath := fixture(t, tmp)
	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: priv})

	inputPath := filepath.Join(tmp, "build.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("artifact"), 0o644)) //nolint:gosec // test fixture

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSign(cmd, newTestProps(),
		"fake", "ignored", pubPath, "", "",
		inputPath,
	)
	require.NoError(t, err)

	_, statErr := os.Stat(inputPath + ".sig")
	require.NoError(t, statErr, "expected default <input>.sig")

	// Use the PEM writer to keep the import live (lint sees it as
	// "used"); also serves as a future regression test slot when the
	// local backend grows test coverage in this file.
	_ = writePEM(t, tmp, priv)
}

// TestSameSignPath_CleanedSpellings proves the clobber guard catches
// different spellings of the same path that a raw string compare would
// miss ("./x" vs "x", trailing slash, redundant ".." segments).
func TestSameSignPath_CleanedSpellings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		input  string
		same   bool
	}{
		{name: "identical", output: "x.txt", input: "x.txt", same: true},
		{name: "dot-slash prefix", output: "./x.txt", input: "x.txt", same: true},
		{name: "redundant dotdot", output: "a/../x.txt", input: "x.txt", same: true},
		{name: "different files", output: "x.txt.sig", input: "x.txt", same: false},
		{name: "different dirs", output: "a/x.txt", input: "b/x.txt", same: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.same, sameSignPath(tc.output, tc.input))
		})
	}
}

// TestSameSignPath_SameFileViaSymlink proves the guard falls back to
// os.SameFile so a symlink (or otherwise distinct spelling that resolves
// to the same inode) is still refused.
func TestSameSignPath_SameFileViaSymlink(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	target := filepath.Join(tmp, "artifact.txt")
	require.NoError(t, os.WriteFile(target, []byte("payload"), 0o600))

	link := filepath.Join(tmp, "link.txt")
	require.NoError(t, os.Symlink(target, link))

	// Distinct path strings (clean compare differs) but the same
	// underlying file: os.SameFile must catch it.
	assert.True(t, sameSignPath(link, target),
		"a symlink resolving to the input must be refused via os.SameFile")
}

// TestRunSign_RefusesOutputEqualsInput_DotSlashSpelling proves the
// end-to-end guard rejects "./payload.txt" as --output when the input is
// "payload.txt" (the spelling-bypass the finding describes).
func TestRunSign_RefusesOutputEqualsInput_DotSlashSpelling(t *testing.T) {
	tmp := t.TempDir()
	priv, pubPath := fixture(t, tmp)
	withRegisteredBackend(t, &fakeBackend{name: "fake", signer: priv})

	inputPath := filepath.Join(tmp, "payload.txt")
	require.NoError(t, os.WriteFile(inputPath, []byte("x"), 0o600))

	// Spell the output as "<dir>/./payload.txt" — a raw string compare
	// against inputPath ("<dir>/payload.txt") would let this slip through.
	dottedOutput := filepath.Dir(inputPath) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(inputPath)

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSign(cmd, newTestProps(),
		"fake", "ignored", pubPath, dottedOutput, "",
		inputPath,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "equals the input path")
}
