package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/config"
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
	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("no-slash-here")
	sub.EXPECT().GetString("username.env").Return("")
	sub.EXPECT().GetString("username").Return("u")
	sub.EXPECT().GetString("app_password.env").Return("")
	sub.EXPECT().GetString("app_password").Return("p")

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	user, pass, err := resolveCredentials(cfg)
	require.NoError(t, err)
	assert.Equal(t, "u", user)
	assert.Equal(t, "p", pass)
}

func TestResolveCredentials_NilConfig(t *testing.T) {
	t.Parallel()

	user, pass, err := resolveCredentials(nil)
	require.NoError(t, err)
	assert.Empty(t, user)
	assert.Empty(t, pass)
}

func TestResolveCredentials_NilSubReturn(t *testing.T) {
	t.Parallel()

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(nil)

	user, pass, err := resolveCredentials(cfg)
	require.NoError(t, err)
	assert.Empty(t, user)
	assert.Empty(t, pass)
}

// Ensures we surface the config.Containable type so the test package
// can assert on it without importing unused symbols — guards against
// the internal type drifting out of the public interface.
var _ config.Containable = (*mockcfg.MockContainable)(nil)
