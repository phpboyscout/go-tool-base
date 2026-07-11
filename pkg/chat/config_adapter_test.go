package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestConfigAdapter_LoadsTypedSections(t *testing.T) {
	t.Setenv("GTB_OPENAI_API_KEY", "env-literal-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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

func TestApplyRuntimeConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetString(ConfigKeyAIProvider).Return(string(ProviderGemini)).Once()

	chatConfig := Config{}
	err := applyRuntimeConfig(&props.Props{Config: cfg}, &chatConfig)
	require.NoError(t, err)

	assert.Equal(t, ProviderGemini, chatConfig.Provider)
}

func TestLoadRuntimeConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetString(ConfigKeyAIProvider).Return(string(ProviderClaude)).Once()
	cfg.EXPECT().GetBool(ConfigKeyAIFallbackEnabled).Return(true).Once()

	runtime, err := loadRuntimeConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, ProviderClaude, runtime.Provider)
	assert.True(t, runtime.Fallback.Enabled)
}

func TestLoadFallbackConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.Set(ConfigKeyAIFallbackProviders, []string{"openai", "gemini"})

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetBool(ConfigKeyAIFallbackEnabled).Return(true).Once()
	cfg.EXPECT().GetViper().Return(v).Once()

	fallback, err := loadFallbackConfig(cfg)
	require.NoError(t, err)

	assert.True(t, fallback.Enabled)
	assert.Equal(t, []Provider{ProviderOpenAI, ProviderGemini}, fallback.Providers)
}

func TestLoadCredentialConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider Provider
		envKey   string
		kcKey    string
		keyKey   string
	}{
		{
			name:     "openai",
			provider: ProviderOpenAI,
			envKey:   ConfigKeyOpenAIEnv,
			kcKey:    ConfigKeyOpenAIKeychain,
			keyKey:   ConfigKeyOpenAIKey,
		},
		{
			name:     "claude",
			provider: ProviderClaude,
			envKey:   ConfigKeyClaudeEnv,
			kcKey:    ConfigKeyClaudeKeychain,
			keyKey:   ConfigKeyClaudeKey,
		},
		{
			name:     "gemini",
			provider: ProviderGemini,
			envKey:   ConfigKeyGeminiEnv,
			kcKey:    ConfigKeyGeminiKeychain,
			keyKey:   ConfigKeyGeminiKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := configmocks.NewMockContainable(t)
			cfg.EXPECT().GetString(tt.envKey).Return("TOKEN_ENV").Once()
			cfg.EXPECT().GetString(tt.kcKey).Return("service/account").Once()
			cfg.EXPECT().GetString(tt.keyKey).Return("literal-key").Once()

			credentials, err := loadCredentialConfig(cfg, tt.provider)
			require.NoError(t, err)

			assert.Equal(t, CredentialConfig{
				Env:      "TOKEN_ENV",
				Keychain: "service/account",
				Key:      "literal-key",
			}, credentials)
		})
	}
}

func TestLoadCredentialConfig_UnknownProvider(t *testing.T) {
	t.Parallel()

	credentials, err := loadCredentialConfig(configmocks.NewMockContainable(t), Provider("unknown"))
	require.NoError(t, err)

	assert.True(t, credentials.IsZero())
}

func TestConfigAdapter_NilAndSkipBranches(t *testing.T) {
	t.Parallel()

	require.NoError(t, applyRuntimeConfig(nil, nil))
	require.NoError(t, applyCredentialConfig(nil, nil))

	runtime, err := loadRuntimeConfig(nil)
	require.NoError(t, err)
	assert.Zero(t, runtime)

	fallback, err := loadFallbackConfig(nil)
	require.NoError(t, err)
	assert.Zero(t, fallback)

	credentials, err := loadCredentialConfig(nil, ProviderOpenAI)
	require.NoError(t, err)
	assert.True(t, credentials.IsZero())

	cfg := Config{
		Provider:    ProviderOpenAI,
		Credentials: CredentialConfig{Key: "already-set"},
	}
	require.NoError(t, applyCredentialConfig(&props.Props{Config: configmocks.NewMockContainable(t)}, &cfg))
	assert.Equal(t, CredentialConfig{Key: "already-set"}, cfg.Credentials)

	local := Config{Provider: ProviderClaudeLocal}
	require.NoError(t, applyCredentialConfig(&props.Props{Config: configmocks.NewMockContainable(t)}, &local))
}

