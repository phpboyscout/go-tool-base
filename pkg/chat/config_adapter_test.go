package chat

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go/config"
	configmocks "gitlab.com/phpboyscout/go/config/mocks"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// chatStoreFromYAML builds a store over the given YAML document, with any
// further options (e.g. config.WithEnv) layered above it.
func chatStoreFromYAML(t *testing.T, yaml string, opts ...config.StoreOption) *config.Store {
	t.Helper()

	store, err := config.NewStore(t.Context(), append([]config.StoreOption{
		config.WithReaders(config.NamedSource{Name: "test", Content: []byte(yaml)}),
	}, opts...)...)
	require.NoError(t, err)

	return store
}

func TestConfigAdapter_LoadsTypedSections(t *testing.T) {
	t.Setenv("GTB_OPENAI_API_KEY", "env-literal-key")

	view := chatStoreFromYAML(t, `
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
`, config.WithEnv("GTB")).View()

	runtime, err := loadRuntimeConfig(view)
	require.NoError(t, err)

	assert.Equal(t, gochat.ProviderOpenAI, runtime.Provider)
	assert.Equal(t, 9*time.Second, runtime.RequestTimeout)
	assert.True(t, runtime.Fallback.Enabled)
	assert.Equal(t, []gochat.Provider{gochat.ProviderOpenAI, gochat.ProviderClaude}, runtime.Fallback.Providers)

	credentials, err := loadCredentialConfig(view, gochat.ProviderOpenAI)
	require.NoError(t, err)

	assert.Equal(t, "CUSTOM_OPENAI_TOKEN", credentials.Env)
	assert.Equal(t, "service/account", credentials.Keychain)
	assert.Equal(t, "env-literal-key", credentials.Key)
}

func TestLoadFallbackConfig_TypedSection(t *testing.T) {
	t.Parallel()

	view := chatStoreFromYAML(t, `
ai:
  fallback:
    enabled: true
    providers: [openai, gemini]
`).View()

	fallback, err := loadFallbackConfig(view)
	require.NoError(t, err)

	assert.True(t, fallback.Enabled)
	assert.Equal(t, []gochat.Provider{gochat.ProviderOpenAI, gochat.ProviderGemini}, fallback.Providers)
}

// TestApplyRuntimeConfig_MockReader pins the property the deleted legacy
// branch existed for: the adapter stays drivable by the published mocks.
func TestApplyRuntimeConfig_MockReader(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockReader(t)
	cfg.EXPECT().SectionExists("ai").Return(true).Once()
	cfg.EXPECT().UnmarshalKey("ai", mock.Anything).RunAndReturn(func(_ string, target any) error {
		runtime, ok := target.(*gochat.RuntimeConfig)
		require.True(t, ok)

		runtime.Provider = gochat.ProviderGemini

		return nil
	}).Once()

	chatConfig := gochat.Config{}
	require.NoError(t, applyRuntimeConfig(cfg, &chatConfig))

	assert.Equal(t, gochat.ProviderGemini, chatConfig.Provider)
}

func TestLoadCredentialConfig_TypedSections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider gochat.Provider
		yaml     string
	}{
		{
			name:     "openai",
			provider: gochat.ProviderOpenAI,
			yaml:     "openai:\n  api:\n    env: TOKEN_ENV\n    keychain: service/account\n    key: literal-key\n",
		},
		{
			name:     "claude",
			provider: gochat.ProviderClaude,
			yaml:     "anthropic:\n  api:\n    env: TOKEN_ENV\n    keychain: service/account\n    key: literal-key\n",
		},
		{
			name:     "gemini",
			provider: gochat.ProviderGemini,
			yaml:     "gemini:\n  api:\n    env: TOKEN_ENV\n    keychain: service/account\n    key: literal-key\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			credentials, err := loadCredentialConfig(chatStoreFromYAML(t, tt.yaml).View(), tt.provider)
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

	credentials, err := loadCredentialConfig(configmocks.NewMockReader(t), gochat.Provider("unknown"))
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

	// Credentials already supplied: the reader must not be consulted at all.
	cfg := gochat.Config{
		Provider:    gochat.ProviderOpenAI,
		Credentials: gochat.CredentialConfig{Key: "already-set"},
	}
	require.NoError(t, applyCredentialConfig(configmocks.NewMockReader(t), &cfg))
	assert.Equal(t, gochat.CredentialConfig{Key: "already-set"}, cfg.Credentials)

	// claude-local resolves credentials via the CLI binary, never from config.
	local := gochat.Config{Provider: gochat.ProviderClaudeLocal}
	require.NoError(t, applyCredentialConfig(configmocks.NewMockReader(t), &local))
}

func TestNewWithFallback_EnabledBuildsChainFromConfig(t *testing.T) {
	registerTestProviders(t)

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: chatStoreFromYAML(t, "ai:\n  fallback:\n    enabled: true\n    providers: [fbt-ok, fbt-ok2]\n"),
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

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: chatStoreFromYAML(t, "ai:\n  fallback:\n    enabled: true\n    providers: [fbt-ok, fbt-ok2]\n"),
	}

	client, err := NewWithFallback(context.Background(), p, gochat.Config{Provider: gochat.ProviderClaude})
	require.NoError(t, err)

	got, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

func TestNewWithFallbackFromProps_NoSpuriousOverrideWarnWhenProviderUnset(t *testing.T) {
	registerTestProviders(t)

	buf := logger.NewBuffer()
	p := &props.Props{
		Logger: buf,
		Config: chatStoreFromYAML(t, "ai:\n  fallback:\n    enabled: true\n    providers: [fbt-ok]\n"),
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

	buf := logger.NewBuffer()
	p := &props.Props{
		Logger: buf,
		Config: chatStoreFromYAML(t, "ai:\n  provider: fbt-ok2\n  fallback:\n    enabled: true\n    providers: [fbt-ok]\n"),
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

	store := chatStoreFromYAML(t, `
ai:
  provider: openai
  request_timeout: 11s
openai:
  api:
    key: file-literal-key
`, config.WithEnv("GTB"))

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
		Config: store,
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
