package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestConfigAdapter_LoadsTypedSections(t *testing.T) {
	t.Setenv("GTB_OPENAI_API_KEY", "env-literal-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader(`
ai:
  provider: openai
  request_timeout: 9s
  fallback:
    enabled: true
    providers:
      - openai
      - claude
openai:
  api:
    key: file-literal-key
    env: CUSTOM_OPENAI_TOKEN
    keychain: service/account
`)),
	)

	runtime, err := loadRuntimeConfig(c)
	require.NoError(t, err)

	assert.Equal(t, ProviderOpenAI, runtime.Provider)
	assert.Equal(t, 9*time.Second, runtime.RequestTimeout)
	assert.True(t, runtime.Fallback.Enabled)
	assert.Equal(t, []Provider{ProviderOpenAI, ProviderClaude}, runtime.Fallback.Providers)

	credentials, err := loadCredentialConfig(c, ProviderOpenAI)
	require.NoError(t, err)

	assert.Equal(t, "CUSTOM_OPENAI_TOKEN", credentials.Env)
	assert.Equal(t, "service/account", credentials.Keychain)
	assert.Equal(t, "env-literal-key", credentials.Key)
}

func TestNew_AppliesTypedConfigBeforeProviderFactory(t *testing.T) {
	t.Setenv("GTB_OPENAI_API_KEY", "env-literal-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader(`
ai:
  provider: openai
  request_timeout: 11s
openai:
  api:
    key: file-literal-key
`)),
	)

	var got Config
	registryMu.RLock()
	original := providerRegistry[ProviderOpenAI]
	registryMu.RUnlock()
	RegisterProvider(ProviderOpenAI, func(_ context.Context, _ *props.Props, cfg Config) (ChatClient, error) {
		got = cfg

		return &fakeClient{chatReply: "ok"}, nil
	})
	t.Cleanup(func() {
		registryMu.Lock()
		if original == nil {
			delete(providerRegistry, ProviderOpenAI)
		} else {
			providerRegistry[ProviderOpenAI] = original
		}
		registryMu.Unlock()
	})

	client, err := New(context.Background(), &props.Props{
		Logger: logger.NewNoop(),
		Config: c,
	}, Config{})
	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Equal(t, ProviderOpenAI, got.Provider)
	assert.Equal(t, 11*time.Second, got.RequestTimeout)
	assert.Equal(t, CredentialConfig{Key: "env-literal-key"}, got.Credentials)
}
