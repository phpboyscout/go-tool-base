package chat

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"
)

func TestApplyDefaultProvider(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	t.Run("keeps an explicit provider", func(t *testing.T) {
		cfg := gochat.Config{Provider: gochat.ProviderGemini}
		require.NoError(t, applyDefaultProvider(log, &cfg, nil))
		assert.Equal(t, gochat.ProviderGemini, cfg.Provider)
	})

	t.Run("uses AI_PROVIDER when unset", func(t *testing.T) {
		t.Setenv(EnvAIProvider, "openai")
		cfg := gochat.Config{}
		require.NoError(t, applyDefaultProvider(log, &cfg, nil))
		assert.Equal(t, gochat.ProviderOpenAI, cfg.Provider)
	})

	// Spec 0196 D5: the framework names no vendor. Nothing configured is an
	// error that says what to set, not a silent Claude.
	t.Run("nothing set is an error naming ai.provider", func(t *testing.T) {
		t.Setenv(EnvAIProvider, "")
		cfg := gochat.Config{}
		err := applyDefaultProvider(log, &cfg, nil)
		require.ErrorIs(t, err, ErrProviderUnset)
		assert.Contains(t, errors.FlattenHints(err), ConfigKeyAIProvider)
		assert.Contains(t, errors.FlattenHints(err), "init ai")
		assert.Empty(t, cfg.Provider)
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
