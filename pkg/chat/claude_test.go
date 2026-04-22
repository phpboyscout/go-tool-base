package chat_test

import (
	"context"
	"encoding/json"
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

func TestClaudeProvider_New(t *testing.T) {
	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("").Maybe()
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
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "Anthropic API key is required")
	})

	t.Run("success", func(t *testing.T) {
		cfg := chat.Config{
			Provider: chat.ProviderClaude,
			Token:    "test-key",
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("success_from_props", func(t *testing.T) {
		cfgMock := mockConfig.NewMockContainable(t)
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("")
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("")
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key")
		pWithKey := &props.Props{
			Logger: logger.NewNoop(),
			Config: cfgMock,
		}
		cfg := chat.Config{Provider: chat.ProviderClaude}
		client, err := chat.New(context.Background(), pWithKey, cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("success_from_env", func(t *testing.T) {
		t.Setenv(chat.EnvClaudeKey, "env-key")
		cfg := chat.Config{Provider: chat.ProviderClaude}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestClaudeProvider_Ask(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	cfg := chat.Config{
		Provider:             chat.ProviderClaude,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
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
			_ = json.NewEncoder(w).Encode(resp)
		}

		var target response
		err := client.Ask(context.Background(), "What is the answer?", &target)
		require.NoError(t, err)
		assert.Equal(t, "The answer is 42", target.Answer)
	})

	t.Run("no_tool_use_error", func(t *testing.T) {
		cfgWithSchema := chat.Config{
			Provider:             chat.ProviderClaude,
			Token:                "test-key",
			BaseURL:              server.URL + "/",
			AllowInsecureBaseURL: true,
			ResponseSchema:       map[string]interface{}{"type": "object"},
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
			_ = json.NewEncoder(w).Encode(resp)
		}

		var target map[string]interface{}
		err = clientWithSchema.Ask(context.Background(), "test", &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Claude did not provide a tool use response")
	})

	t.Run("with_response_schema", func(t *testing.T) {
		type response struct {
			Answer string `json:"answer"`
		}
		cfgWithSchema := chat.Config{
			Provider:             chat.ProviderClaude,
			Token:                "test-key",
			BaseURL:              server.URL + "/",
			AllowInsecureBaseURL: true,
			ResponseSchema:       chat.GenerateSchema[response](),
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
			_ = json.NewEncoder(w).Encode(resp)
		}

		var target response
		err = clientWithSchema.Ask(context.Background(), "test", &target)
		require.NoError(t, err)
		assert.Equal(t, "Structured 42", target.Answer)
	})

	t.Run("malformed_json_response", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Return tool_use but with malformed input (missing closing brace)
			_, _ = w.Write([]byte(`{
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to call Anthropic API")
	})

	t.Run("ask_empty_question", func(t *testing.T) {
		var target map[string]interface{}
		err := client.Ask(context.Background(), "", &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "question cannot be empty")
	})
}

func TestClaudeProvider_Chat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	cfg := chat.Config{
		Provider:             chat.ProviderClaude,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
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
			_ = json.NewEncoder(w).Encode(resp)
		}

		resp, err := client.Chat(context.Background(), "Hi")
		require.NoError(t, err)
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
			_ = json.NewEncoder(w).Encode(resp)
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
		require.NoError(t, err)
		assert.Equal(t, "The weather in London is sunny.", resp)
	})

	t.Run("chat_empty_prompt", func(t *testing.T) {
		resp, err := client.Chat(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
		assert.Empty(t, resp)
	})

	t.Run("max_steps_exceeded", func(t *testing.T) {
		maxStepsServer := NewMockServer()
		defer maxStepsServer.Close()

		maxStepsCfgMock := mockConfig.NewMockContainable(t)
		maxStepsCfgMock.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("").Maybe()
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("").Maybe()
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

		maxStepsProps := &props.Props{
			Logger: logger.NewNoop(),
			Config: maxStepsCfgMock,
		}

		maxStepsCfg := chat.Config{
			Provider:             chat.ProviderClaude,
			Token:                "test-key",
			BaseURL:              maxStepsServer.URL + "/",
			AllowInsecureBaseURL: true,
			MaxSteps:             2,
		}

		maxStepsClient, err := chat.New(context.Background(), maxStepsProps, maxStepsCfg)
		require.NoError(t, err)

		// Always respond with a tool call, never a final text answer.
		maxStepsServer.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"id":    "msg_loop",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-5-sonnet",
				"content": []map[string]interface{}{
					{
						"type": "tool_use",
						"id":   "toolu_loop",
						"name": "get_weather",
						"input": map[string]interface{}{
							"location": "London",
						},
					},
				},
				"stop_reason": "tool_use",
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		type weatherArgs struct {
			Location string `json:"location"`
		}
		err = maxStepsClient.SetTools([]chat.Tool{
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

		resp, err := maxStepsClient.Chat(context.Background(), "What is the weather?")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maximum ReAct steps")
		assert.Contains(t, err.Error(), "2")
		assert.Empty(t, resp)
	})

	t.Run("multiple_tool_calls_single_turn", func(t *testing.T) {
		multiServer := NewMockServer()
		defer multiServer.Close()

		multiCfgMock := mockConfig.NewMockContainable(t)
		multiCfgMock.EXPECT().GetString(chat.ConfigKeyClaudeEnv).Return("").Maybe()
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKeychain).Return("").Maybe()
		cfgMock.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("test-key").Maybe()

		multiProps := &props.Props{
			Logger: logger.NewNoop(),
			Config: multiCfgMock,
		}

		multiCfg := chat.Config{
			Provider:             chat.ProviderClaude,
			Token:                "test-key",
			BaseURL:              multiServer.URL + "/",
			AllowInsecureBaseURL: true,
		}

		freshClient, err := chat.New(context.Background(), multiProps, multiCfg)
		require.NoError(t, err)

		step := 0
		var capturedRequests []map[string]interface{}

		multiServer.Handler = func(w http.ResponseWriter, r *http.Request) {
			var reqBody map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			capturedRequests = append(capturedRequests, reqBody)

			w.Header().Set("Content-Type", "application/json")
			var resp map[string]interface{}
			if step == 0 {
				resp = map[string]interface{}{
					"id":    "msg_multi",
					"type":  "message",
					"role":  "assistant",
					"model": "claude-3-5-sonnet",
					"content": []map[string]interface{}{
						{
							"type": "tool_use",
							"id":   "toolu_a",
							"name": "get_weather",
							"input": map[string]interface{}{
								"location": "London",
							},
						},
						{
							"type": "tool_use",
							"id":   "toolu_b",
							"name": "get_time",
							"input": map[string]interface{}{
								"timezone": "UTC",
							},
						},
					},
					"stop_reason": "tool_use",
				}
				step++
			} else {
				resp = map[string]interface{}{
					"id":    "msg_final",
					"type":  "message",
					"role":  "assistant",
					"model": "claude-3-5-sonnet",
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "London is sunny and UTC time is 12:00.",
						},
					},
					"stop_reason": "end_turn",
				}
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		type weatherArgs struct {
			Location string `json:"location"`
		}
		type timeArgs struct {
			Timezone string `json:"timezone"`
		}

		weatherCalled := false
		timeCalled := false

		err = freshClient.SetTools([]chat.Tool{
			{
				Name:        "get_weather",
				Description: "Get weather for a location",
				Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
				Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
					weatherCalled = true
					return "sunny", nil
				},
			},
			{
				Name:        "get_time",
				Description: "Get time for a timezone",
				Parameters:  chat.GenerateSchema[timeArgs]().(*jsonschema.Schema),
				Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
					timeCalled = true
					return "12:00", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := freshClient.Chat(context.Background(), "What is the weather and time?")
		require.NoError(t, err)
		assert.Equal(t, "London is sunny and UTC time is 12:00.", resp)
		assert.True(t, weatherCalled, "get_weather tool should have been called")
		assert.True(t, timeCalled, "get_time tool should have been called")

		// The second request should contain both tool results.
		require.Len(t, capturedRequests, 2, "expected two API requests (tool call + final)")
		secondReqMessages, ok := capturedRequests[1]["messages"].([]interface{})
		require.True(t, ok)
		lastMsg := secondReqMessages[len(secondReqMessages)-1].(map[string]interface{})
		assert.Equal(t, "user", lastMsg["role"])
		toolResultContent := lastMsg["content"].([]interface{})
		assert.Len(t, toolResultContent, 2, "expected two tool results in the follow-up request")
	})
}
