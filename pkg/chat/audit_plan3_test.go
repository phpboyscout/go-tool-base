package chat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockConfig "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type staleArgs struct {
	X string `json:"x"`
}

// writeJSON encodes payload to w, failing the test on a marshal error.
// Wrapping the Encode keeps errchkjson happy about the any-typed payloads
// these provider-stub handlers build.
func writeJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		panic(err)
	}
}

// claudeAuditClient builds a Claude client pointed at server.
func claudeAuditClient(t *testing.T, url string, opts ...func(*chat.Config)) chat.ChatClient {
	t.Helper()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

	cfg := chat.Config{
		Provider:             chat.ProviderClaude,
		Token:                "test-key",
		BaseURL:              url + "/",
		AllowInsecureBaseURL: true,
	}
	for _, o := range opts {
		o(&cfg)
	}

	p := &props.Props{Logger: logger.NewNoop(), Config: cfgMock}

	client, err := chat.New(context.Background(), p, cfg)
	require.NoError(t, err)

	return client
}

func openaiAuditClient(t *testing.T, url string, opts ...func(*chat.Config)) chat.ChatClient {
	t.Helper()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

	cfg := chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              url + "/",
		AllowInsecureBaseURL: true,
	}
	for _, o := range opts {
		o(&cfg)
	}

	p := &props.Props{Logger: logger.NewNoop(), Config: cfgMock}

	client, err := chat.New(context.Background(), p, cfg)
	require.NoError(t, err)

	return client
}

func toolWithHandler(name string, called *bool) chat.Tool {
	return chat.Tool{
		Name:        name,
		Description: name,
		Parameters:  chat.GenerateSchema[staleArgs]().(*jsonschema.Schema),
		Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			*called = true

			return "ok", nil
		},
	}
}

// TestSetTools_ReplacesStaleHandlers asserts a second SetTools call drops the
// handlers registered by the first (audit: settools-accumulates-stale-handlers).
func TestSetTools_ReplacesStaleHandlers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(t *testing.T, url string) chat.ChatClient
	}{
		{"claude", func(t *testing.T, url string) chat.ChatClient { return claudeAuditClient(t, url) }},
		{"openai", func(t *testing.T, url string) chat.ChatClient { return openaiAuditClient(t, url) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := NewMockServer()
			defer server.Close()

			client := tc.build(t, server.URL)

			var oldCalled, newCalled bool

			require.NoError(t, client.SetTools([]chat.Tool{toolWithHandler("old_tool", &oldCalled)}))
			// Second call must fully replace the first's handler map.
			require.NoError(t, client.SetTools([]chat.Tool{toolWithHandler("new_tool", &newCalled)}))

			// Provider asks to invoke the OLD tool name; with the handler map
			// replaced it must not resolve to the stale handler.
			step := 0
			server.Handler = claudeOrOpenAIToolHandler(tc.name, &step, "old_tool")

			_, err := client.Chat(context.Background(), "go")
			require.NoError(t, err)

			assert.False(t, oldCalled, "stale handler from first SetTools must not run after a second SetTools")
		})
	}
}

// claudeOrOpenAIToolHandler returns an httptest handler that first asks the
// model to call toolName, then returns a final text answer.
func claudeOrOpenAIToolHandler(provider string, step *int, toolName string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		first := *step == 0
		*step++

		if provider == "claude" {
			if first {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "m", "type": "message", "role": "assistant", "model": "claude-3-5-sonnet",
					"content": []map[string]any{
						{"type": "tool_use", "id": "t1", "name": toolName, "input": map[string]any{}},
					},
					"stop_reason": "tool_use",
				})

				return
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "m2", "type": "message", "role": "assistant", "model": "claude-3-5-sonnet",
				"content":     []map[string]any{{"type": "text", "text": "done"}},
				"stop_reason": "end_turn",
			})

			return
		}

		// openai
		if first {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "c", "choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"id": "t1", "type": "function",
							"function": map[string]any{"name": toolName, "arguments": "{}"},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			})

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c2", "choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "done"},
				"finish_reason": "stop",
			}},
		})
	}
}

// TestClaude_SetTools_ResetsResponseSchema asserts the Claude-only
// ResponseSchema reset still fires on SetTools (consistency alignment).
func TestClaude_SetTools_ResetsResponseSchema(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	client := claudeAuditClient(t, server.URL, func(c *chat.Config) {
		c.ResponseSchema = chat.GenerateSchema[staleArgs]()
		c.SchemaName = "x"
	})

	var capturedTools any

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedTools = body["tools"]

		writeJSON(w, map[string]any{
			"id": "m", "type": "message", "role": "assistant", "model": "claude-3-5-sonnet",
			"content":     []map[string]any{{"type": "text", "text": "hi"}},
			"stop_reason": "end_turn",
		})
	}

	var called bool

	require.NoError(t, client.SetTools([]chat.Tool{toolWithHandler("only", &called)}))

	_, err := client.Chat(context.Background(), "go")
	require.NoError(t, err)

	// After SetTools the response schema is cleared, so the request carries the
	// real tool list (one tool), not a schema-enforcing submit_response tool.
	toolsArr, ok := capturedTools.([]any)
	require.True(t, ok, "expected a tools array on the request")
	require.Len(t, toolsArr, 1)

	tool0 := toolsArr[0].(map[string]any)
	assert.Equal(t, "only", tool0["name"])
}

