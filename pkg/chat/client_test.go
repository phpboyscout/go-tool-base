package chat

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestNew(t *testing.T) {
	// Clear environment variables to ensure predictable test results
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	ctx := context.Background()
	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: config.NewReaderContainer(logger.NewNoop(), "yaml"),
	}

	t.Run("default provider is OpenAI", func(t *testing.T) {
		t.Setenv("AI_PROVIDER", "")
		cfg := Config{}
		client, err := New(ctx, p, cfg)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "OpenAI token is required")
		}
		assert.Nil(t, client)
	})

	t.Run("Claude provider", func(t *testing.T) {
		cfg := Config{Provider: ProviderClaude}
		client, err := New(ctx, p, cfg)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "Anthropic API key is required")
		}
		assert.Nil(t, client)
	})

	t.Run("Gemini provider", func(t *testing.T) {
		cfg := Config{Provider: ProviderGemini}
		client, err := New(ctx, p, cfg)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "Gemini API key is required")
		}
		assert.Nil(t, client)
	})

	t.Run("ProviderOpenAICompatible requires model", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderOpenAICompatible,
			Token:    "test-token",
			BaseURL:  "http://localhost:11434/v1",
		}
		client, err := New(ctx, p, cfg)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "Model is required for ProviderOpenAICompatible")
		}
		assert.Nil(t, client)
	})

	t.Run("ProviderClaudeLocal binary not found", func(t *testing.T) {
		// This test relies on "claude" not being in PATH in CI; if it is present, skip.
		if _, err := findClaude(); err == nil {
			t.Skip("claude binary found in PATH; skipping not-found test")
		}
		cfg := Config{Provider: ProviderClaudeLocal}
		client, err := New(ctx, p, cfg)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "claude binary not found")
		}
		assert.Nil(t, client)
	})

	t.Run("unsupported provider", func(t *testing.T) {
		cfg := Config{Provider: "invalid"}
		client, err := New(ctx, p, cfg)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "unsupported provider")
		}
		assert.Nil(t, client)
	})

	t.Run("registry dispatch", func(t *testing.T) {
		called := false
		mockFactory := func(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
			called = true
			return nil, nil
		}

		RegisterProvider("mock-provider", mockFactory)
		t.Cleanup(func() {
			registryMu.Lock()
			delete(providerRegistry, "mock-provider")
			registryMu.Unlock()
		})

		cfg := Config{Provider: "mock-provider"}
		_, err := New(ctx, p, cfg)
		require.NoError(t, err)
		assert.True(t, called, "expected mock factory to be called")
	})
}

// findClaude is a helper used by tests to check if the claude binary is available.
func findClaude() (string, error) {
	return exec.LookPath("claude")
}
