package chat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/exectest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

// TestUsage_Add verifies element-wise accumulation and Known propagation.
func TestUsage_Add(t *testing.T) {
	t.Parallel()

	a := chat.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CachedTokens: 2, ReasoningTokens: 1, Known: true}
	b := chat.Usage{InputTokens: 4, OutputTokens: 6, TotalTokens: 10, CachedTokens: 1, ReasoningTokens: 3}

	got := a.Add(b)

	assert.Equal(t, 14, got.InputTokens)
	assert.Equal(t, 11, got.OutputTokens)
	assert.Equal(t, 25, got.TotalTokens)
	assert.Equal(t, 3, got.CachedTokens)
	assert.Equal(t, 4, got.ReasoningTokens)
	assert.True(t, got.Known, "Known should propagate when either operand is Known")

	zero := chat.Usage{}
	assert.False(t, zero.Add(chat.Usage{}).Known)
}

func newClaudeUsageClient(t *testing.T, server *MockServer, cfg chat.Config) chat.ChatClient {
	t.Helper()

	cfg.Provider = chat.ProviderClaude
	cfg.Token = "test-key"
	cfg.BaseURL = server.URL + "/"
	cfg.AllowInsecureBaseURL = true

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	return client
}

// TestUsage_ClaudeChat maps Anthropic usage and reports it via Usage().
func TestUsage_ClaudeChat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	server.RespondWithJSON(http.StatusOK, map[string]interface{}{
		"id":          "msg_1",
		"type":        "message",
		"role":        "assistant",
		"model":       "claude-opus-4-8",
		"content":     []map[string]interface{}{{"type": "text", "text": "hello"}},
		"stop_reason": "end_turn",
		"usage": map[string]interface{}{
			"input_tokens":            120,
			"output_tokens":           34,
			"cache_read_input_tokens": 8,
		},
	})

	client := newClaudeUsageClient(t, server, chat.Config{})

	out, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "hello", out)

	u := client.Usage()
	assert.True(t, u.Known)
	assert.Equal(t, 120, u.InputTokens)
	assert.Equal(t, 34, u.OutputTokens)
	assert.Equal(t, 154, u.TotalTokens, "total computed when provider omits it")
	assert.Equal(t, 8, u.CachedTokens)
}

// TestUsage_ClaudeReActSummed verifies usage is summed across a tool-loop and the
// observer fires once per round-trip.
func TestUsage_ClaudeReActSummed(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	var calls int

	var mu sync.Mutex

	server.Handler = func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		var resp map[string]interface{}
		if first {
			resp = map[string]interface{}{
				"id": "msg_tool", "type": "message", "role": "assistant", "model": "claude-opus-4-8",
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "toolu_1", "name": "ping", "input": map[string]interface{}{}},
				},
				"stop_reason": "tool_use",
				"usage":       map[string]interface{}{"input_tokens": 100, "output_tokens": 10},
			}
		} else {
			resp = map[string]interface{}{
				"id": "msg_final", "type": "message", "role": "assistant", "model": "claude-opus-4-8",
				"content":     []map[string]interface{}{{"type": "text", "text": "done"}},
				"stop_reason": "end_turn",
				"usage":       map[string]interface{}{"input_tokens": 200, "output_tokens": 20},
			}
		}

		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			http.Error(w, encErr.Error(), http.StatusInternalServerError)
		}
	}

	var observed []chat.Usage

	var obsMu sync.Mutex

	cfg := chat.Config{UsageObserver: func(u chat.Usage) {
		obsMu.Lock()
		observed = append(observed, u)
		obsMu.Unlock()
	}}

	client := newClaudeUsageClient(t, server, cfg)

	require.NoError(t, client.SetTools([]chat.Tool{{
		Name:        "ping",
		Description: "ping",
		Parameters:  nil,
		Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			return "pong", nil
		},
	}}))

	out, err := client.Chat(context.Background(), "go")
	require.NoError(t, err)
	assert.Equal(t, "done", out)

	u := client.Usage()
	assert.Equal(t, 300, u.InputTokens, "summed across both round-trips")
	assert.Equal(t, 30, u.OutputTokens)
	assert.Equal(t, 330, u.TotalTokens)

	obsMu.Lock()
	defer obsMu.Unlock()
	require.Len(t, observed, 2, "observer fires once per round-trip")
	assert.Equal(t, 100, observed[0].InputTokens)
	assert.Equal(t, 200, observed[1].InputTokens)
}

// TestUsage_ClaudeStream captures usage from a streamed message_delta event.
func TestUsage_ClaudeStream(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	client := newClaudeUsageClient(t, server, chat.Config{})

	streamer, ok := client.(chat.StreamingChatClient)
	require.True(t, ok)

	server.Handler = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			sseEvent(map[string]any{"type": "message_start", "message": map[string]any{"id": "m1", "type": "message", "role": "assistant", "content": []any{}, "model": "claude-opus-4-8"}}),
			sseEvent(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "hi"}}),
			sseEvent(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"input_tokens": 42, "output_tokens": 9}}),
			sseEvent(map[string]any{"type": "message_stop"}),
		}
		for _, e := range events {
			_, _ = w.Write([]byte(e))
			flusher.Flush()
		}
	}

	_, err := streamer.StreamChat(context.Background(), "hi", func(_ chat.StreamEvent) error { return nil })
	require.NoError(t, err)

	u := client.Usage()
	assert.True(t, u.Known)
	assert.Equal(t, 42, u.InputTokens)
	assert.Equal(t, 9, u.OutputTokens)
	assert.Equal(t, 51, u.TotalTokens)
}