func TestClaudeLocal_SetToolsUnsupported(t *testing.T) {
	t.Parallel()

	err := (&ClaudeLocal{}).SetTools([]Tool{{Name: "tool"}})

	require.ErrorContains(t, err, "does not support SetTools")
}

func TestNewWithFallback_EnabledBuildsChainFromConfig(t *testing.T) {
	registerTestProviders(t)

	v := viper.New()
	v.Set(ConfigKeyAIFallbackEnabled, true)
	v.Set(ConfigKeyAIFallbackProviders, []string{"fbt-ok", "fbt-ok2"})

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: config.NewContainerFromViper(nil, v),
	}

	// cfg.Provider deliberately disagrees with providers[0], exercising the
	// override WARN; providers[0] (fbt-ok) is the effective primary.
	client, err := NewWithFallbackFromProps(context.Background(), p, Config{Provider: ProviderClaude})
	require.NoError(t, err)

	got, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

// TestNewWithFallback_WrapperBuildsChain covers the public NewWithFallback
// wrapper, which delegates to NewWithFallbackFromProps.
func TestNewWithFallback_WrapperBuildsChain(t *testing.T) {
	registerTestProviders(t)

	v := viper.New()
	v.Set(ConfigKeyAIFallbackEnabled, true)
	v.Set(ConfigKeyAIFallbackProviders, []string{"fbt-ok", "fbt-ok2"})

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: config.NewContainerFromViper(nil, v),
	}

	client, err := NewWithFallback(context.Background(), p, Config{Provider: ProviderClaude})
	require.NoError(t, err)

	got, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

func TestNewWithFallbackFromProps_NoSpuriousOverrideWarnWhenProviderUnset(t *testing.T) {
	registerTestProviders(t)

	v := viper.New()
	v.Set(ConfigKeyAIFallbackEnabled, true)
	v.Set(ConfigKeyAIFallbackProviders, []string{"fbt-ok"})

	buf := logger.NewBuffer()
	p := &props.Props{
		Logger: buf,
		Config: config.NewContainerFromViper(nil, v),
	}

	// No ai.provider is configured and the caller passes an empty Config, so the
	// provider is only defaulted internally (to claude). The override warning
	// must NOT fire — nothing the operator configured was overridden.
	client, err := NewWithFallbackFromProps(context.Background(), p, Config{})
	require.NoError(t, err)
	require.NotNil(t, client)

	assert.False(t, buf.Contains("overrides ai.provider"),
		"override warning must not fire when no provider was configured")
}

func TestNewWithFallbackFromProps_WarnsWhenConfiguredProviderOverridden(t *testing.T) {
	registerTestProviders(t)

	v := viper.New()
	v.Set(ConfigKeyAIProvider, "fbt-ok2")
	v.Set(ConfigKeyAIFallbackEnabled, true)
	v.Set(ConfigKeyAIFallbackProviders, []string{"fbt-ok"})

	buf := logger.NewBuffer()
	p := &props.Props{
		Logger: buf,
		Config: config.NewContainerFromViper(nil, v),
	}

	// ai.provider=fbt-ok2 is explicitly configured but fallback.providers[0]
	// (fbt-ok) becomes the effective primary, so the override warning MUST fire
	// even though the caller passed an empty Config (the provider is resolved
	// from ai.provider, not defaulted).
	client, err := NewWithFallbackFromProps(context.Background(), p, Config{})
	require.NoError(t, err)
	require.NotNil(t, client)

	require.True(t, buf.ContainsLevel(logger.WarnLevel, "overrides ai.provider"),
		"override warning must fire when a configured provider is overridden")
}

func TestNew_AppliesTypedConfigBeforeProviderFactory(t *testing.T) {
	t.Setenv("GTB_OPENAI_API_KEY", "env-literal-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
	RegisterProvider(ProviderOpenAI, func(_ context.Context, settings Settings) (ChatClient, error) {
		got = settings.Config

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

	client, err := NewFromProps(context.Background(), &props.Props{
		Logger: logger.NewNoop(),
		Config: c,
	}, Config{})

	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Equal(t, ProviderOpenAI, got.Provider)
	assert.Equal(t, 11*time.Second, got.RequestTimeout)
	assert.Equal(t, CredentialConfig{Key: "env-literal-key"}, got.Credentials)
}
