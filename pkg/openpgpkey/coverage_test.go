package openpgpkey

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedCovTime is a stable creation time so any keys minted in this
// file are reproducible.
var fixedCovTime = time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)

// mintRSAArmored mints a single armored RSA public key with one UID.
func mintRSAArmored(t *testing.T, name, email string) []byte {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	armored, err := ArmoredPublicKey(priv, name, email, fixedCovTime)
	require.NoError(t, err)

	return armored
}

// mintRSAEntity builds a fresh signing-capable RSA entity (with UID)
// for serialising directly into custom armored blocks.
func mintRSAEntity(t *testing.T) *openpgp.Entity {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	ent, err := Entity(priv, "Cov", "cov@example.com", fixedCovTime)
	require.NoError(t, err)

	return ent
}

// mintEdDSAArmored mints an armored EdDSA public key. Used to exercise
// the non-RSA primary-key branch in assertSignerMatchesPublicKey: the
// block parses (it has a UID) but its primary key is not *rsa.PublicKey.
func mintEdDSAArmored(t *testing.T) []byte {
	t.Helper()

	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}

	ent, err := openpgp.NewEntity("EdDSA", "", "eddsa@example.com", cfg)
	require.NoError(t, err)

	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, ent.Serialize(w))
	require.NoError(t, w.Close())

	return buf.Bytes()
}

// --- dearmorIfNeeded / concatBinaryKeys error branches ---------------

func TestDearmorIfNeeded_BinaryGarbage_Rejected(t *testing.T) {
	t.Parallel()

	_, _, err := dearmorIfNeeded([]byte{0x00, 0x01, 0x02, 0x03})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing binary entity")
}