// TestClaude_SystemPrompt_UsesSystemField asserts the system prompt lands in
// the dedicated System field, not as a user-role message (audit:
// claude-system-prompt-as-user-message).
func TestClaude_SystemPrompt_UsesSystemField(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	client := claudeAuditClient(t, server.URL, func(c *chat.Config) {
		c.SystemPrompt = "You are a pirate."
	})

	var captured map[string]any

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		captured = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&captured)

		writeJSON(w, map[string]any{
			"id": "m", "type": "message", "role": "assistant", "model": "claude-3-5-sonnet",
			"content":     []map[string]any{{"type": "text", "text": "arr"}},
			"stop_reason": "end_turn",
		})
	}

	_, err := client.Chat(context.Background(), "hello")
	require.NoError(t, err)

	// System field must carry the prompt.
	system, ok := captured["system"]
	require.True(t, ok, "request must include a system field")
	require.NotEmpty(t, system)

	// And no user message may equal the system prompt.
	msgs, ok := captured["messages"].([]any)
	require.True(t, ok)

	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] != "user" {
			continue
		}

		assert.NotContains(t, flattenText(mm["content"]), "You are a pirate.",
			"system prompt must not be smuggled in as a user message")
	}
}

// flattenText extracts text from a Claude content value that may be a plain
// string or an array of content blocks.
func flattenText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var out string
		for _, b := range c {
			if bm, ok := b.(map[string]any); ok {
				if s, ok := bm["text"].(string); ok {
					out += s
				}
			}
		}

		return out
	default:
		return ""
	}
}

// TestOpenAI_Seed asserts the seed is omitted by default and sent when set
// (audit: openai-hardcoded-seed-zero).
func TestOpenAI_Seed(t *testing.T) {
	t.Parallel()

	seed := int64(42)

	tests := []struct {
		name      string
		seed      *int64
		wantSeed  bool
		wantValue float64
	}{
		{"omitted_by_default", nil, false, 0},
		{"sent_when_configured", &seed, true, 42},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := NewMockServer()
			defer server.Close()

			client := openaiAuditClient(t, server.URL, func(c *chat.Config) {
				c.Seed = tc.seed
			})

			var (
				seedPresent bool
				seedValue   float64
			)

			server.Handler = func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				_, seedPresent = body["seed"]
				seedValue, _ = body["seed"].(float64)

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "c", "choices": []map[string]any{{
						"message":       map[string]any{"role": "assistant", "content": "ok"},
						"finish_reason": "stop",
					}},
				})
			}

			_, err := client.Chat(context.Background(), "hi")
			require.NoError(t, err)

			assert.Equal(t, tc.wantSeed, seedPresent, "seed presence on the request body")

			if tc.wantSeed {
				assert.InEpsilon(t, tc.wantValue, seedValue, 0.001)
			}
		})
	}
}

// TestClaude_Stream_EmptyToolArgs_NormalisedToObject asserts a streamed tool
// call with no input_json_delta events hands the handler "{}" (valid JSON),
// not "" (audit: claude-stream-empty-tool-args-invalid-json).
func TestClaude_Stream_EmptyToolArgs_NormalisedToObject(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	client := claudeAuditClient(t, server.URL)
	streamer, ok := client.(chat.StreamingChatClient)
	require.True(t, ok)

	var gotArgs string

	require.NoError(t, client.SetTools([]chat.Tool{{
		Name:        "noargs",
		Description: "takes no args",
		Parameters:  chat.GenerateSchema[staleArgs]().(*jsonschema.Schema),
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			gotArgs = string(args)
			// Must be parseable JSON.
			var m map[string]any

			return nil, json.Unmarshal(args, &m)
		},
	}}))

	step := 0

	server.Handler = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		if step == 0 {
			// tool_use block with NO input_json_delta -> empty argBuf.
			for _, e := range []string{
				sseEvent(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "tu1", "name": "noargs", "input": map[string]any{}}}),
				sseEvent(map[string]any{"type": "content_block_stop", "index": 0}),
				sseEvent(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}}),
				sseEvent(map[string]any{"type": "message_stop"}),
			} {
				_, _ = fmt.Fprint(w, e)
			}

			step++
		} else {
			for _, e := range []string{
				sseEvent(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "done"}}),
				sseEvent(map[string]any{"type": "message_stop"}),
			} {
				_, _ = fmt.Fprint(w, e)
			}
		}

		flusher.Flush()
	}

	result, err := streamer.StreamChat(context.Background(), "go", func(_ chat.StreamEvent) error { return nil })
	require.NoError(t, err)
	assert.Equal(t, "done", result)
	assert.Equal(t, "{}", gotArgs, "empty streamed tool args must normalise to {}")
}

// TestClaude_Restore_RebuildsSystemField asserts a restored snapshot's system
// prompt is sent in the System field on the next request — the prompt no longer
// lives inside the serialised message history.
func TestClaude_Restore_RebuildsSystemField(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	client := claudeAuditClient(t, server.URL)

	pc, ok := client.(chat.PersistentChatClient)
	require.True(t, ok)

	snap := chat.NewSnapshot(chat.ProviderClaude, "claude-3-5-sonnet", "You are restored.", json.RawMessage(`[]`), nil, nil)
	require.NoError(t, pc.Restore(snap))

	var captured map[string]any

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		captured = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&captured)

		writeJSON(w, map[string]any{
			"id": "m", "type": "message", "role": "assistant", "model": "claude-3-5-sonnet",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
		})
	}

	_, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)

	system, ok := captured["system"]
	require.True(t, ok, "restored client must send the system prompt in the system field")
	require.NotEmpty(t, system)
}
