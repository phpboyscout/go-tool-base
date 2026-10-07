package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// resolveModel fills a provider's default when nothing names a model, except
// where the provider has none to give: an OpenAI-compatible backend serves
// whatever models it hosts, and go/chat-openai refuses it without one rather
// than send it OpenAI's default (openai.go, "Model is required").
func TestResolveModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider gochat.Provider
		model    string
		want     string
	}{
		{name: "openai takes its default", provider: gochat.ProviderOpenAI, want: gochat.DefaultModelOpenAI},
		{name: "claude takes its default", provider: gochat.ProviderClaude, want: gochat.DefaultModelClaude},
		{name: "gemini takes its default", provider: gochat.ProviderGemini, want: gochat.DefaultModelGemini},
		{name: "an openai-compatible backend gets none", provider: gochat.ProviderOpenAICompatible, want: ""},
		{name: "claude-local leaves it to the CLI", provider: gochat.ProviderClaudeLocal, want: ""},
		{name: "a named model wins", provider: gochat.ProviderOpenAICompatible, model: "llama3.2", want: "llama3.2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := New(&props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}, &Config{AIModel: tc.model})
			assert.Equal(t, tc.want, g.resolveModel(tc.provider))
		})
	}
}
