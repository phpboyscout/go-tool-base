package openpgpkey_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openpgpkey"
)

// generateArmoredKey produces an armored OpenPGP public key suitable
// for feeding to WriteWKDTree. Uses RSA-2048 for speed (WKD doesn't
// care about key strength; it stores whatever it's given). Returns
// the armored bytes plus the fingerprint of the underlying entity.
func generateArmoredKey(t *testing.T, name, email string) (armored []byte, fingerprintHex string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	armoredBytes, err := openpgpkey.ArmoredPublicKey(priv, name, email, time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armoredBytes))
	require.NoError(t, err)
	require.Len(t, ring, 1)

	return armoredBytes, hex.EncodeToString(ring[0].PrimaryKey.Fingerprint)
}

func TestWriteWKDTree_AdvancedMethod_ProducesCanonicalLayout(t *testing.T) {
	t.Parallel()

	keyA, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")
	keyB, _ := generateArmoredKey(t, "Release Rotation", "release@phpboyscout.uk")

	dir := t.TempDir()

	paths, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk",
		openpgpkey.Options{
			Method:            openpgpkey.MethodAdvanced,
			SubmissionAddress: "release@phpboyscout.uk",
		},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{keyA, keyB}},
	)
	require.NoError(t, err)

	// Expected layout per gpg-wks-client:
	//   .well-known/openpgpkey/phpboyscout.uk/
	//     policy             (empty)
	//     submission-address (the email)
	//     hu/y84sdmnksfqswe7fxf5mzjg53tbdz8f5
	const releaseHash = "y84sdmnksfqswe7fxf5mzjg53tbdz8f5"

	expectedRel := []string{
		filepath.Join(".well-known", "openpgpkey", "phpboyscout.uk", "policy"),
		filepath.Join(".well-known", "openpgpkey", "phpboyscout.uk", "submission-address"),
		filepath.Join(".well-known", "openpgpkey", "phpboyscout.uk", "hu", releaseHash),
	}

	assert.Equal(t, expectedRel, paths, "WriteWKDTree must report paths in {policy, submission-address, hu/...} order")

	for _, p := range expectedRel {
		full := filepath.Join(dir, p)
		_, statErr := os.Stat(full)
		require.NoErrorf(t, statErr, "expected file %s to exist", p)
	}

	policyBytes, err := os.ReadFile(filepath.Join(dir, expectedRel[0]))
	require.NoError(t, err)
	assert.Empty(t, policyBytes, "policy file must be zero-byte by default")

	subBytes, err := os.ReadFile(filepath.Join(dir, expectedRel[1]))
	require.NoError(t, err)
	assert.Equal(t, "release@phpboyscout.uk", string(subBytes), "submission-address must contain the configured email exactly (no trailing newline)")

	huBytes, err := os.ReadFile(filepath.Join(dir, expectedRel[2]))
	require.NoError(t, err)
	assert.NotEmpty(t, huBytes, "hu/<hash> file must be non-empty")

	// Sanity: the hu/ file must parse as a binary OpenPGP key ring
	// containing both entities.
	ring, err := openpgp.ReadKeyRing(bytes.NewReader(huBytes))
	require.NoError(t, err)
	assert.Len(t, ring, 2, "hu/ file must contain both keys")
}

func TestWriteWKDTree_DirectMethod_OmitsDomainSegment(t *testing.T) {
	t.Parallel()

	key, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")

	dir := t.TempDir()

	paths, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk",
		openpgpkey.Options{Method: openpgpkey.MethodDirect},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{key}},
	)
	require.NoError(t, err)

	// Direct method drops the <domain>/ level.
	for _, p := range paths {
		assert.NotContainsf(t, p, filepath.Join("openpgpkey", "phpboyscout.uk"),
			"direct method must not nest under <domain>/: %q", p)
	}

	// Specifically: hu file must be directly under openpgpkey/.
	const releaseHash = "y84sdmnksfqswe7fxf5mzjg53tbdz8f5"
	expectedHu := filepath.Join(".well-known", "openpgpkey", "hu", releaseHash)
	assert.Contains(t, paths, expectedHu)
}

