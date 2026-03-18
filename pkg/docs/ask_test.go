package docs

import (
	"io"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/phpboyscout/gtb/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/stretchr/testify/assert"
)

func TestResolveProvider(t *testing.T) {
	t.Run("explicit override", func(t *testing.T) {
		p := &props.Props{}
		provider := ResolveProvider(p, "gemini")
		assert.Equal(t, chat.ProviderGemini, provider)
	})

	t.Run("config override", func(t *testing.T) {
		p := &props.Props{
			Config: config.NewReaderContainer(log.New(io.Discard), "yaml"),
		}
		t.Setenv("AI_PROVIDER", "claude")

		provider := ResolveProvider(p)
		assert.Equal(t, chat.ProviderClaude, provider)
	})

	t.Run("default is openai", func(t *testing.T) {
		p := &props.Props{
			Config: config.NewReaderContainer(log.New(io.Discard), "yaml"),
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
