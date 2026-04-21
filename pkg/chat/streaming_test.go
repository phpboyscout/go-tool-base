package chat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// sseEvent formats a JSON payload as a server-sent event line.
// If the payload contains a "type" field, it is emitted as the SSE event header
// so that the Anthropic SDK's ssestream decoder can route it correctly.
func sseEvent(data map[string]interface{}) string {
	b, err := json.Marshal(data)
	if err != nil {
		panic(fmt.Sprintf("sseEvent: failed to marshal: %v", err))
	}

	if t, ok := data["type"].(string); ok && t != "" {
		return fmt.Sprintf("event: %s\ndata: %s\n\n", t, b)
	}

	return fmt.Sprintf("data: %s\n\n", b)
}

// sseDone returns the SSE stream terminator used by OpenAI.
func sseDone() string { return "data: [DONE]\n\n" }

// ---- helpers ----------------------------------------------------------------

func claudeStreamProps(t *testing.T) (*props.Props, string) {
	t.Helper()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("").Maybe()
	cfg.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("").Maybe()
	cfg.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

	return &props.Props{Logger: logger.NewNoop(), Config: cfg}, "test-key"
}

func openaiStreamProps(t *testing.T) (*props.Props, string) {
	t.Helper()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("").Maybe()
	cfg.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("").Maybe()
	cfg.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

	return &props.Props{Logger: logger.NewNoop(), Config: cfg}, "test-key"
}

func geminiStreamProps(t *testing.T) (*props.Props, string) {
	t.Helper()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyGeminiEnv).Return("").Maybe()
	cfg.EXPECT().GetString(chat.ConfigKeyGeminiKeychain).Return("").Maybe()
	cfg.EXPECT().GetString(chat.ConfigKeyGeminiKey).Return("test-key").Maybe()

	return &props.Props{Logger: logger.NewNoop(), Config: cfg}, "test-key"
}

// ---- StreamEventType --------------------------------------------------------

func TestStreamEventTypes_Values(t *testing.T) {
	t.Parallel()

	assert.Equal(t, chat.EventTextDelta, chat.StreamEventType(0))
	assert.Equal(t, chat.EventToolCallStart, chat.StreamEventType(1))
	assert.Equal(t, chat.EventToolCallEnd, chat.StreamEventType(2))
	assert.Equal(t, chat.EventComplete, chat.StreamEventType(3))
	assert.Equal(t, chat.EventError, chat.StreamEventType(4))
}

// ---- ClaudeLocal does not implement StreamingChatClient --------------------

func TestClaudeLocal_NotStreaming(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	p := &props.Props{Logger: logger.NewNoop(), Config: cfg}

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:     chat.ProviderClaudeLocal,
		Model:        "claude-sonnet-4-6",
		ExecLookPath: func(_ string) (string, error) { return "/usr/local/bin/claude", nil },
	})
	require.NoError(t, err)

	_, ok := client.(chat.StreamingChatClient)
	assert.False(t, ok, "ClaudeLocal should not implement StreamingChatClient")
}

// ---- Claude StreamChat ------------------------------------------------------

