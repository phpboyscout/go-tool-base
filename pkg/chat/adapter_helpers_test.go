package chat

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplyDefaultProvider(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	t.Run("keeps an explicit provider", func(t *testing.T) {
		cfg := Config{Provider: ProviderGemini}
		applyDefaultProvider(log, &cfg)
		assert.Equal(t, ProviderGemini, cfg.Provider)
	})

	t.Run("uses AI_PROVIDER when unset", func(t *testing.T) {
		t.Setenv(EnvAIProvider, "openai")
		cfg := Config{}
		applyDefaultProvider(log, &cfg)
		assert.Equal(t, ProviderOpenAI, cfg.Provider)
	})

	t.Run("defaults to claude when nothing set", func(t *testing.T) {
		t.Setenv(EnvAIProvider, "")
		cfg := Config{}
		applyDefaultProvider(log, &cfg)
		assert.Equal(t, ProviderClaude, cfg.Provider)
	})
}

func TestWarnFallbackPrimaryOverride(t *testing.T) {
	newLog := func(buf *bytes.Buffer) *slog.Logger {
		return slog.New(slog.NewTextHandler(buf, nil))
	}

	t.Run("warns when a configured provider is overridden", func(t *testing.T) {
		var buf bytes.Buffer
		warnFallbackPrimaryOverride(newLog(&buf), Config{Provider: ProviderGemini}, ProviderClaude)
		assert.Contains(t, buf.String(), "overrides ai.provider")
	})

	t.Run("silent when provider unset", func(t *testing.T) {
		var buf bytes.Buffer
		warnFallbackPrimaryOverride(newLog(&buf), Config{}, ProviderClaude)
		assert.Empty(t, buf.String())
	})

	t.Run("silent when provider equals primary", func(t *testing.T) {
		var buf bytes.Buffer
		warnFallbackPrimaryOverride(newLog(&buf), Config{Provider: ProviderClaude}, ProviderClaude)
		assert.Empty(t, buf.String())
	})
}

func TestFallbackProviderConfigs(t *testing.T) {
	t.Parallel()

	base := Config{
		Provider:    ProviderClaude,
		Token:       "secret",
		Model:       "some-model",
		BaseURL:     "https://example.test",
		Credentials: CredentialConfig{Key: "k"},
		MaxSteps:    7, // a non-provider-specific field that must be inherited
	}

	got := fallbackProviderConfigs(base, []Provider{ProviderOpenAI, ProviderGemini})

	assert.Len(t, got, 2)
	assert.Equal(t, ProviderOpenAI, got[0].Provider)
	assert.Equal(t, ProviderGemini, got[1].Provider)

	for _, c := range got {
		assert.Empty(t, c.Token, "per-provider config clears the token")
		assert.Empty(t, c.Model, "per-provider config clears the model")
		assert.Empty(t, c.BaseURL, "per-provider config clears the base URL")
		assert.True(t, c.Credentials.IsZero(), "per-provider config clears credentials")
		assert.Equal(t, 7, c.MaxSteps, "non-provider-specific fields are inherited")
	}
}

func TestResolveChatTimeout(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 8*time.Minute, resolveChatTimeout(Config{RequestTimeout: 8 * time.Minute}))
	assert.Equal(t, DefaultChatRequestTimeout, resolveChatTimeout(Config{}))
}

func TestGenerateSchema_Wrapper(t *testing.T) {
	t.Parallel()

	// The generic wrapper forwards to the module; a non-nil schema is enough to
	// confirm the forwarding compiles and runs.
	schema := GenerateSchema[struct {
		Path string `json:"path"`
	}]()
	assert.NotNil(t, schema)
}
