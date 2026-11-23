package setup

import (
	"bytes"
	"crypto"
	"io"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

func TestCheckTrustKeys(t *testing.T) {
	t.Parallel()

	a, b := armoredTestKey(t, "a"), armoredTestKey(t, "b")

	for name, tc := range map[string]struct {
		release, anchorOnly [][]byte
		want                string
	}{
		"none":                     {},
		"distinct keys":            {release: [][]byte{a}, anchorOnly: [][]byte{b}},
		"in both sets":             {release: [][]byte{a}, anchorOnly: [][]byte{b, a}, want: "is both a release key and an anchor-only key"},
		"twice among release keys": {release: [][]byte{a, b, a}, want: "twice among the release keys"},
		"twice among anchor-only":  {anchorOnly: [][]byte{b, b}, want: "twice among the anchor-only keys"},
		"the same key re-armoured": {release: [][]byte{a}, anchorOnly: [][]byte{rearmoured(t, a)}, want: "is both a release key and an anchor-only key"},
	} {
		err := CheckTrustKeys(tc.release, tc.anchorOnly)
		if tc.want == "" {
			require.NoError(t, err, name)

			continue
		}

		require.ErrorIs(t, err, ErrDuplicateTrustKey, name)
		assert.Contains(t, err.Error(), tc.want, name)
		assert.Contains(t, errors.FlattenHints(err), "internal/trustkeys/authority/", name)
	}
}

func TestCheckTrustKeys_RefusesAKeyThatDoesNotLoad(t *testing.T) {
	t.Parallel()

	err := CheckTrustKeys(nil, [][]byte{[]byte("not a key")})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading the anchor-only keys")
}

func armoredTestKey(t *testing.T, name string) []byte {
	t.Helper()

	k, err := generateTestSigningKey(&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}, name, name+"@test.example")
	require.NoError(t, err)

	return k.armoredPub
}

// rearmoured is key exported again with an armour comment: different bytes,
// the same key.
func rearmoured(t *testing.T, key []byte) []byte {
	t.Helper()

	block, err := armor.Decode(bytes.NewReader(key))
	require.NoError(t, err)

	body, err := io.ReadAll(block.Body)
	require.NoError(t, err)

	var out bytes.Buffer

	w, err := armor.Encode(&out, block.Type, map[string]string{"Comment": "exported again"})
	require.NoError(t, err)

	_, err = w.Write(body)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NotEqual(t, key, out.Bytes())

	return out.Bytes()
}

// #218: an anchor-only key cross-certified as a release key's signing subkey
// signs releases through it, though no primary appears twice.
func TestCheckTrustKeys_RefusesAKeyBoundAsAnotherKeysSubkey(t *testing.T) {
	t.Parallel()

	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}
	release, err := generateTestSigningKey(cfg, "release", "release@test.example")
	require.NoError(t, err)
	authority, err := generateTestSigningKey(cfg, "authority", "authority@test.example")
	require.NoError(t, err)

	other, err := generateTestSigningKey(cfg, "other", "other@test.example")
	require.NoError(t, err)

	bound := withSigningSubkey(t, release.entity, authority.entity)
	boundToOther := withSigningSubkey(t, other.entity, authority.entity)

	ts, err := verify.LoadTrustSet(bound)
	require.NoError(t, err)
	manifest := []byte("checksums")
	require.NoError(t, ts.VerifyManifestSignature(manifest, detachSign(t, authority.entity, manifest)),
		"the fixture is real: the release key verifies what the authority key signs")

	for name, tc := range map[string]struct {
		release, anchorOnly [][]byte
		want                string
	}{
		"anchor-only key as a release key's subkey":   {release: [][]byte{bound}, anchorOnly: [][]byte{authority.armoredPub}, want: "is a subkey of release key"},
		"release key as another release key's subkey": {release: [][]byte{authority.armoredPub, bound}, want: "is a subkey of release key"},
		"one subkey under a release and an anchor-only key": {
			release: [][]byte{bound}, anchorOnly: [][]byte{boundToOther},
			want: "is a subkey of release key " + fingerprintOf(t, release) + " and of anchor-only key " + fingerprintOf(t, other),
		},
		"release key as an anchor-only key's subkey": {
			release: [][]byte{authority.armoredPub}, anchorOnly: [][]byte{boundToOther},
			want: "is a subkey of anchor-only key " + fingerprintOf(t, other) + " and also a release key",
		},
		"a key in both sets": {release: [][]byte{authority.armoredPub}, anchorOnly: [][]byte{authority.armoredPub}, want: "is both a release key and an anchor-only key"},
	} {
		err := CheckTrustKeys(tc.release, tc.anchorOnly)

		require.ErrorIs(t, err, ErrDuplicateTrustKey, name)
		assert.Contains(t, err.Error(), tc.want, name)
		assert.Contains(t, err.Error(), fingerprintOf(t, authority), name)
	}

	require.NoError(t, CheckTrustKeys([][]byte{bound}, nil), "a subkey of its own is no duplicate")
}

// withSigningSubkey is primary's public key with sub's primary key bound to it
// as a cross-certified signing subkey, armoured.
func withSigningSubkey(t *testing.T, primary, sub *openpgp.Entity) []byte {
	t.Helper()

	pub := *sub.PrimaryKey
	pub.IsSubkey = true
	priv := *sub.PrivateKey
	priv.PublicKey = pub

	sig := bindingSignature(primary.PrimaryKey, packet.SigTypeSubkeyBinding)
	sig.FlagsValid, sig.FlagSign = true, true
	sig.EmbeddedSignature = bindingSignature(&pub, packet.SigTypePrimaryKeyBinding)
	require.NoError(t, sig.EmbeddedSignature.CrossSignKey(&pub, primary.PrimaryKey, &priv, nil))
	require.NoError(t, sig.SignKey(&pub, primary.PrivateKey, nil))

	e := *primary
	e.Subkeys = append(append([]openpgp.Subkey(nil), primary.Subkeys...), openpgp.Subkey{PublicKey: &pub, PrivateKey: &priv, Sig: sig})

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, e.Serialize(w))
	require.NoError(t, w.Close())

	return buf.Bytes()
}

func bindingSignature(signer *packet.PublicKey, sigType packet.SignatureType) *packet.Signature {
	return &packet.Signature{
		Version: signer.Version, SigType: sigType, PubKeyAlgo: signer.PubKeyAlgo, Hash: crypto.SHA256,
		CreationTime: time.Now(), IssuerKeyId: &signer.KeyId, IssuerFingerprint: signer.Fingerprint,
	}
}