func TestClaude_StreamChat_Success(t *testing.T) {
	t.Parallel()

	t.Run("text_deltas_delivered", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		p, _ := claudeStreamProps(t)

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:             chat.ProviderClaude,
			Token:                "test-key",
			BaseURL:              server.URL + "/",
			AllowInsecureBaseURL: true,
		})
		require.NoError(t, err)

		streamer, ok := client.(chat.StreamingChatClient)
		require.True(t, ok, "Claude should implement StreamingChatClient")

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher := w.(http.Flusher)

			events := []string{
				sseEvent(map[string]interface{}{"type": "message_start", "message": map[string]interface{}{"id": "msg_1", "type": "message", "role": "assistant", "content": []interface{}{}, "model": "claude-sonnet-4-6"}}),
				sseEvent(map[string]interface{}{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "text", "text": ""}}),
				sseEvent(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": "Hello"}}),
				sseEvent(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": ", world"}}),
				sseEvent(map[string]interface{}{"type": "content_block_stop", "index": 0}),
				sseEvent(map[string]interface{}{"type": "message_stop"}),
			}

			for _, e := range events {
				_, _ = fmt.Fprint(w, e)
				flusher.Flush()
			}
		}

		var deltas []string

		result, err := streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
			if e.Type == chat.EventTextDelta {
				deltas = append(deltas, e.Delta)
			}

			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, "Hello, world", result)
		assert.Equal(t, []string{"Hello", ", world"}, deltas)
	})

	t.Run("complete_event_delivered", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		p, _ := claudeStreamProps(t)

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:             chat.ProviderClaude,
			Token:                "test-key",
			BaseURL:              server.URL + "/",
			AllowInsecureBaseURL: true,
		})
		require.NoError(t, err)

		streamer := client.(chat.StreamingChatClient)

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": "ok"}}))
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"type": "message_stop"}))
			flusher.Flush()
		}

		var gotComplete bool

		_, err = streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
			if e.Type == chat.EventComplete {
				gotComplete = true
			}

			return nil
		})

		require.NoError(t, err)
		assert.True(t, gotComplete, "EventComplete should be delivered")
	})
}

func TestClaude_StreamChat_CallbackError(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := claudeStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderClaude,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": "Hello"}}))
		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": " more"}}))
		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"type": "message_stop"}))
		flusher.Flush()
	}

	cbErr := errors.New("stop now")

	result, err := streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
		if e.Type == chat.EventTextDelta && e.Delta == "Hello" {
			return cbErr
		}

		return nil
	})

	assert.Equal(t, cbErr, err)
	assert.Equal(t, "Hello", result, "partial text should be returned on callback error")
}

func TestClaude_StreamChat_EmptyPrompt(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := claudeStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderClaude,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	result, err := streamer.StreamChat(context.Background(), "", func(_ chat.StreamEvent) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt cannot be empty")
	assert.Empty(t, result)
}

func TestClaude_StreamChat_WithToolCalls(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := claudeStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderClaude,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	type weatherArgs struct {
		Location string `json:"location"`
	}

	err = client.SetTools([]chat.Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
			Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
				return "sunny", nil
			},
		},
	})
	require.NoError(t, err)

	step := 0
	var toolStartSeen, toolEndSeen bool

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		if step == 0 {
			// First response: tool use block
			events := []string{
				sseEvent(map[string]interface{}{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]interface{}{}}}),
				sseEvent(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "input_json_delta", "partial_json": `{"location":"London"}`}}),
				sseEvent(map[string]interface{}{"type": "content_block_stop", "index": 0}),
				sseEvent(map[string]interface{}{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": "tool_use"}}),
				sseEvent(map[string]interface{}{"type": "message_stop"}),
			}
			for _, e := range events {
				_, _ = fmt.Fprint(w, e)
			}
			step++
		} else {
			// Second response: final text
			events := []string{
				sseEvent(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": "London is sunny."}}),
				sseEvent(map[string]interface{}{"type": "message_stop"}),
			}
			for _, e := range events {
				_, _ = fmt.Fprint(w, e)
			}
		}

		flusher.Flush()
	}

	result, err := streamer.StreamChat(context.Background(), "What is the weather?", func(e chat.StreamEvent) error {
		switch e.Type { //nolint:exhaustive // only tool call events are relevant here
		case chat.EventToolCallStart:
			toolStartSeen = true
		case chat.EventToolCallEnd:
			toolEndSeen = true
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "London is sunny.", result)
	assert.True(t, toolStartSeen, "EventToolCallStart should be emitted")
	assert.True(t, toolEndSeen, "EventToolCallEnd should be emitted")
}

// ---- OpenAI StreamChat ------------------------------------------------------

func TestOpenAI_StreamChat_Success(t *testing.T) {
	t.Parallel()

	t.Run("text_deltas_delivered", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		p, _ := openaiStreamProps(t)

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:             chat.ProviderOpenAI,
			Token:                "test-key",
			BaseURL:              server.URL + "/",
			AllowInsecureBaseURL: true,
		})
		require.NoError(t, err)

		streamer, ok := client.(chat.StreamingChatClient)
		require.True(t, ok, "OpenAI should implement StreamingChatClient")

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)

			chunks := []string{
				sseEvent(map[string]interface{}{"id": "chatcmpl_1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": ""}, "finish_reason": nil}}}),
				sseEvent(map[string]interface{}{"id": "chatcmpl_1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": "Hello"}, "finish_reason": nil}}}),
				sseEvent(map[string]interface{}{"id": "chatcmpl_1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": ", world"}, "finish_reason": nil}}}),
				sseEvent(map[string]interface{}{"id": "chatcmpl_1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"}}}),
				sseDone(),
			}

			for _, c := range chunks {
				_, _ = fmt.Fprint(w, c)
				flusher.Flush()
			}
		}

		var deltas []string

		result, err := streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
			if e.Type == chat.EventTextDelta {
				deltas = append(deltas, e.Delta)
			}

			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, "Hello, world", result)
		assert.Equal(t, []string{"Hello", ", world"}, deltas)
	})

	t.Run("complete_event_delivered", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		p, _ := openaiStreamProps(t)

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:             chat.ProviderOpenAI,
			Token:                "test-key",
			BaseURL:              server.URL + "/",
			AllowInsecureBaseURL: true,
		})
		require.NoError(t, err)

		streamer := client.(chat.StreamingChatClient)

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": "ok"}, "finish_reason": nil}}}))
			_, _ = fmt.Fprint(w, sseDone())
			flusher.Flush()
		}

		var gotComplete bool

		_, err = streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
			if e.Type == chat.EventComplete {
				gotComplete = true
			}

			return nil
		})

		require.NoError(t, err)
		assert.True(t, gotComplete)
	})
}