func TestWriteWKDTree_DefaultMethodIsAdvanced(t *testing.T) {
	t.Parallel()

	key, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")

	dir := t.TempDir()

	paths, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk",
		openpgpkey.Options{}, // zero-value Method
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{key}},
	)
	require.NoError(t, err)

	found := false

	for _, p := range paths {
		if p == filepath.Join(".well-known", "openpgpkey", "phpboyscout.uk", "hu", "y84sdmnksfqswe7fxf5mzjg53tbdz8f5") {
			found = true
		}
	}

	assert.True(t, found, "zero-value Method must behave like Advanced")
}

func TestWriteWKDTree_HuFile_IsReproducible_RegardlessOfKeyOrder(t *testing.T) {
	t.Parallel()

	keyA, fpA := generateArmoredKey(t, "Release", "release@phpboyscout.uk")
	keyB, fpB := generateArmoredKey(t, "Release Rotation", "release@phpboyscout.uk")

	// The keys must have different fingerprints for the reproducibility
	// test to be meaningful.
	require.NotEqual(t, fpA, fpB)

	dir1 := t.TempDir()
	_, err := openpgpkey.WriteWKDTree(dir1, "phpboyscout.uk",
		openpgpkey.Options{},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{keyA, keyB}},
	)
	require.NoError(t, err)

	dir2 := t.TempDir()
	_, err = openpgpkey.WriteWKDTree(dir2, "phpboyscout.uk",
		openpgpkey.Options{},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{keyB, keyA}}, // reversed
	)
	require.NoError(t, err)

	const huFile = ".well-known/openpgpkey/phpboyscout.uk/hu/y84sdmnksfqswe7fxf5mzjg53tbdz8f5"

	b1, err := os.ReadFile(filepath.Join(dir1, huFile))
	require.NoError(t, err)
	b2, err := os.ReadFile(filepath.Join(dir2, huFile))
	require.NoError(t, err)

	assert.Equal(t, sha256.Sum256(b1), sha256.Sum256(b2),
		"hu/<hash> file content must be byte-identical regardless of input key order (lexicographic-by-fingerprint sort)")
}

func TestWriteWKDTree_MultipleEmails_GetSeparateHuBuckets(t *testing.T) {
	t.Parallel()

	relKey, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")
	secKey, _ := generateArmoredKey(t, "Security", "security@phpboyscout.uk")

	dir := t.TempDir()

	paths, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk",
		openpgpkey.Options{},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{relKey}},
		openpgpkey.Entry{Email: "security@phpboyscout.uk", Keys: [][]byte{secKey}},
	)
	require.NoError(t, err)

	const (
		releaseHash  = "y84sdmnksfqswe7fxf5mzjg53tbdz8f5"
		securityHash = "t5s8ztdbon8yzntexy6oz5y48etqsnbb" // gpg-wks-client --print-wkd-hash <<<"security@phpboyscout.uk"
	)

	huReleasePath := filepath.Join(".well-known", "openpgpkey", "phpboyscout.uk", "hu", releaseHash)
	huSecurityPath := filepath.Join(".well-known", "openpgpkey", "phpboyscout.uk", "hu", securityHash)

	assert.Contains(t, paths, huReleasePath)
	assert.Contains(t, paths, huSecurityPath)
}

func TestWriteWKDTree_SubmissionAddress_DefaultsOff(t *testing.T) {
	t.Parallel()

	key, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")

	dir := t.TempDir()

	paths, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk",
		openpgpkey.Options{}, // no SubmissionAddress
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{key}},
	)
	require.NoError(t, err)

	for _, p := range paths {
		assert.NotContains(t, p, "submission-address", "submission-address must not be emitted when Options.SubmissionAddress is empty")
	}

	_, err = os.Stat(filepath.Join(dir, ".well-known", "openpgpkey", "phpboyscout.uk", "submission-address"))
	assert.True(t, os.IsNotExist(err), "submission-address file must not exist on disk when not requested")
}

