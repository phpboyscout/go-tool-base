package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// resolveAPIKey precedence: direct > {provider}.api.env var ref >
// {provider}.api.key literal > envFallback.

func TestGetOpenAICredentials(t *testing.T) {
	t.Run("token provided directly", func(t *testing.T) {
		token, err := getOpenAICredentials("direct-token", nil)
		require.NoError(t, err)
		assert.Equal(t, "direct-token", token)
	})

	t.Run("token from config literal", func(t *testing.T) {
		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIEnv).Return("")
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("config-token")

		token, err := getOpenAICredentials("", cfg)
		require.NoError(t, err)
		assert.Equal(t, "config-token", token)
	})

	t.Run("token from config env var reference", func(t *testing.T) {
		t.Setenv("CUSTOM_OPENAI_KEY", "referenced-token")

		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIEnv).Return("CUSTOM_OPENAI_KEY")

		token, err := getOpenAICredentials("", cfg)
		require.NoError(t, err)
		assert.Equal(t, "referenced-token", token)
	})

	t.Run("env ref with unset var falls through to literal", func(t *testing.T) {
		// Stale reference to an env var that isn't set must not
		// mask the literal fallback — the resolver falls through.
		t.Setenv("UNSET_OPENAI_KEY", "")

		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIEnv).Return("UNSET_OPENAI_KEY")
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("literal-fallback")

		token, err := getOpenAICredentials("", cfg)
		require.NoError(t, err)
		assert.Equal(t, "literal-fallback", token)
	})

	t.Run("token from well-known fallback env", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "env-token")

		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIEnv).Return("")
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("")

		token, err := getOpenAICredentials("", cfg)
		require.NoError(t, err)
		assert.Equal(t, "env-token", token)
	})

	t.Run("no token anywhere", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "")

		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIEnv).Return("")
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("")

		_, err := getOpenAICredentials("", cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OpenAI token is required")
	})

	t.Run("nil config falls through to env", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "")

		_, err := getOpenAICredentials("", nil)
		assert.Error(t, err)
	})

	t.Run("whitespace-only values fall through", func(t *testing.T) {
		// A whitespace-only config value must not satisfy the
		// "populated" check; it falls through to the next step.
		t.Setenv(EnvOpenAIKey, "   ")

		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIEnv).Return("   ")
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("   ")

		_, err := getOpenAICredentials("", cfg)
		require.Error(t, err)
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
