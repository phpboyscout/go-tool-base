package setup

import (
	"context"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgetest "gitlab.com/phpboyscout/go/forge/test"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func newAuthorityKey(t *testing.T) testSigningKey {
	t.Helper()

	k, err := generateTestSigningKey(&packet.Config{
		Algorithm: packet.PubKeyAlgoEdDSA,
		Curve:     packet.Curve25519,
	}, "Rotation Authority Test", "authority@test.example")
	require.NoError(t, err)

	return k
}

func assertAnchorOnlyRefused(t *testing.T, r verify.KeyResolver, release, authority testSigningKey) {
	t.Helper()

	ts, err := r.Resolve(context.Background())
	require.NoError(t, err)
	assert.Len(t, ts.Fingerprints(), 2, "the anchor-only key stays in the set the cross-check compares")

	manifest := []byte("deadbeef  gtb_Linux_x86_64.tar.gz\n")

	_, err = ts.VerifyManifestSignatureSigner(manifest, detachSign(t, authority.entity, manifest))
	require.ErrorIs(t, err, verify.ErrSignatureInvalid, "a manifest signed by an anchor-only key must not verify")

	_, err = ts.VerifyManifestSignatureSigner(manifest, detachSign(t, release.entity, manifest))
	require.NoError(t, err, "the release key still verifies")
}

func TestBuildDefaultKeyResolver_AnchorOnlyKeyCannotSign(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	authority := newAuthorityKey(t)

	s := &SelfUpdater{
		logger:         logger.NewNoop(),
		embeddedKeys:   [][]byte{testEd25519.armoredPub},
		anchorOnlyKeys: [][]byte{authority.armoredPub},
		keySource:      verify.DefaultKeySource,
	}
	require.NoError(t, s.buildDefaultKeyResolver())

	assertAnchorOnlyRefused(t, s.keyResolver, testEd25519, authority)
}

func TestBuildDefaultKeyResolver_MalformedAnchorOnlyKey(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	s := &SelfUpdater{
		logger:         logger.NewNoop(),
		embeddedKeys:   [][]byte{testEd25519.armoredPub},
		anchorOnlyKeys: [][]byte{[]byte("not a key")},
		keySource:      verify.DefaultKeySource,
	}
	require.Error(t, s.buildDefaultKeyResolver())
}

func TestNewUpdater_SeedsAnchorOnlyKeysFromProps(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	authority := newAuthorityKey(t)

	p := injectProps(t, props.Tool{
		ReleaseProvider: forgetest.New(forgetest.WithRelease("v1.0.0")),
		Signing: props.SigningConfig{
			EmbeddedKeys:   [][]byte{testEd25519.armoredPub},
			AnchorOnlyKeys: [][]byte{authority.armoredPub},
		},
	})

	u, err := NewUpdater(context.Background(), p, "", false)
	require.NoError(t, err)

	assertAnchorOnlyRefused(t, u.keyResolver, testEd25519, authority)
}
