package chat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeProvider_New(t *testing.T) {
	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	t.Run("missing_api_key", func(t *testing.T) {
		t.Setenv(chat.EnvClaudeKey, "")
		cfg := chat.Config{
			Provider: chat.ProviderClaude,
			Token:    "",
		}
		client, err := chat.New(context.Background(), p, cfg)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "Anthropic API key is required")
	})

	t.Run("success", func(t *testing.T) {
		cfg := chat.Config{
			Provider: chat.ProviderClaude,
			Token:    "test-key",
		}
		client, err := chat.New(context.Background(), p, cfg)
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("success_from_props", func(t *testing.T) {
		cfgMock := mockConfig.NewMockContainable(t)
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key")
		pWithKey := &props.Props{
			Logger: logger.NewNoop(),
			Config: cfgMock,
		}
		cfg := chat.Config{Provider: chat.ProviderClaude}
		client, err := chat.New(context.Background(), pWithKey, cfg)
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("success_from_env", func(t *testing.T) {
		t.Setenv(chat.EnvClaudeKey, "env-key")
		cfg := chat.Config{Provider: chat.ProviderClaude}
		client, err := chat.New(context.Background(), p, cfg)
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestClaudeProvider_Ask(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	cfg := chat.Config{
		Provider: chat.ProviderClaude,
		Token:    "test-key",
		BaseURL:  server.URL + "/",
	}

	client, err := chat.New(context.Background(), p, cfg)
	require.NoError(t, err)

	t.Run("success_structured", func(t *testing.T) {
		type response struct {
			Answer string `json:"answer"`
		}

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Mock Claude tool use response
			resp := map[string]interface{}{
				"id":    "msg_123",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-5-sonnet-20240620",
				"content": []map[string]interface{}{
					{
						"type": "tool_use",
						"id":   "toolu_123",
						"name": "submit_response",
						"input": map[string]interface{}{
							"answer": "The answer is 42",
						},
					},
				},
				"stop_reason": "tool_use",
			}
			json.NewEncoder(w).Encode(resp)
		}

		var target response
		err := client.Ask(context.Background(), "What is the answer?", &target)
		assert.NoError(t, err)
		assert.Equal(t, "The answer is 42", target.Answer)
	})

	t.Run("no_tool_use_error", func(t *testing.T) {
		cfgWithSchema := chat.Config{
			Provider:       chat.ProviderClaude,
			Token:          "test-key",
			BaseURL:        server.URL + "/",
			ResponseSchema: map[string]interface{}{"type": "object"},
		}
		clientWithSchema, err := chat.New(context.Background(), p, cfgWithSchema)
		require.NoError(t, err)

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"id":    "msg_no_tool",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-5-sonnet",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "Just some text, no tool use here.",
					},
				},
				"stop_reason": "end_turn",
			}
			json.NewEncoder(w).Encode(resp)
		}

		var target map[string]interface{}
		err = clientWithSchema.Ask(context.Background(), "test", &target)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Claude did not provide a tool use response")
	})

	t.Run("with_response_schema", func(t *testing.T) {
		type response struct {
			Answer string `json:"answer"`
		}
		cfgWithSchema := chat.Config{
			Provider:       chat.ProviderClaude,
			Token:          "test-key",
			BaseURL:        server.URL + "/",
			ResponseSchema: chat.GenerateSchema[response](),
		}
		clientWithSchema, err := chat.New(context.Background(), p, cfgWithSchema)
		require.NoError(t, err)

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"id":    "msg_schema",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-5-sonnet",
				"content": []map[string]interface{}{
					{
						"type": "tool_use",
						"id":   "toolu_schema",
						"name": "submit_response",
						"input": map[string]interface{}{
							"answer": "Structured 42",
						},
					},
				},
				"stop_reason": "tool_use",
			}
			json.NewEncoder(w).Encode(resp)
		}

		var target response
		err = clientWithSchema.Ask(context.Background(), "test", &target)
		assert.NoError(t, err)
		assert.Equal(t, "Structured 42", target.Answer)
	})

	t.Run("malformed_json_response", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Return tool_use but with malformed input (missing closing brace)
			w.Write([]byte(`{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"content": [
					{
						"type": "tool_use",
						"id": "toolu_123",
						"name": "submit_response",
						"input": {"answer": "42"
					}
				],
				"stop_reason": "tool_use"
			}`))
		}

		var target map[string]interface{}
		err := client.Ask(context.Background(), "test", &target)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to call Anthropic API")
	})

	t.Run("ask_empty_question", func(t *testing.T) {
		var target map[string]interface{}
		err := client.Ask(context.Background(), "", &target)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "question cannot be empty")
	})
}

func TestClaudeProvider_Chat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	cfg := chat.Config{
		Provider: chat.ProviderClaude,
		Token:    "test-key",
		BaseURL:  server.URL + "/",
	}

	client, err := chat.New(context.Background(), p, cfg)
	require.NoError(t, err)

	t.Run("success_text", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"id":    "msg_123",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-5-sonnet-20240620",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "Hello! How can I help you?",
					},
				},
				"stop_reason": "end_turn",
			}
			json.NewEncoder(w).Encode(resp)
		}

		resp, err := client.Chat(context.Background(), "Hi")
		assert.NoError(t, err)
		assert.Equal(t, "Hello! How can I help you?", resp)
	})

	t.Run("react_loop", func(t *testing.T) {
		step := 0
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var resp map[string]interface{}
			if step == 0 {
				// First response: tool use
				resp = map[string]interface{}{
					"id":    "msg_tool",
					"type":  "message",
					"role":  "assistant",
					"model": "claude-3-5-sonnet",
					"content": []map[string]interface{}{
						{
							"type": "tool_use",
							"id":   "toolu_1",
							"name": "get_weather",
							"input": map[string]interface{}{
								"location": "London",
							},
						},
					},
					"stop_reason": "tool_use",
				}
				step++
			} else {
				// Second response: final answer
				resp = map[string]interface{}{
					"id":    "msg_final",
					"type":  "message",
					"role":  "assistant",
					"model": "claude-3-5-sonnet",
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "The weather in London is sunny.",
						},
					},
					"stop_reason": "end_turn",
				}
			}
			json.NewEncoder(w).Encode(resp)
		}

		type weatherArgs struct {
			Location string `json:"location"`
		}
		err := client.SetTools([]chat.Tool{
			{
				Name:        "get_weather",
				Description: "Get weather for a location",
				Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
				Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
					return "sunny", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "What is the weather?")
		assert.NoError(t, err)
		assert.Equal(t, "The weather in London is sunny.", resp)
	})

	t.Run("chat_empty_prompt", func(t *testing.T) {
		resp, err := client.Chat(context.Background(), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
		assert.Empty(t, resp)
	})
}
