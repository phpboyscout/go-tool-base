package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Build-tag-agnostic resolver behaviour. The "keychain entry missing
// / unreachable falls through to literal" case is split across two
// files: this one covers the bits that don't touch the keychain
// backend at all (malformed refs, nil config, nil Sub return). The
// positive keychain path (valid blob, precedence over literal,
// corrupt-JSON abort) lives in credentials_keychain_test.go behind
// `!nokeychain`. The stub-build fall-through assertion lives in
// credentials_nokeychain_test.go behind `nokeychain`.

func TestResolveCredentials_MalformedKeychainRefFallsThrough(t *testing.T) {
	t.Parallel()

	// A keychain reference without a slash must not crash resolution —
	// it's treated as absent and the chain continues.
	cfg := bitbucketConfig(map[string]string{
		"keychain":     "no-slash-here",
		"username":     "u",
		"app_password": "p",
	})

	user, pass, err := resolveCredentials(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "u", user)
	assert.Equal(t, "p", pass)
}

func TestResolveCredentials_NilConfig(t *testing.T) {
	t.Parallel()

	user, pass, err := resolveCredentials(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, user)
	assert.Empty(t, pass)
}

func TestResolveCredentials_NilSubReturn(t *testing.T) {
	t.Parallel()

	user, pass, err := resolveCredentials(t.Context(), testReleaseConfig{})
	require.NoError(t, err)
	assert.Empty(t, user)
	assert.Empty(t, pass)
}
