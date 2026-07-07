package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// resolveAPIKey precedence: direct > {provider}.api.env var ref >
// {provider}.api.key literal > envFallback.

func TestGetOpenAICredentials(t *testing.T) {
	t.Run("token provided directly", func(t *testing.T) {
		token, err := getOpenAICredentials(t.Context(), "direct-token", CredentialConfig{})
		require.NoError(t, err)
		assert.Equal(t, "direct-token", token)
	})

	t.Run("token from config literal", func(t *testing.T) {
		token, err := getOpenAICredentials(t.Context(), "", CredentialConfig{Key: "config-token"})
		require.NoError(t, err)
		assert.Equal(t, "config-token", token)
	})

	t.Run("token from config env var reference", func(t *testing.T) {
		t.Setenv("CUSTOM_OPENAI_KEY", "referenced-token")

		token, err := getOpenAICredentials(t.Context(), "", CredentialConfig{Env: "CUSTOM_OPENAI_KEY"})
		require.NoError(t, err)
		assert.Equal(t, "referenced-token", token)
	})

	t.Run("env ref with unset var falls through to literal", func(t *testing.T) {
		// Stale reference to an env var that isn't set must not
		// mask the literal fallback — the resolver falls through.
		t.Setenv("UNSET_OPENAI_KEY", "")

		token, err := getOpenAICredentials(t.Context(), "", CredentialConfig{
			Env: "UNSET_OPENAI_KEY",
			Key: "literal-fallback",
		})
		require.NoError(t, err)
		assert.Equal(t, "literal-fallback", token)
	})

	t.Run("token from well-known fallback env", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "env-token")

		token, err := getOpenAICredentials(t.Context(), "", CredentialConfig{})
		require.NoError(t, err)
		assert.Equal(t, "env-token", token)
	})

	t.Run("no token anywhere", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "")

		_, err := getOpenAICredentials(t.Context(), "", CredentialConfig{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OpenAI token is required")
	})

	t.Run("nil config falls through to env", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "")

		_, err := getOpenAICredentials(t.Context(), "", CredentialConfig{})
		assert.Error(t, err)
	})

	t.Run("whitespace-only values fall through", func(t *testing.T) {
		// A whitespace-only config value must not satisfy the
		// "populated" check; it falls through to the next step.
		t.Setenv(EnvOpenAIKey, "   ")

		_, err := getOpenAICredentials(t.Context(), "", CredentialConfig{
			Env:      "   ",
			Keychain: "   ",
			Key:      "   ",
		})
		require.Error(t, err)
	})

	t.Run("keychain reference unavailable in default build falls through", func(t *testing.T) {
		// With no keychain build tag compiled in, a populated
		// {provider}.api.keychain reference must fall through to
		// the literal step rather than failing the whole resolve.
		token, err := getOpenAICredentials(t.Context(), "", CredentialConfig{
			Keychain: "mytool/openai.api",
			Key:      "literal-wins",
		})
		require.NoError(t, err)
		assert.Equal(t, "literal-wins", token)
	})
}

func TestRegisterProvider_CustomProvider(t *testing.T) {
	called := false
	RegisterProvider("test-custom", func(_ context.Context, _ *props.Props, _ Config) (ChatClient, error) {
		called = true
		return nil, nil
	})
	t.Cleanup(func() {
		registryMu.Lock()
		delete(providerRegistry, "test-custom")
		registryMu.Unlock()
	})

	registryMu.RLock()
	_, ok := providerRegistry["test-custom"]
	registryMu.RUnlock()

	assert.True(t, ok)
	assert.False(t, called, "factory should not be called yet")
}
