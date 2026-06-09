package setup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEmbeddedResolver_Resolve(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	r := NewEmbeddedResolver(testEd25519.armoredPub, testRSA4096.armoredPub)
	require.NotNil(t, r)
	assert.Equal(t, "embedded", r.Name())

	ts, err := r.Resolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ts)
	assert.Len(t, ts.Fingerprints(), 2)
}

func TestNewEmbeddedResolver_PanicsOnWeakKey(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	assert.Panics(t, func() {
		NewEmbeddedResolver(testRSA1024.armoredPub)
	}, "weak embedded key must abort at construction")
}

func TestNewEmbeddedResolver_PanicsOnMalformedKey(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() {
		NewEmbeddedResolver([]byte("not a real armored key"))
	}, "malformed embedded key must abort at construction")
}

func TestNewEmbeddedResolver_PanicsOnNoKeys(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() {
		NewEmbeddedResolver()
	}, "empty embedded key set must abort at construction")
}

func TestEmbeddedResolver_ResolveCachesTrustSet(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	r := NewEmbeddedResolver(testEd25519.armoredPub)

	ts1, err := r.Resolve(context.Background())
	require.NoError(t, err)
	ts2, err := r.Resolve(context.Background())
	require.NoError(t, err)

	assert.Same(t, ts1, ts2, "Resolve should return the same TrustSet pointer on every call")
}

func TestEmbeddedResolver_VerifyEndToEnd(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	r := NewEmbeddedResolver(testEd25519.armoredPub)

	ts, err := r.Resolve(context.Background())
	require.NoError(t, err)

	manifest := []byte("abc123  artifact.tar.gz\n")
	sig := detachSign(t, testEd25519.entity, manifest)

	assert.NoError(t, ts.VerifyManifestSignature(manifest, sig),
		"resolver-produced TrustSet must verify a signature from one of its keys")
}
