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

	gochat "gitlab.com/phpboyscout/go/chat"

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

	assert.Equal(t, gochat.ProviderOpenAI, runtime.Provider)
	assert.Equal(t, 9*time.Second, runtime.RequestTimeout)
	assert.True(t, runtime.Fallback.Enabled)
	assert.Equal(t, []gochat.Provider{gochat.ProviderOpenAI, gochat.ProviderClaude}, runtime.Fallback.Providers)

	credentials, err := loadCredentialConfig(c, gochat.ProviderOpenAI)
	require.NoError(t, err)

	assert.Equal(t, "CUSTOM_OPENAI_TOKEN", credentials.Env)
	assert.Equal(t, "service/account", credentials.Keychain)
	assert.Equal(t, "env-literal-key", credentials.Key)
}

func TestApplyRuntimeConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetString(ConfigKeyAIProvider).Return(string(gochat.ProviderGemini)).Once()

	chatConfig := gochat.Config{}
	err := applyRuntimeConfig(&props.Props{Config: cfg}, &chatConfig)
	require.NoError(t, err)

	assert.Equal(t, gochat.ProviderGemini, chatConfig.Provider)
}

func TestLoadRuntimeConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetString(ConfigKeyAIProvider).Return(string(gochat.ProviderClaude)).Once()
	cfg.EXPECT().GetBool(ConfigKeyAIFallbackEnabled).Return(true).Once()

	runtime, err := loadRuntimeConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, gochat.ProviderClaude, runtime.Provider)
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
	assert.Equal(t, []gochat.Provider{gochat.ProviderOpenAI, gochat.ProviderGemini}, fallback.Providers)
}

func TestLoadCredentialConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider gochat.Provider
		envKey   string
		kcKey    string
		keyKey   string
	}{
		{
			name:     "openai",
			provider: gochat.ProviderOpenAI,
			envKey:   ConfigKeyOpenAIEnv,
			kcKey:    ConfigKeyOpenAIKeychain,
			keyKey:   ConfigKeyOpenAIKey,
		},
		{
			name:     "claude",
			provider: gochat.ProviderClaude,
			envKey:   ConfigKeyClaudeEnv,
			kcKey:    ConfigKeyClaudeKeychain,
			keyKey:   ConfigKeyClaudeKey,
		},
		{
			name:     "gemini",
			provider: gochat.ProviderGemini,
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

			assert.Equal(t, gochat.CredentialConfig{
				Env:      "TOKEN_ENV",
				Keychain: "service/account",
				Key:      "literal-key",
			}, credentials)
		})
	}
}

func TestLoadCredentialConfig_UnknownProvider(t *testing.T) {
	t.Parallel()

	credentials, err := loadCredentialConfig(configmocks.NewMockContainable(t), gochat.Provider("unknown"))
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

	credentials, err := loadCredentialConfig(nil, gochat.ProviderOpenAI)
	require.NoError(t, err)
	assert.True(t, credentials.IsZero())

	cfg := gochat.Config{
		Provider:    gochat.ProviderOpenAI,
		Credentials: gochat.CredentialConfig{Key: "already-set"},
	}
	require.NoError(t, applyCredentialConfig(&props.Props{Config: configmocks.NewMockContainable(t)}, &cfg))
	assert.Equal(t, gochat.CredentialConfig{Key: "already-set"}, cfg.Credentials)

	local := gochat.Config{Provider: gochat.ProviderClaudeLocal}
	require.NoError(t, applyCredentialConfig(&props.Props{Config: configmocks.NewMockContainable(t)}, &local))
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
	client, err := NewWithFallbackFromProps(context.Background(), p, gochat.Config{Provider: gochat.ProviderClaude})
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

	client, err := NewWithFallback(context.Background(), p, gochat.Config{Provider: gochat.ProviderClaude})
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
	client, err := NewWithFallbackFromProps(context.Background(), p, gochat.Config{})
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
	client, err := NewWithFallbackFromProps(context.Background(), p, gochat.Config{})
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

	// Overwrite the real (blank-imported) chat-openai provider with a capturing
	// fake for this test. The module's registry exposes no removal, so there is
	// no restore — safe here because no other pkg/chat test constructs a real
	// OpenAI client.
	var got gochat.Config
	gochat.RegisterProvider(gochat.ProviderOpenAI, func(_ context.Context, settings gochat.Settings) (gochat.ChatClient, error) {
		got = settings.Config

		return &fakeClient{chatReply: "ok"}, nil
	})

	client, err := NewFromProps(context.Background(), &props.Props{
		Logger: logger.NewNoop(),
		Config: c,
	}, gochat.Config{})

	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Equal(t, gochat.ProviderOpenAI, got.Provider)
	assert.Equal(t, 11*time.Second, got.RequestTimeout)
	assert.Equal(t, "env-literal-key", got.Credentials.Key)
	assert.Empty(t, got.Credentials.Env)
	assert.Empty(t, got.Credentials.Keychain)
	// The GTB adapter wires the keychain resolver and hardened HTTP client.
	assert.NotNil(t, got.Credentials.Lookup)
	assert.NotNil(t, got.HTTPClient)
}
