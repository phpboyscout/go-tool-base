package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openpgpkey"
)

// writeArmoredKey is a test helper that mints an armored OpenPGP
// public key for a given name + email and writes it to dir.
func writeArmoredKey(t *testing.T, dir, filename, name, email string) string {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	armored, err := openpgpkey.ArmoredPublicKey(priv, name, email, time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	path := filepath.Join(dir, filename)
	require.NoError(t, os.WriteFile(path, armored, 0o644)) //nolint:gosec // test fixture

	return path
}

func TestWKD_HappyPath(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := t.TempDir()

	keyA := writeArmoredKey(t, in, "a.asc", "Rotation", "release@phpboyscout.uk")
	keyB := writeArmoredKey(t, in, "b.asc", "Signing", "release@phpboyscout.uk")

	err := runWKD(newTestProps(),
		[]string{keyA, keyB},
		"phpboyscout.uk",
		[]string{"release@phpboyscout.uk"},
		out,
		"advanced",
		"",
	)
	require.NoError(t, err)

	// hu file uses gpg-wks-client canonical hash for `release`.
	const releaseHash = "y84sdmnksfqswe7fxf5mzjg53tbdz8f5"

	for _, p := range []string{
		filepath.Join("openpgpkey", "phpboyscout.uk", "policy"),
		filepath.Join("openpgpkey", "phpboyscout.uk", "submission-address"),
		filepath.Join("openpgpkey", "phpboyscout.uk", "hu", releaseHash),
	} {
		_, err := os.Stat(filepath.Join(out, ".well-known", p))
		assert.NoErrorf(t, err, "expected %s to exist", p)
	}
}

func TestWKD_SubmissionAddress_DefaultsToFirstEmail(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := t.TempDir()

	key := writeArmoredKey(t, in, "k.asc", "Release", "release@phpboyscout.uk")

	err := runWKD(newTestProps(),
		[]string{key},
		"phpboyscout.uk",
		[]string{"release@phpboyscout.uk"},
		out,
		"advanced",
		"", // empty → should default to first --email
	)
	require.NoError(t, err)

	sub, err := os.ReadFile(filepath.Join(out, ".well-known", "openpgpkey", "phpboyscout.uk", "submission-address"))
	require.NoError(t, err)
	assert.Equal(t, "release@phpboyscout.uk", string(sub))
}

func TestWKD_AutoDiscoverEmails_WhenFlagOmitted(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := t.TempDir()

	rel := writeArmoredKey(t, in, "rel.asc", "Release", "release@phpboyscout.uk")
	sec := writeArmoredKey(t, in, "sec.asc", "Security", "security@phpboyscout.uk")

	err := runWKD(newTestProps(),
		[]string{rel, sec},
		"phpboyscout.uk",
		nil, // no --email
		out,
		"advanced",
		"",
	)
	require.NoError(t, err)

	for _, hash := range []string{
		"y84sdmnksfqswe7fxf5mzjg53tbdz8f5", // release@
		"t5s8ztdbon8yzntexy6oz5y48etqsnbb", // security@
	} {
		_, err := os.Stat(filepath.Join(out, ".well-known", "openpgpkey", "phpboyscout.uk", "hu", hash))
		assert.NoErrorf(t, err, "expected hu/%s to exist", hash)
	}
}

func TestWKD_DirectMethod_OmitsDomainSegment(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := t.TempDir()

	key := writeArmoredKey(t, in, "k.asc", "Release", "release@phpboyscout.uk")

	err := runWKD(newTestProps(),
		[]string{key},
		"phpboyscout.uk",
		[]string{"release@phpboyscout.uk"},
		out,
		"direct",
		"release@phpboyscout.uk",
	)
	require.NoError(t, err)

	// direct: no <domain>/ level under openpgpkey/
	_, err = os.Stat(filepath.Join(out, ".well-known", "openpgpkey", "hu", "y84sdmnksfqswe7fxf5mzjg53tbdz8f5"))
	require.NoError(t, err)

	// And no advanced-method file.
	_, err = os.Stat(filepath.Join(out, ".well-known", "openpgpkey", "phpboyscout.uk", "hu", "y84sdmnksfqswe7fxf5mzjg53tbdz8f5"))
	assert.True(t, os.IsNotExist(err), "direct method must NOT emit the advanced layout")
}

func TestWKD_MissingKeyFile_ReturnsError(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	err := runWKD(newTestProps(),
		[]string{"/nonexistent/key.asc"},
		"phpboyscout.uk",
		nil,
		out,
		"advanced",
		"",
	)
	assert.Error(t, err)
}

func TestWKD_EmailNotMatched_ReturnsError(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := t.TempDir()

	key := writeArmoredKey(t, in, "k.asc", "Release", "release@phpboyscout.uk")

	err := runWKD(newTestProps(),
		[]string{key},
		"phpboyscout.uk",
		[]string{"security@phpboyscout.uk"}, // not present on any UID
		out,
		"advanced",
		"",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security@phpboyscout.uk")
}

func TestWKD_NonOpenPGPInput_ReturnsError(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := t.TempDir()

	notAKey := filepath.Join(in, "garbage.asc")
	require.NoError(t, os.WriteFile(notAKey, []byte("not an OpenPGP key"), 0o644)) //nolint:gosec // test fixture

	err := runWKD(newTestProps(),
		[]string{notAKey},
		"phpboyscout.uk",
		nil,
		out,
		"advanced",
		"",
	)
	assert.Error(t, err)
}
