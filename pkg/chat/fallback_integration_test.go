package chat_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestFallback_RealHTTPFailover proves end-to-end that a real provider SDK error
// (an OpenAI 503) is classified as retryable and the composite advances to a
// second endpoint that completes — the one path unit tests with fakes cannot
// cover (that the live SDK surfaces a classifiable *openai.Error). Gated because
// the OpenAI SDK retries 5xx internally with backoff.
func TestFallback_RealHTTPFailover(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")

	primary := NewMockServer()
	defer primary.Close()
	primary.RespondWithJSON(http.StatusServiceUnavailable,
		map[string]any{"error": map[string]any{"message": "overloaded", "type": "server_error"}})

	fallback := NewMockServer()
	defer fallback.Close()
	fallback.RespondWithJSON(http.StatusOK, map[string]any{
		"id": "chatcmpl-fallback",
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": "from-fallback"}, "finish_reason": "stop"},
		},
	})

	buf := logger.NewBuffer()
	p := &props.Props{Logger: buf}

	client, err := chat.NewFallbackFromConfigs(context.Background(), p, []chat.Config{
		{Provider: chat.ProviderOpenAI, Token: "test", BaseURL: primary.URL + "/", AllowInsecureBaseURL: true},
		{Provider: chat.ProviderOpenAI, Token: "test", BaseURL: fallback.URL + "/", AllowInsecureBaseURL: true},
	})
	require.NoError(t, err)

	reply, err := client.Chat(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "from-fallback", reply, "caller receives the fallback endpoint's completion")
	assert.True(t, buf.Contains("chat provider failover"), "exactly one failover WARN is logged")
}