func TestOpenAI_StreamChat_CallbackError(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := openaiStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": "Hello"}, "finish_reason": nil}}}))
		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": " more"}, "finish_reason": nil}}}))
		_, _ = fmt.Fprint(w, sseDone())
		flusher.Flush()
	}

	cbErr := errors.New("stop now")

	result, err := streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
		if e.Type == chat.EventTextDelta && e.Delta == "Hello" {
			return cbErr
		}

		return nil
	})

	assert.Equal(t, cbErr, err)
	assert.Equal(t, "Hello", result)
}

func TestOpenAI_StreamChat_EmptyPrompt(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := openaiStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	result, err := streamer.StreamChat(context.Background(), "", func(_ chat.StreamEvent) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt cannot be empty")
	assert.Empty(t, result)
}

func TestOpenAI_StreamChat_WithToolCalls(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := openaiStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	type weatherArgs struct {
		Location string `json:"location"`
	}

	err = client.SetTools([]chat.Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
			Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
				return "sunny", nil
			},
		},
	})
	require.NoError(t, err)

	step := 0
	var toolStartSeen, toolEndSeen bool

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		if step == 0 {
			chunks := []string{
				sseEvent(map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": nil, "tool_calls": []map[string]interface{}{{"index": 0, "id": "call_1", "type": "function", "function": map[string]interface{}{"name": "get_weather", "arguments": ""}}}}, "finish_reason": nil}}}),
				sseEvent(map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"tool_calls": []map[string]interface{}{{"index": 0, "function": map[string]interface{}{"arguments": `{"location":"London"}`}}}}, "finish_reason": nil}}}),
				sseEvent(map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "tool_calls"}}}),
				sseDone(),
			}
			for _, c := range chunks {
				_, _ = fmt.Fprint(w, c)
			}
			step++
		} else {
			chunks := []string{
				sseEvent(map[string]interface{}{"id": "c2", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": "London is sunny."}, "finish_reason": nil}}}),
				sseEvent(map[string]interface{}{"id": "c2", "object": "chat.completion.chunk", "choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"}}}),
				sseDone(),
			}
			for _, c := range chunks {
				_, _ = fmt.Fprint(w, c)
			}
		}

		flusher.Flush()
	}

	result, err := streamer.StreamChat(context.Background(), "What is the weather?", func(e chat.StreamEvent) error {
		switch e.Type { //nolint:exhaustive // only tool call events are relevant here
		case chat.EventToolCallStart:
			toolStartSeen = true
		case chat.EventToolCallEnd:
			toolEndSeen = true
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "London is sunny.", result)
	assert.True(t, toolStartSeen, "EventToolCallStart should be emitted")
	assert.True(t, toolEndSeen, "EventToolCallEnd should be emitted")
}

// ---- Gemini StreamChat ------------------------------------------------------

func TestGemini_StreamChat_Success(t *testing.T) {
	t.Parallel()

	t.Run("text_deltas_delivered", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		p, _ := geminiStreamProps(t)

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:             chat.ProviderGemini,
			Token:                "test-key",
			BaseURL:              server.URL,
			AllowInsecureBaseURL: true,
		})
		require.NoError(t, err)

		streamer, ok := client.(chat.StreamingChatClient)
		require.True(t, ok, "Gemini should implement StreamingChatClient")

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)

			chunks := []string{
				sseEvent(map[string]interface{}{"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": "Hello"}}}}}}),
				sseEvent(map[string]interface{}{"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": ", world"}}}, "finishReason": "STOP"}}}),
			}

			for _, c := range chunks {
				_, _ = fmt.Fprint(w, c)
				flusher.Flush()
			}
		}

		var deltas []string

		result, err := streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
			if e.Type == chat.EventTextDelta {
				deltas = append(deltas, e.Delta)
			}

			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, "Hello, world", result)
		assert.Equal(t, []string{"Hello", ", world"}, deltas)
	})

	t.Run("complete_event_delivered", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		p, _ := geminiStreamProps(t)

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:             chat.ProviderGemini,
			Token:                "test-key",
			BaseURL:              server.URL,
			AllowInsecureBaseURL: true,
		})
		require.NoError(t, err)

		streamer := client.(chat.StreamingChatClient)

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": "ok"}}}, "finishReason": "STOP"}}}))
			flusher.Flush()
		}

		var gotComplete bool

		_, err = streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
			if e.Type == chat.EventComplete {
				gotComplete = true
			}

			return nil
		})

		require.NoError(t, err)
		assert.True(t, gotComplete)
	})
}

