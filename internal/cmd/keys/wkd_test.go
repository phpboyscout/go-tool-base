package keys

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
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

// TestParseKeyFiles_RingFileDeduplicatesPerEntity proves that a single
// input file holding two entities yields two parsedKey entries whose
// `raw` bytes differ (each is the re-serialized single entity), rather
// than two copies of the whole file. Regression for
// wkd-keyring-file-duplicates-raw-bytes-per-entity.
func TestParseKeyFiles_RingFileDeduplicatesPerEntity(t *testing.T) {
	t.Parallel()

	in := t.TempDir()

	ringBytes := twoEntityRing(t, "release@phpboyscout.uk")

	ring := filepath.Join(in, "ring.asc")
	require.NoError(t, os.WriteFile(ring, ringBytes, 0o600))

	parsed, err := parseKeyFiles([]string{ring})
	require.NoError(t, err)
	require.Len(t, parsed, 2, "a two-entity ring must yield two parsedKey entries")

	// Each entry carries ONLY its own entity, so the two raw blobs differ
	// and neither equals the whole-file bytes (the old bug attached the
	// whole ring to every entity).
	assert.NotEqual(t, parsed[0].raw, parsed[1].raw,
		"per-entity bytes must differ; the old bug attached the whole file to both")
	assert.NotEqual(t, ringBytes, parsed[0].raw,
		"parsedKey.raw must be a single re-serialized entity, not the whole ring file")

	// Each re-serialized entity must itself be a parseable single key.
	for _, pk := range parsed {
		single, perr := parseKeyFiles([]string{writeRaw(t, in, pk.raw)})
		require.NoError(t, perr)
		assert.Len(t, single, 1)
	}
}

// twoEntityRing builds a single ASCII-armored block containing two
// distinct OpenPGP public keys that both claim the same email, so
// ReadArmoredKeyRing parses it as a two-entity ring.
func twoEntityRing(t *testing.T, email string) []byte {
	t.Helper()

	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)

	for range 2 {
		priv, kerr := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, kerr)

		armored, aerr := openpgpkey.ArmoredPublicKey(priv, "Release", email, time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC))
		require.NoError(t, aerr)

		ents, perr := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
		require.NoError(t, perr)
		require.Len(t, ents, 1)
		require.NoError(t, ents[0].Serialize(w))
	}

	require.NoError(t, w.Close())

	return buf.Bytes()
}

// writeRaw writes b to a uniquely-named .asc file under dir and returns
// the path, for round-tripping re-serialized entity bytes.
func writeRaw(t *testing.T, dir string, b []byte) string {
	t.Helper()

	f, err := os.CreateTemp(dir, "entity-*.asc")
	require.NoError(t, err)
	_, err = f.Write(b)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	return f.Name()
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
		"auto", // opt in to the submission-address file (first --email)
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

func TestWKD_SubmissionAddress_EmptyOmitsFile(t *testing.T) {
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
		"", // empty → omit the submission-address file (matches the help)
	)
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(out, ".well-known", "openpgpkey", "phpboyscout.uk", "submission-address"))
	assert.True(t, os.IsNotExist(statErr),
		"empty --submission-address must omit the file, per the documented behaviour")
}

func TestWKD_SubmissionAddress_AutoUsesFirstEmail(t *testing.T) {
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
		"auto", // explicit opt-in → first --email
	)
	require.NoError(t, err)

	sub, err := os.ReadFile(filepath.Join(out, ".well-known", "openpgpkey", "phpboyscout.uk", "submission-address"))
	require.NoError(t, err)
	assert.Equal(t, "release@phpboyscout.uk", string(sub))
}

func TestWKD_SubmissionAddress_ExplicitOverride(t *testing.T) {
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
		"keys@phpboyscout.uk", // explicit address used verbatim
	)
	require.NoError(t, err)

	sub, err := os.ReadFile(filepath.Join(out, ".well-known", "openpgpkey", "phpboyscout.uk", "submission-address"))
	require.NoError(t, err)
	assert.Equal(t, "keys@phpboyscout.uk", string(sub))
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
