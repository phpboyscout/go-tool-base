package docs

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestAskAI_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"guide.md": {Data: []byte("# Guide\nThis is the guide.")},
	}

	l := logger.NewNoop()
	p := &props.Props{Logger: l}

	logFn := func(msg string, level logger.Level) {}

	// "nonexistent-provider-xyz" is not registered → chat.New returns error
	_, err := AskAI(context.Background(), p, fsys, "what is this?", logFn, nil, "nonexistent-provider-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider")
}

func TestAskAI_FSError(t *testing.T) {
	t.Parallel()

	// Use an empty FS but with a question — GetAllMarkdownContent succeeds on empty FS,
	// then chat.New fails with unsupported provider.
	fsys := fstest.MapFS{}
	l := logger.NewNoop()
	p := &props.Props{Logger: l}
	logCalls := 0
	logFn := func(msg string, level logger.Level) { logCalls++ }

	_, err := AskAI(context.Background(), p, fsys, "question", logFn, nil, "bad-provider")
	require.Error(t, err)
	assert.Positive(t, logCalls, "logFn should have been called")
}

func TestResolveProvider(t *testing.T) {
	t.Run("explicit override", func(t *testing.T) {
		p := &props.Props{}
		provider := ResolveProvider(p, "gemini")
		assert.Equal(t, chat.ProviderGemini, provider)
	})

	t.Run("config override", func(t *testing.T) {
		p := &props.Props{
			Config: config.NewReaderContainer(logger.NewNoop(), "yaml"),
		}
		t.Setenv("AI_PROVIDER", "claude")

		provider := ResolveProvider(p)
		assert.Equal(t, chat.ProviderClaude, provider)
	})

	t.Run("default is openai", func(t *testing.T) {
		p := &props.Props{
			Config: config.NewReaderContainer(logger.NewNoop(), "yaml"),
		}
		t.Setenv("AI_PROVIDER", "")

		provider := ResolveProvider(p)
		assert.Equal(t, chat.ProviderOpenAI, provider)
	})

	t.Run("no config defaults to openai", func(t *testing.T) {
		p := &props.Props{}
		provider := ResolveProvider(p)
		assert.Equal(t, chat.ProviderOpenAI, provider)
	})
}