func TestGemini_StreamChat_CallbackError(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := geminiStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": "Hello"}}}}}}))
		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": " more"}}}, "finishReason": "STOP"}}}))
		flusher.Flush()
	}

	cbErr := errors.New("stop now")

	result, err := streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
		if e.Type == chat.EventTextDelta && e.Delta == "Hello" {
			return cbErr
		}

		return nil
	})

	assert.Equal(t, cbErr, err)
	assert.Equal(t, "Hello", result)
}

func TestGemini_StreamChat_EmptyPrompt(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := geminiStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	result, err := streamer.StreamChat(context.Background(), "", func(_ chat.StreamEvent) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt cannot be empty")
	assert.Empty(t, result)
}

func TestGemini_StreamChat_WithToolCalls(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	p, _ := geminiStreamProps(t)

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer := client.(chat.StreamingChatClient)

	type weatherArgs struct {
		Location string `json:"location"`
	}

	err = client.SetTools([]chat.Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
			Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
				return "sunny", nil
			},
		},
	})
	require.NoError(t, err)

	step := 0
	var toolStartSeen, toolEndSeen bool

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		if step == 0 {
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]interface{}{{"functionCall": map[string]interface{}{"name": "get_weather", "args": map[string]interface{}{"location": "London"}}}}}, "finishReason": "STOP"}}}))
			step++
		} else {
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": "London is sunny."}}}, "finishReason": "STOP"}}}))
		}

		flusher.Flush()
	}

	result, err := streamer.StreamChat(context.Background(), "What is the weather?", func(e chat.StreamEvent) error {
		switch e.Type { //nolint:exhaustive // only tool call events are relevant here
		case chat.EventToolCallStart:
			toolStartSeen = true
		case chat.EventToolCallEnd:
			toolEndSeen = true
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "London is sunny.", result)
	assert.True(t, toolStartSeen, "EventToolCallStart should be emitted")
	assert.True(t, toolEndSeen, "EventToolCallEnd should be emitted")
}
