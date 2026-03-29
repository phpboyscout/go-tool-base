package chat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

type parallelToolArgs struct {
	Value string `json:"value"`
}

func TestClaudeProvider_Chat_ParallelTools(t *testing.T) {
	t.Parallel()

	t.Run("parallel_path_taken", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		cfgMock := mockConfig.NewMockContainable(t)
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

		p := &props.Props{Logger: logger.NewNoop(), Config: cfgMock}

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:      chat.ProviderClaude,
			Token:         "test-key",
			BaseURL:       server.URL + "/",
			ParallelTools: true,
		})
		require.NoError(t, err)

		step := 0
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var resp map[string]interface{}
			if step == 0 {
				resp = map[string]interface{}{
					"id":    "msg_parallel",
					"type":  "message",
					"role":  "assistant",
					"model": "claude-sonnet-4-6",
					"content": []map[string]interface{}{
						{"type": "tool_use", "id": "toolu_a", "name": "tool_a", "input": map[string]interface{}{"value": "alpha"}},
						{"type": "tool_use", "id": "toolu_b", "name": "tool_b", "input": map[string]interface{}{"value": "beta"}},
					},
					"stop_reason": "tool_use",
				}
				step++
			} else {
				resp = map[string]interface{}{
					"id":    "msg_final",
					"type":  "message",
					"role":  "assistant",
					"model": "claude-sonnet-4-6",
					"content": []map[string]interface{}{
						{"type": "text", "text": "done"},
					},
					"stop_reason": "end_turn",
				}
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		var aCalled, bCalled atomic.Bool

		err = client.SetTools([]chat.Tool{
			{
				Name:        "tool_a",
				Description: "Tool A",
				Parameters:  chat.GenerateSchema[parallelToolArgs]().(*jsonschema.Schema),
				Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
					aCalled.Store(true)
					return "result-a", nil
				},
			},
			{
				Name:        "tool_b",
				Description: "Tool B",
				Parameters:  chat.GenerateSchema[parallelToolArgs]().(*jsonschema.Schema),
				Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
					bCalled.Store(true)
					return "result-b", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "run both tools")
		require.NoError(t, err)
		assert.Equal(t, "done", resp)
		assert.True(t, aCalled.Load(), "tool_a should have been called")
		assert.True(t, bCalled.Load(), "tool_b should have been called")
	})
}

func TestOpenAIProvider_Chat_ParallelTools(t *testing.T) {
	t.Parallel()

	t.Run("parallel_path_taken", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		cfgMock := mockConfig.NewMockContainable(t)
		cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

		p := &props.Props{Logger: logger.NewNoop(), Config: cfgMock}

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:      chat.ProviderOpenAI,
			Token:         "test-key",
			BaseURL:       server.URL + "/",
			ParallelTools: true,
		})
		require.NoError(t, err)

		step := 0
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var resp map[string]interface{}
			if step == 0 {
				resp = map[string]interface{}{
					"id":     "chatcmpl_parallel",
					"object": "chat.completion",
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"message": map[string]interface{}{
								"role":    "assistant",
								"content": "",
								"tool_calls": []map[string]interface{}{
									{"id": "call_a", "type": "function", "function": map[string]interface{}{"name": "tool_a", "arguments": `{"value":"alpha"}`}},
									{"id": "call_b", "type": "function", "function": map[string]interface{}{"name": "tool_b", "arguments": `{"value":"beta"}`}},
								},
							},
							"finish_reason": "tool_calls",
						},
					},
				}
				step++
			} else {
				resp = map[string]interface{}{
					"id":     "chatcmpl_final",
					"object": "chat.completion",
					"choices": []map[string]interface{}{
						{
							"index":         0,
							"message":       map[string]interface{}{"role": "assistant", "content": "done"},
							"finish_reason": "stop",
						},
					},
				}
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		var aCalled, bCalled atomic.Bool

		err = client.SetTools([]chat.Tool{
			{
				Name:        "tool_a",
				Description: "Tool A",
				Parameters:  chat.GenerateSchema[parallelToolArgs]().(*jsonschema.Schema),
				Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
					aCalled.Store(true)
					return "result-a", nil
				},
			},
			{
				Name:        "tool_b",
				Description: "Tool B",
				Parameters:  chat.GenerateSchema[parallelToolArgs]().(*jsonschema.Schema),
				Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
					bCalled.Store(true)
					return "result-b", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "run both tools")
		require.NoError(t, err)
		assert.Equal(t, "done", resp)
		assert.True(t, aCalled.Load(), "tool_a should have been called")
		assert.True(t, bCalled.Load(), "tool_b should have been called")
	})
}

func TestGeminiProvider_Chat_ParallelTools(t *testing.T) {
	t.Parallel()

	t.Run("parallel_path_taken", func(t *testing.T) {
		t.Parallel()

		server := NewMockServer()
		defer server.Close()

		cfgMock := mockConfig.NewMockContainable(t)
		cfgMock.EXPECT().GetString(chat.ConfigKeyGeminiKey).Return("test-key").Maybe()

		p := &props.Props{Logger: logger.NewNoop(), Config: cfgMock}

		client, err := chat.New(context.Background(), p, chat.Config{
			Provider:      chat.ProviderGemini,
			Token:         "test-key",
			BaseURL:       server.URL,
			ParallelTools: true,
		})
		require.NoError(t, err)

		step := 0
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var resp map[string]interface{}
			if step == 0 {
				resp = map[string]interface{}{
					"candidates": []map[string]interface{}{
						{
							"content": map[string]interface{}{
								"parts": []map[string]interface{}{
									{"functionCall": map[string]interface{}{"name": "tool_a", "args": map[string]interface{}{"value": "alpha"}}},
									{"functionCall": map[string]interface{}{"name": "tool_b", "args": map[string]interface{}{"value": "beta"}}},
								},
							},
						},
					},
				}
				step++
			} else {
				resp = map[string]interface{}{
					"candidates": []map[string]interface{}{
						{
							"content": map[string]interface{}{
								"parts": []map[string]interface{}{
									{"text": "done"},
								},
							},
						},
					},
				}
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		var aCalled, bCalled atomic.Bool

		err = client.SetTools([]chat.Tool{
			{
				Name:        "tool_a",
				Description: "Tool A",
				Parameters:  chat.GenerateSchema[parallelToolArgs]().(*jsonschema.Schema),
				Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
					aCalled.Store(true)
					return "result-a", nil
				},
			},
			{
				Name:        "tool_b",
				Description: "Tool B",
				Parameters:  chat.GenerateSchema[parallelToolArgs]().(*jsonschema.Schema),
				Handler: func(_ context.Context, _ json.RawMessage) (interface{}, error) {
					bCalled.Store(true)
					return "result-b", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "run both tools")
		require.NoError(t, err)
		assert.Equal(t, "done", resp)
		assert.True(t, aCalled.Load(), "tool_a should have been called")
		assert.True(t, bCalled.Load(), "tool_b should have been called")
	})
}
