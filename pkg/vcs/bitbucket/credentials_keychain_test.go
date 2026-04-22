package bitbucket

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// storeBlob writes a JSON blob through the active keychain backend
// so a test can exercise the retrieval path without hand-writing
// escape sequences. Pair with [credtest.Install] to target the
// in-memory backend rather than any real OS keychain.
func storeBlob(t *testing.T, service, account, username, appPassword string) {
	t.Helper()

	blob, err := json.Marshal(map[string]string{
		"username":     username,
		"app_password": appPassword,
	})
	require.NoError(t, err)
	require.NoError(t, credentials.Store(t.Context(), service, account, string(blob)))
}

// The happy path: when `bitbucket.keychain` points at a valid JSON
// blob, both fields are populated without consulting any literal or
// env-var config keys.
func TestResolveCredentials_KeychainBlobPopulatesBothFields(t *testing.T) {
	credtest.Install(t)
	storeBlob(t, "mytool", "bitbucket.auth", "kcuser", "kcpass")

	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("mytool/bitbucket.auth")
	sub.EXPECT().GetString("username.env").Return("")
	sub.EXPECT().GetString("app_password.env").Return("")

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	user, pass, err := resolveCredentials(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "kcuser", user)
	assert.Equal(t, "kcpass", pass)
}

// A corrupt JSON entry must abort resolution, not fall through. This
// surfaces the problem to the user (R3 in the spec) so a broken
// keychain item cannot be masked by a stale literal in config.
func TestResolveCredentials_CorruptKeychainAborts(t *testing.T) {
	credtest.Install(t)
	require.NoError(t, credentials.Store(t.Context(), "mytool", "bitbucket.auth", "{not json"))

	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("mytool/bitbucket.auth")

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	_, _, err := resolveCredentials(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}

// An incomplete blob (missing one of the two fields) is as invalid as
// corrupt JSON — both fields must be present or the entry is treated
// as broken.
func TestResolveCredentials_IncompleteKeychainAborts(t *testing.T) {
	credtest.Install(t)
	require.NoError(t, credentials.Store(t.Context(), "mytool", "bitbucket.auth", `{"username":"only"}`))

	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("mytool/bitbucket.auth")

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	_, _, err := resolveCredentials(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing username or app_password")
}

// Per-field precedence: an env-var reference for one field wins over
// the keychain blob's value for that same field. The other field
// still comes from the keychain blob. Supports partial rotation.
func TestResolveCredentials_EnvVarOverridesKeychainPerField(t *testing.T) {
	credtest.Install(t)
	storeBlob(t, "mytool", "bitbucket.auth", "kcuser", "kcpass")

	t.Setenv("ROTATED_USER", "env-user")

	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("mytool/bitbucket.auth")
	sub.EXPECT().GetString("username.env").Return("ROTATED_USER")
	sub.EXPECT().GetString("app_password.env").Return("")

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	user, pass, err := resolveCredentials(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "env-user", user, "env-var reference wins over keychain")
	assert.Equal(t, "kcpass", pass, "other field still resolved from keychain")
}

// Keychain wins over literal: a populated keychain blob must beat
// both `bitbucket.username` and `bitbucket.app_password` literal
// config entries. Literals are the fallback, not the leader.
func TestResolveCredentials_KeychainBeatsLiteral(t *testing.T) {
	credtest.Install(t)
	storeBlob(t, "mytool", "bitbucket.auth", "kcuser", "kcpass")

	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("mytool/bitbucket.auth")
	sub.EXPECT().GetString("username.env").Return("")
	sub.EXPECT().GetString("app_password.env").Return("")
	// Literals are available but must not be returned.
	sub.EXPECT().GetString("username").Return("stale-literal-user").Maybe()
	sub.EXPECT().GetString("app_password").Return("stale-literal-pass").Maybe()

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	user, pass, err := resolveCredentials(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "kcuser", user)
	assert.Equal(t, "kcpass", pass)
}

// A corrupt keychain blob propagates as a provider construction
// error so downstream setup commands can surface the problem to the
// user instead of silently running with stale literals.
func TestNewReleaseProvider_CorruptKeychainAborts(t *testing.T) {
	credtest.Install(t)
	require.NoError(t, credentials.Store(t.Context(), "mytool", "bitbucket.auth", "{not json"))

	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("mytool/bitbucket.auth")

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	_, err := NewReleaseProvider(release.ReleaseSourceConfig{}, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}

// A configured keychain reference pointing at a missing entry falls
// through silently to the literal step so the user is never stranded
// by a removed keychain item while a literal fallback is still present.
func TestResolveCredentials_KeychainMissingFallsThrough(t *testing.T) {
	credtest.Install(t)
	// Intentionally no Store — the entry does not exist.

	sub := mockcfg.NewMockContainable(t)
	sub.EXPECT().GetString("keychain").Return("mytool/bitbucket.auth")
	sub.EXPECT().GetString("username.env").Return("")
	sub.EXPECT().GetString("username").Return("literal-user")
	sub.EXPECT().GetString("app_password.env").Return("")
	sub.EXPECT().GetString("app_password").Return("literal-pass")

	cfg := mockcfg.NewMockContainable(t)
	cfg.EXPECT().Sub("bitbucket").Return(sub)

	user, pass, err := resolveCredentials(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "literal-user", user)
	assert.Equal(t, "literal-pass", pass)
}
