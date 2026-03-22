package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	mockConfig "github.com/phpboyscout/gtb/mocks/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
)

func TestGetOpenAICredentials(t *testing.T) {
	t.Run("token provided directly", func(t *testing.T) {
		token, err := getOpenAICredentials("direct-token", nil)
		assert.NoError(t, err)
		assert.Equal(t, "direct-token", token)
	})

	t.Run("token from config", func(t *testing.T) {
		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("config-token")

		token, err := getOpenAICredentials("", cfg)
		assert.NoError(t, err)
		assert.Equal(t, "config-token", token)
	})

	t.Run("token from environment", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "env-token")

		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("")

		token, err := getOpenAICredentials("", cfg)
		assert.NoError(t, err)
		assert.Equal(t, "env-token", token)
	})

	t.Run("no token anywhere", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "")

		cfg := mockConfig.NewMockContainable(t)
		cfg.EXPECT().GetString(ConfigKeyOpenAIKey).Return("")

		_, err := getOpenAICredentials("", cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "OpenAI token is required")
	})

	t.Run("nil config falls through to env", func(t *testing.T) {
		t.Setenv(EnvOpenAIKey, "")

		_, err := getOpenAICredentials("", nil)
		assert.Error(t, err)
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
