package setup

import (
	"bytes"
	"io"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
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