func TestDearmorIfNeeded_MalformedArmor_Rejected(t *testing.T) {
	t.Parallel()

	// Begins with the armor sentinel so the armored branch is taken,
	// but the block body is not valid base64 armor.
	raw := []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nnot base64 @@@\n-----END PGP PUBLIC KEY BLOCK-----\n")

	_, _, err := dearmorIfNeeded(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding armor")
}

func TestDearmorIfNeeded_ArmoredNonKey_Rejected(t *testing.T) {
	t.Parallel()

	// A well-formed armor block whose body is not an OpenPGP key
	// entity: the armor decodes and the body reads, but ReadEntity
	// fails (exercises the "parsing dearmored entity" wrap).
	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.SignatureType, nil)
	require.NoError(t, err)
	_, err = w.Write([]byte("definitely not a key packet stream"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, _, err = dearmorIfNeeded(buf.Bytes())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing dearmored entity")
}

func TestDearmorIfNeeded_LeadingWhitespaceArmor_Accepted(t *testing.T) {
	t.Parallel()

	armored := mintRSAArmored(t, "Lead", "lead@example.com")
	padded := append([]byte("  \t\r\n"), armored...)

	bin, ent, err := dearmorIfNeeded(padded)
	require.NoError(t, err)
	assert.NotEmpty(t, bin)
	require.NotNil(t, ent)
	assert.NotNil(t, ent.PrimaryKey)
}

func TestConcatBinaryKeys_BadKey_Wrapped(t *testing.T) {
	t.Parallel()

	_, err := concatBinaryKeys([][]byte{[]byte("garbage")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key 0")
}

// --- wkdHash error branch --------------------------------------------

func TestWkdHash_InvalidEmail_Rejected(t *testing.T) {
	t.Parallel()

	_, err := wkdHash("not-an-email")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing email")
}

// --- wkdRoot default branch (unreachable in WriteWKDTree, but the
// switch's default is still exercisable directly) ---------------------

func TestWkdRoot_Layouts(t *testing.T) {
	t.Parallel()

	advanced := wkdRoot("/out", "example.com", MethodAdvanced)
	assert.Equal(t, filepath.Join("/out", ".well-known", "openpgpkey", "example.com"), advanced)

	direct := wkdRoot("/out", "example.com", MethodDirect)
	assert.Equal(t, filepath.Join("/out", ".well-known", "openpgpkey"), direct)

	// Unknown method falls through to the defensive default (same as
	// direct). Validated upstream, but we cover the branch here.
	fallback := wkdRoot("/out", "example.com", Method("bogus"))
	assert.Equal(t, filepath.Join("/out", ".well-known", "openpgpkey"), fallback)
}

// --- isHostnameRune branches -----------------------------------------

func TestIsHostnameRune_Classes(t *testing.T) {
	t.Parallel()

	cases := map[rune]bool{
		'a': true, 'z': true,
		'A': true, 'Z': true, // uppercase branch
		'0': true, '9': true, // digit branch
		'.': true, '-': true,
		'_': false, '/': false, ' ': false, '$': false,
	}

	for r, want := range cases {
		assert.Equalf(t, want, isHostnameRune(r), "isHostnameRune(%q)", r)
	}
}

// --- validateInputs direct coverage ----------------------------------

func TestValidateInputs_Errors(t *testing.T) {
	t.Parallel()

	key := mintRSAArmored(t, "V", "v@example.com")
	entry := Entry{Email: "v@example.com", Keys: [][]byte{key}}

	t.Run("no entries", func(t *testing.T) {
		t.Parallel()

		_, _, err := validateInputs("example.com", Options{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one Entry")
	})

	t.Run("bad domain", func(t *testing.T) {
		t.Parallel()

		_, _, err := validateInputs("bad/domain", Options{}, []Entry{entry})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path separator")
	})

	t.Run("bad method", func(t *testing.T) {
		t.Parallel()

		_, _, err := validateInputs("example.com", Options{Method: "weird"}, []Entry{entry})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Method")
	})

	t.Run("empty entry email", func(t *testing.T) {
		t.Parallel()

		_, _, err := validateInputs("example.com", Options{}, []Entry{{Email: "", Keys: [][]byte{key}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is empty")
	})

	t.Run("entry no keys", func(t *testing.T) {
		t.Parallel()

		_, _, err := validateInputs("example.com", Options{}, []Entry{{Email: "v@example.com"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no Keys")
	})

	t.Run("ok", func(t *testing.T) {
		t.Parallel()

		method, merged, err := validateInputs("example.com", Options{}, []Entry{entry})
		require.NoError(t, err)
		assert.Equal(t, MethodAdvanced, method)
		assert.Len(t, merged, 1)
	})
}

// --- WriteWKDTree filesystem error paths -----------------------------

// skipIfRoot guards the chmod-based read-only-directory tricks: root
// ignores DAC permission bits, so the write would succeed.
func skipIfRoot(t *testing.T) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("write-permission error paths cannot be exercised as root")
	}
}

func TestWriteWKDTree_MkdirAllFails(t *testing.T) {
	skipIfRoot(t)

	key := mintRSAArmored(t, "M", "m@example.com")

	// outDir is a regular file: MkdirAll under it cannot create the
	// hu/ directory.
	root := t.TempDir()
	outFile := filepath.Join(root, "not-a-dir")
	require.NoError(t, os.WriteFile(outFile, []byte("x"), 0o600))

	_, err := WriteWKDTree(outFile, "example.com", Options{},
		Entry{Email: "m@example.com", Keys: [][]byte{key}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WKD hu directory")
}

func TestWriteWKDTree_PolicyWriteFails(t *testing.T) {
	skipIfRoot(t)

	key := mintRSAArmored(t, "P", "p@example.com")

	outDir := t.TempDir()

	// Pre-create the domain root with hu/ present, then make the root
	// read-only so the policy WriteFile into the root is denied.
	root := filepath.Join(outDir, ".well-known", "openpgpkey", "example.com")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "hu"), 0o755))
	require.NoError(t, os.Chmod(root, 0o500))

	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	_, err := WriteWKDTree(outDir, "example.com", Options{},
		Entry{Email: "p@example.com", Keys: [][]byte{key}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing policy")
}

func TestWriteWKDTree_SubmissionAddressInvalid(t *testing.T) {
	t.Parallel()

	key := mintRSAArmored(t, "S", "s@example.com")
	outDir := t.TempDir()

	_, err := WriteWKDTree(outDir, "example.com",
		Options{SubmissionAddress: "not-a-valid-address"},
		Entry{Email: "s@example.com", Keys: [][]byte{key}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SubmissionAddress")
}

func TestWriteWKDTree_SubmissionAddressWriteFails(t *testing.T) {
	skipIfRoot(t)

	key := mintRSAArmored(t, "S", "s@example.com")
	outDir := t.TempDir()

	root := filepath.Join(outDir, ".well-known", "openpgpkey", "example.com")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "hu"), 0o755))
	// Pre-create policy so the policy write succeeds (overwrite of an
	// existing writable file), then make the directory read-only so the
	// NEW submission-address file cannot be created.
	require.NoError(t, os.WriteFile(filepath.Join(root, "policy"), nil, 0o644))
	require.NoError(t, os.Chmod(root, 0o500))

	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	_, err := WriteWKDTree(outDir, "example.com",
		Options{SubmissionAddress: "s@example.com"},
		Entry{Email: "s@example.com", Keys: [][]byte{key}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "submission-address")
}

func TestWriteWKDTree_HuWriteFails(t *testing.T) {
	skipIfRoot(t)

	key := mintRSAArmored(t, "H", "h@example.com")
	outDir := t.TempDir()

	root := filepath.Join(outDir, ".well-known", "openpgpkey", "example.com")
	huDir := filepath.Join(root, "hu")
	require.NoError(t, os.MkdirAll(huDir, 0o755))
	// Pre-create policy (writable) so metadata succeeds, then make the
	// hu/ directory read-only so the hu file write is denied.
	require.NoError(t, os.WriteFile(filepath.Join(root, "policy"), nil, 0o644))
	require.NoError(t, os.Chmod(huDir, 0o500))

	t.Cleanup(func() { _ = os.Chmod(huDir, 0o755) })

	_, err := WriteWKDTree(outDir, "example.com", Options{},
		Entry{Email: "h@example.com", Keys: [][]byte{key}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hu/")
}

func TestWriteWKDTree_HuEncodeFails_BadKey(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	// A valid email but a key that cannot be parsed: writeHuFiles
	// reaches concatBinaryKeys and fails before writing.
	_, err := WriteWKDTree(outDir, "example.com", Options{},
		Entry{Email: "bad@example.com", Keys: [][]byte{[]byte("not a key")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encoding keys")
}

// --- DetachSign / loadIdentityFromArmoredKey branches ----------------

func TestDetachSign_MultiEntityKey_Rejected(t *testing.T) {
	t.Parallel()

	// Two entities serialised into a single armor block => ring len 2.
	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, mintRSAEntity(t).Serialize(w))
	require.NoError(t, mintRSAEntity(t).Serialize(w))
	require.NoError(t, w.Close())

	signer, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	_, err = DetachSign(signer, buf.Bytes(), bytes.NewReader([]byte("x")), fixedCovTime)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly one entity")
}

func TestDetachSign_NonRSAPublicKey_Rejected(t *testing.T) {
	t.Parallel()

	// EdDSA public key block: parses (has a UID), the signer is RSA, so
	// the RSA-signer gate passes, but the embedded primary key is not
	// *rsa.PublicKey -> assertSignerMatchesPublicKey rejects.
	pub := mintEdDSAArmored(t)

	signer, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	_, err = DetachSign(signer, pub, bytes.NewReader([]byte("x")), fixedCovTime)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signer is RSA")
}

func TestLoadIdentityFromArmoredKey_OK(t *testing.T) {
	t.Parallel()

	pub := mintRSAArmored(t, "Load", "load@example.com")

	ent, err := loadIdentityFromArmoredKey(pub)
	require.NoError(t, err)
	require.NotNil(t, ent)
	assert.NotEmpty(t, ent.Identities)
}

// --- relTo --------------------------------------------------------------

func TestRelTo(t *testing.T) {
	t.Parallel()

	base := filepath.Join("/a", "b")
	full := filepath.Join("/a", "b", "c", "d")
	assert.Equal(t, filepath.Join("c", "d"), relTo(base, full))

	if runtime.GOOS != "windows" {
		// filepath.Rel cannot relate an absolute target to a relative
		// base; relTo falls back to returning the full path.
		got := relTo("relative-base", "/absolute/target")
		assert.Equal(t, "/absolute/target", got)
	}
}