// TestUsage_OpenAIChat maps OpenAI usage (prompt/completion/total) and details.
func TestUsage_OpenAIChat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	server.RespondWithJSON(http.StatusOK, map[string]interface{}{
		"id":      "chatcmpl-1",
		"object":  "chat.completion",
		"model":   "gpt-test",
		"choices": []map[string]interface{}{{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hi there"}, "finish_reason": "stop"}},
		"usage": map[string]interface{}{
			"prompt_tokens":             50,
			"completion_tokens":         12,
			"total_tokens":              62,
			"completion_tokens_details": map[string]interface{}{"reasoning_tokens": 4},
			"prompt_tokens_details":     map[string]interface{}{"cached_tokens": 9},
		},
	})

	client, err := newTestClient(context.Background(), chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		Model:                "gpt-test",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	out, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "hi there", out)

	u := client.Usage()
	assert.True(t, u.Known)
	assert.Equal(t, 50, u.InputTokens)
	assert.Equal(t, 12, u.OutputTokens)
	assert.Equal(t, 62, u.TotalTokens, "provider-supplied total preserved")
	assert.Equal(t, 9, u.CachedTokens)
	assert.Equal(t, 4, u.ReasoningTokens)
}

// TestUsage_GeminiChat maps Gemini UsageMetadata token counts.
func TestUsage_GeminiChat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	server.RespondWithJSON(http.StatusOK, map[string]interface{}{
		"candidates": []map[string]interface{}{{
			"content": map[string]interface{}{"role": "model", "parts": []map[string]interface{}{{"text": "gem"}}},
		}},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":        70,
			"candidatesTokenCount":    15,
			"totalTokenCount":         85,
			"cachedContentTokenCount": 5,
			"thoughtsTokenCount":      3,
		},
	})

	client, err := newTestClient(context.Background(), chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	_, err = client.Chat(context.Background(), "hi")
	require.NoError(t, err)

	u := client.Usage()
	assert.True(t, u.Known)
	assert.Equal(t, 70, u.InputTokens)
	assert.Equal(t, 15, u.OutputTokens)
	assert.Equal(t, 85, u.TotalTokens)
	assert.Equal(t, 5, u.CachedTokens)
	assert.Equal(t, 3, u.ReasoningTokens)
}

// TestUsage_ClaudeLocalUnknown verifies the local CLI reports an unknown usage
// without error when the binary omits a usage block.
func TestUsage_ClaudeLocalUnknown(t *testing.T) {
	t.Parallel()

	client, err := newTestClient(context.Background(), chat.Config{
		Provider:     chat.ProviderClaudeLocal,
		ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
		ExecCommand:  exectest.EchoCommand(`{"type":"message","result":"ok","session_id":"s1","is_error":false}`),
	})
	require.NoError(t, err)

	out, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", out)

	u := client.Usage()
	assert.False(t, u.Known, "claude-local reports unknown usage when CLI omits it")
	assert.Zero(t, u.TotalTokens)
}

// TestUsage_ClaudeLocalReported verifies the local CLI usage block, when present,
// is surfaced.
func TestUsage_ClaudeLocalReported(t *testing.T) {
	t.Parallel()

	var observed chat.Usage

	client, err := newTestClient(context.Background(), chat.Config{
		Provider:     chat.ProviderClaudeLocal,
		ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
		ExecCommand: exectest.EchoCommand(
			`{"type":"message","result":"ok","session_id":"s1","is_error":false,` +
				`"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":2}}`),
		UsageObserver: func(u chat.Usage) { observed = u },
	})
	require.NoError(t, err)

	_, err = client.Chat(context.Background(), "hi")
	require.NoError(t, err)

	u := client.Usage()
	assert.True(t, u.Known)
	assert.Equal(t, 11, u.InputTokens)
	assert.Equal(t, 7, u.OutputTokens)
	assert.Equal(t, 18, u.TotalTokens)
	assert.Equal(t, 2, u.CachedTokens)
	assert.Equal(t, 11, observed.InputTokens, "observer received the round-trip usage")
}

// TestUsage_InitialZero verifies a fresh client reports a zero, unknown usage.
func TestUsage_InitialZero(t *testing.T) {
	t.Parallel()

	client, err := newTestClient(context.Background(), chat.Config{
		Provider: chat.ProviderClaude,
		Token:    "test-key",
	})
	require.NoError(t, err)

	u := client.Usage()
	assert.False(t, u.Known)
	assert.Zero(t, u.TotalTokens)
}
