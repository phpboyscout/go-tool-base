package chat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/internal/testutil"
	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// TestStreamingIntegration_LiveSSERoundTrip verifies that all deltas are delivered
// in order and the assembled text matches the concatenated deltas.
func TestStreamingIntegration_LiveSSERoundTrip(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

	p := &props.Props{Logger: logger.NewNoop(), Config: cfg}

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderClaude,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer, ok := client.(chat.StreamingChatClient)
	require.True(t, ok)

	wantDeltas := []string{"Hello", ", ", "world", "!"}

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []map[string]interface{}{
			{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "text", "text": ""}},
		}
		for _, d := range wantDeltas {
			events = append(events, map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{"type": "text_delta", "text": d},
			})
		}
		events = append(events, map[string]interface{}{"type": "message_stop"})

		for _, e := range events {
			_, _ = fmt.Fprint(w, sseEvent(e))
			flusher.Flush()
		}
	}

	var gotDeltas []string
	var gotComplete bool

	result, err := streamer.StreamChat(context.Background(), "hi", func(e chat.StreamEvent) error {
		switch e.Type { //nolint:exhaustive // only relevant events handled
		case chat.EventTextDelta:
			gotDeltas = append(gotDeltas, e.Delta)
		case chat.EventComplete:
			gotComplete = true
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "Hello, world!", result)
	assert.Equal(t, wantDeltas, gotDeltas)
	assert.True(t, gotComplete)
}

// TestStreamingIntegration_ContextCancellation verifies that cancelling the context
// mid-stream returns promptly with the partial result.
func TestStreamingIntegration_ContextCancellation(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

	p := &props.Props{Logger: logger.NewNoop(), Config: cfg}

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer, ok := client.(chat.StreamingChatClient)
	require.True(t, ok)

	started := make(chan struct{})

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{
			"id": "c1", "object": "chat.completion.chunk",
			"choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": "partial"}, "finish_reason": nil}},
		}))
		flusher.Flush()

		close(started)

		// Simulate a slow stream — the client should cancel before this arrives.
		time.Sleep(5 * time.Second)

		_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{
			"id": "c1", "object": "chat.completion.chunk",
			"choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": " more"}, "finish_reason": "stop"}},
		}))
		_, _ = fmt.Fprint(w, sseDone())
		flusher.Flush()
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-started
		cancel()
	}()

	start := time.Now()

	result, err := streamer.StreamChat(ctx, "hi", func(_ chat.StreamEvent) error { return nil })

	elapsed := time.Since(start)

	require.Error(t, err, "expected error on context cancellation")
	assert.Less(t, elapsed, 3*time.Second, "StreamChat should return promptly after cancellation")
	// partial text may or may not be returned depending on timing
	_ = result
}

// TestStreamingIntegration_ToolCallDuringStream verifies that a tool call emitted
// during streaming is executed and the stream resumes with the tool result.
func TestStreamingIntegration_ToolCallDuringStream(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyGeminiKey).Return("test-key").Maybe()

	p := &props.Props{Logger: logger.NewNoop(), Config: cfg}

	client, err := chat.New(context.Background(), p, chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	})
	require.NoError(t, err)

	streamer, ok := client.(chat.StreamingChatClient)
	require.True(t, ok)

	type weatherArgs struct {
		Location string `json:"location"`
	}

	err = client.SetTools([]chat.Tool{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
			Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
				return "sunny and warm", nil
			},
		},
	})
	require.NoError(t, err)

	step := 0

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		if step == 0 {
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{
				"candidates": []map[string]interface{}{{
					"content": map[string]interface{}{
						"parts": []map[string]interface{}{{
							"functionCall": map[string]interface{}{
								"name": "get_weather",
								"args": map[string]interface{}{"location": "London"},
							},
						}},
					},
					"finishReason": "STOP",
				}},
			}))
			step++
		} else {
			_, _ = fmt.Fprint(w, sseEvent(map[string]interface{}{
				"candidates": []map[string]interface{}{{
					"content": map[string]interface{}{
						"parts": []map[string]interface{}{{"text": "London is sunny and warm."}},
					},
					"finishReason": "STOP",
				}},
			}))
		}

		flusher.Flush()
	}

	var toolStartSeen, toolEndSeen bool

	result, err := streamer.StreamChat(context.Background(), "What is the weather in London?", func(e chat.StreamEvent) error {
		switch e.Type { //nolint:exhaustive // only tool call events are relevant here
		case chat.EventToolCallStart:
			toolStartSeen = true
		case chat.EventToolCallEnd:
			toolEndSeen = true
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "London is sunny and warm.", result)
	assert.True(t, toolStartSeen, "EventToolCallStart should be emitted")
	assert.True(t, toolEndSeen, "EventToolCallEnd should be emitted")
}
