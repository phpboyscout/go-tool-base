package chat

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	gochat "gitlab.com/phpboyscout/go/chat"
)

func TestApplyDefaultProvider(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	t.Run("keeps an explicit provider", func(t *testing.T) {
		cfg := gochat.Config{Provider: gochat.ProviderGemini}
		applyDefaultProvider(log, &cfg)
		assert.Equal(t, gochat.ProviderGemini, cfg.Provider)
	})

	t.Run("uses AI_PROVIDER when unset", func(t *testing.T) {
		t.Setenv(EnvAIProvider, "openai")
		cfg := gochat.Config{}
		applyDefaultProvider(log, &cfg)
		assert.Equal(t, gochat.ProviderOpenAI, cfg.Provider)
	})

	t.Run("defaults to claude when nothing set", func(t *testing.T) {
		t.Setenv(EnvAIProvider, "")
		cfg := gochat.Config{}
		applyDefaultProvider(log, &cfg)
		assert.Equal(t, gochat.ProviderClaude, cfg.Provider)
	})
}

func TestResolveChatTimeout(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 8*time.Minute, resolveChatTimeout(gochat.Config{RequestTimeout: 8 * time.Minute}))
	assert.Equal(t, gochat.DefaultChatRequestTimeout, resolveChatTimeout(gochat.Config{}))
}

func TestGenerateSchema_Wrapper(t *testing.T) {
	t.Parallel()

	// The generic wrapper forwards to the module; a non-nil schema is enough to
	// confirm the forwarding compiles and runs.
	schema := gochat.GenerateSchema[struct {
		Path string `json:"path"`
	}]()
	assert.NotNil(t, schema)
}