func TestWriteWKDTree_InvalidDomain_Rejected(t *testing.T) {
	t.Parallel()

	key, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")
	dir := t.TempDir()

	for _, bad := range []string{
		"",          // empty
		"foo/bar",   // path separator
		"foo\\bar",  // windows path separator
		"foo..bar",  // double-dot
		".foo.bar",  // leading dot
		"foo.bar.",  // trailing dot
		"foo$bar",   // invalid char
		"../../etc", // traversal attempt
	} {
		t.Run(bad, func(t *testing.T) {
			_, err := openpgpkey.WriteWKDTree(dir, bad,
				openpgpkey.Options{},
				openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{key}},
			)
			assert.Error(t, err, "invalid domain %q must be rejected", bad)
		})
	}
}

func TestWriteWKDTree_EmptyEntries_Rejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk", openpgpkey.Options{})
	assert.Error(t, err)
}

func TestWriteWKDTree_InvalidEmail_Rejected(t *testing.T) {
	t.Parallel()

	key, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")
	dir := t.TempDir()

	for _, bad := range []string{"", "not-an-email", "@nolocal", "noat"} {
		t.Run(bad, func(t *testing.T) {
			_, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk",
				openpgpkey.Options{},
				openpgpkey.Entry{Email: bad, Keys: [][]byte{key}},
			)
			assert.Error(t, err)
		})
	}
}

func TestWriteWKDTree_InvalidMethod_Rejected(t *testing.T) {
	t.Parallel()

	key, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")
	dir := t.TempDir()

	_, err := openpgpkey.WriteWKDTree(dir, "phpboyscout.uk",
		openpgpkey.Options{Method: "weird"},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{key}},
	)
	assert.Error(t, err)
}

func TestWriteWKDTree_BinaryAndArmoredKeysInterchangeable(t *testing.T) {
	t.Parallel()

	// Armored key.
	armored, _ := generateArmoredKey(t, "Release", "release@phpboyscout.uk")

	// Same key, dearmored.
	ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	require.NoError(t, err)

	var bin bytes.Buffer
	require.NoError(t, ring[0].Serialize(&bin))

	// Write both — they must produce identical hu/ content.
	dirA := t.TempDir()
	_, err = openpgpkey.WriteWKDTree(dirA, "phpboyscout.uk", openpgpkey.Options{},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{armored}},
	)
	require.NoError(t, err)

	dirB := t.TempDir()
	_, err = openpgpkey.WriteWKDTree(dirB, "phpboyscout.uk", openpgpkey.Options{},
		openpgpkey.Entry{Email: "release@phpboyscout.uk", Keys: [][]byte{bin.Bytes()}},
	)
	require.NoError(t, err)

	huPath := filepath.Join(".well-known", "openpgpkey", "phpboyscout.uk", "hu", "y84sdmnksfqswe7fxf5mzjg53tbdz8f5")

	a, err := os.ReadFile(filepath.Join(dirA, huPath))
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(dirB, huPath))
	require.NoError(t, err)

	assert.Equal(t, a, b, "armored vs binary input must produce identical hu/ content")
}

func TestWKDHash_MatchesGPGWKSClient(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		// gpg-wks-client --print-wkd-hash output:
		"joe.user@example.org":   "1eywsd1ce1nfs68idcpb69buui6xnxrd",
		"release@phpboyscout.uk": "y84sdmnksfqswe7fxf5mzjg53tbdz8f5",
		// Uppercase local-part must lowercase before hashing.
		"Release@phpboyscout.uk": "y84sdmnksfqswe7fxf5mzjg53tbdz8f5",
	}

	for email, want := range cases {
		got, err := openpgpkey.WKDHash(email)
		require.NoError(t, err, email)
		assert.Equal(t, want, got, email)
	}
}
