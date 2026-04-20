package credentials_test

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

func TestAvailableModes_DefaultBuildOmitsKeychain(t *testing.T) {
	t.Parallel()

	modes := credentials.AvailableModes()

	// The default build always offers env-var and literal. Keychain
	// is present only in the `keychain`-tagged build.
	assert.Contains(t, modes, credentials.ModeEnvVar)
	assert.Contains(t, modes, credentials.ModeLiteral)

	// In the default (no-tag) build, ModeKeychain must not be
	// present. This guards against accidental inclusion.
	if !credentials.KeychainAvailable() {
		assert.NotContains(t, modes, credentials.ModeKeychain,
			"keychain mode must not surface when KeychainAvailable() is false")
	}
}

func TestAvailableModes_OrderedByPreference(t *testing.T) {
	t.Parallel()

	modes := credentials.AvailableModes()

	// Env-var must always come first — the wizard presents modes in
	// this order and the top entry is the default.
	require.NotEmpty(t, modes)
	assert.Equal(t, credentials.ModeEnvVar, modes[0])

	// Literal is always last — it's the least-preferred option.
	assert.Equal(t, credentials.ModeLiteral, modes[len(modes)-1])
}

func TestKeychainAvailable_MatchesBuildTag(t *testing.T) {
	t.Parallel()

	// In a default build, KeychainAvailable must return false so the
	// setup wizard suppresses the keychain option. The
	// keychain-tagged build's own tests assert the opposite.
	got := credentials.KeychainAvailable()
	t.Logf("KeychainAvailable=%v in this build", got)

	// In the stub build, operations must return ErrCredentialUnsupported.
	if !got {
		err := credentials.Store("svc", "acct", "secret")
		require.ErrorIs(t, err, credentials.ErrCredentialUnsupported)

		_, err = credentials.Retrieve("svc", "acct")
		require.ErrorIs(t, err, credentials.ErrCredentialUnsupported)

		err = credentials.Delete("svc", "acct")
		require.ErrorIs(t, err, credentials.ErrCredentialUnsupported)
	}
}

func TestModeString_RoundTrip(t *testing.T) {
	t.Parallel()

	// Mode values are serialised into YAML config; their string form
	// must remain stable. This test pins the expected spelling.
	assert.Equal(t, "env", string(credentials.ModeEnvVar))
	assert.Equal(t, "keychain", string(credentials.ModeKeychain))
	assert.Equal(t, "literal", string(credentials.ModeLiteral))
}

func TestErrCredentialSentinels_Distinct(t *testing.T) {
	t.Parallel()

	// ErrCredentialNotFound and ErrCredentialUnsupported are
	// distinct sentinels — resolvers use the difference to decide
	// whether to fall through (Unsupported) or abort (NotFound in
	// certain caller contexts).
	require.NotErrorIs(t, credentials.ErrCredentialNotFound, credentials.ErrCredentialUnsupported)
	require.NotErrorIs(t, credentials.ErrCredentialUnsupported, credentials.ErrCredentialNotFound)

	// Both wrap cleanly with errors.Is.
	wrapped := errors.Wrap(credentials.ErrCredentialUnsupported, "context")
	require.ErrorIs(t, wrapped, credentials.ErrCredentialUnsupported)
}
