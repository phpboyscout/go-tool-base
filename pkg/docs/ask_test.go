package docs

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestAskAI_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"guide.md": {Data: []byte("# Guide\nThis is the guide.")},
	}

	l := logger.NewNoop()
	p := &props.Props{Logger: l}

	logFn := func(msg string, level logger.Level) {}

	// "nonexistent-provider-xyz" is not registered → gochat.New returns error
	_, err := AskAI(context.Background(), p, fsys, "what is this?", logFn, nil, "nonexistent-provider-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider")
}

func TestAskAI_FSError(t *testing.T) {
	t.Parallel()

	// Use an empty FS but with a question — GetAllMarkdownContent succeeds on empty FS,
	// then gochat.New fails with unsupported provider.
	fsys := fstest.MapFS{}
	l := logger.NewNoop()
	p := &props.Props{Logger: l}
	logCalls := 0
	logFn := func(msg string, level logger.Level) { logCalls++ }

	_, err := AskAI(context.Background(), p, fsys, "question", logFn, nil, "bad-provider")
	require.Error(t, err)
	assert.Positive(t, logCalls, "logFn should have been called")
}

// TestAskAI_NilCallbacksDoNotPanic exercises the documented "either
// callback may be nil" contract: AskAI must guard nil logFn/deltaFn rather
// than dereferencing them on the first status message. The unsupported
// provider makes the call fail fast, but only after the nil-guarded logFn
// has already been invoked — so a missing guard would panic before the
// error is returned.
func TestAskAI_NilCallbacksDoNotPanic(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"guide.md": {Data: []byte("# Guide\n")},
	}

	p := &props.Props{Logger: logger.NewNoop()}

	assert.NotPanics(t, func() {
		_, err := AskAI(context.Background(), p, fsys, "what is this?", nil, nil, "bad-provider")
		require.Error(t, err)
	}, "nil logFn/deltaFn must not panic")
}

func TestResolveProvider(t *testing.T) {
	t.Run("explicit override", func(t *testing.T) {
		p := &props.Props{}
		provider := ResolveProvider(p, "gemini")
		assert.Equal(t, gochat.ProviderGemini, provider)
	})

	t.Run("config override", func(t *testing.T) {
		p := &props.Props{
			Config: config.NewReaderContainer(afero.NewOsFs(), config.WithConfigFormat("yaml")),
		}
		t.Setenv("AI_PROVIDER", "claude")

		provider := ResolveProvider(p)
		assert.Equal(t, gochat.ProviderClaude, provider)
	})

	t.Run("default is openai", func(t *testing.T) {
		p := &props.Props{
			Config: config.NewReaderContainer(afero.NewOsFs(), config.WithConfigFormat("yaml")),
		}
		t.Setenv("AI_PROVIDER", "")

		provider := ResolveProvider(p)
		assert.Equal(t, gochat.ProviderOpenAI, provider)
	})

	t.Run("no config defaults to openai", func(t *testing.T) {
		p := &props.Props{}
		provider := ResolveProvider(p)
		assert.Equal(t, gochat.ProviderOpenAI, provider)
	})
}
