package chat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

func TestOpenAIProvider_New(t *testing.T) {
	t.Run("missing_api_key", func(t *testing.T) {
		t.Setenv(chat.EnvOpenAIKey, "")
		cfg := chat.Config{
			Provider: chat.ProviderOpenAI,
			Token:    "",
		}
		_, err := newTestClient(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OpenAI token is required")
	})

	t.Run("compatible_missing_model", func(t *testing.T) {
		cfg := chat.Config{
			Provider: chat.ProviderOpenAICompatible,
			Token:    "test-key",
			Model:    "",
			BaseURL:  "https://api.openai.com/v1", // required for ProviderOpenAICompatible — without it we'd hit the BaseURL-required check first
		}
		_, err := newTestClient(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Model is required for ProviderOpenAICompatible")
	})

	t.Run("compatible_missing_baseurl", func(t *testing.T) {
		cfg := chat.Config{
			Provider: chat.ProviderOpenAICompatible,
			Token:    "test-key",
			Model:    "llama-3.1",
		}
		_, err := newTestClient(context.Background(), cfg)
		require.Error(t, err)
		require.ErrorIs(t, err, chat.ErrInvalidBaseURL)
	})

	t.Run("success_from_credentials", func(t *testing.T) {
		cfg := chat.Config{
			Provider:    chat.ProviderOpenAI,
			Credentials: chat.CredentialConfig{Key: "test-key"},
		}
		client, err := newTestClient(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("success_from_env", func(t *testing.T) {
		t.Setenv(chat.EnvOpenAIKey, "env-key")
		cfg := chat.Config{Provider: chat.ProviderOpenAI}
		client, err := newTestClient(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestOpenAIProvider_Ask(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	cfg := chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	t.Run("success_structured", func(t *testing.T) {
		type response struct {
			Result string `json:"result"`
		}

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"id": "chatcmpl-123",
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": `{"result": "success"}`,
						},
						"finish_reason": "stop",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		var target response
		err := client.Ask(context.Background(), "test", &target)
		require.NoError(t, err)
		assert.Equal(t, "success", target.Result)
	})

	t.Run("empty_question", func(t *testing.T) {
		var target map[string]interface{}
		err := client.Ask(context.Background(), "", &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "question cannot be empty")
	})

	t.Run("empty_choices_errors_not_panics", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{"id": "chatcmpl-empty", "choices": []map[string]interface{}{}}
			if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
				t.Errorf("encode: %v", encErr)
			}
		}

		var target map[string]interface{}
		// An OpenAI-compatible backend (Ollama/vLLM) can return 200 with no
		// choices; indexing Choices[0] must not panic.
		assert.NotPanics(t, func() {
			err := client.Ask(context.Background(), "test", &target)
			require.Error(t, err)
		})
	})
}

// TestOpenAIProvider_MaxTokensWired proves Config.MaxTokens reaches the request
// as max_completion_tokens; previously it was silently ignored by this provider.
func TestOpenAIProvider_MaxTokensWired(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	client, err := newTestClient(context.Background(), chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
		MaxTokens:            1234,
	})
	require.NoError(t, err)

	var capturedMaxTokens float64

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedMaxTokens, _ = body["max_completion_tokens"].(float64)

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"id":      "chatcmpl-mt",
			"choices": []map[string]interface{}{{"message": map[string]interface{}{"role": "assistant", "content": "hi"}, "finish_reason": "stop"}},
		}
		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			t.Errorf("encode: %v", encErr)
		}
	}

	_, err = client.Chat(context.Background(), "hello")
	require.NoError(t, err)
	assert.InEpsilon(t, 1234, capturedMaxTokens, 0.001, "Config.MaxTokens must be sent as max_completion_tokens")
}

func TestOpenAIProvider_Add(t *testing.T) {
	t.Parallel()
	cfg := chat.Config{
		Provider: chat.ProviderOpenAI,
		Token:    "test-key",
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	t.Run("empty_prompt", func(t *testing.T) {
		err := client.Add(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
	})

	t.Run("success", func(t *testing.T) {
		err := client.Add(context.Background(), "Hello")
		assert.NoError(t, err)
	})

	t.Run("success_with_credentials", func(t *testing.T) {
		cfgNoToken := chat.Config{
			Provider:    chat.ProviderOpenAI,
			Credentials: chat.CredentialConfig{Key: "test-key"},
		}
		clientWithConfig, err := newTestClient(context.Background(), cfgNoToken)
		require.NoError(t, err)

		err = clientWithConfig.Add(context.Background(), "Hello")
		assert.NoError(t, err)
	})

	t.Run("chunking", func(t *testing.T) {
		// Test with a very long prompt that should be chunked.
		// The ChatClient interface does not expose internal message
		// state, so this test can only verify that Add succeeds
		// without error. Detailed chunking logic is covered by the
		// internal TestChunkByTokens tests in openai_internal_test.go.
		longPrompt := ""
		for i := 0; i < 5000; i++ {
			longPrompt += "token "
		}
		err := client.Add(context.Background(), longPrompt)
		assert.NoError(t, err)
	})

	t.Run("set_tools_error", func(t *testing.T) {
		// Malformed tool
		err := client.SetTools([]chat.Tool{{Name: ""}})
		assert.Error(t, err)
	})
}

func TestOpenAIProvider_Chat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	cfg := chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	t.Run("success_text_no_tools", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"id": "chatcmpl-text",
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "Hello! How can I help you today?",
						},
						"finish_reason": "stop",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		resp, err := client.Chat(context.Background(), "Hi there")
		require.NoError(t, err)
		assert.Equal(t, "Hello! How can I help you today?", resp)
	})

	t.Run("react_loop", func(t *testing.T) {
		step := 0
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var resp map[string]interface{}
			if step == 0 {
				// First response: tool call
				resp = map[string]interface{}{
					"id": "chatcmpl-tool",
					"choices": []map[string]interface{}{
						{
							"message": map[string]interface{}{
								"role":    "assistant",
								"content": "Let me check the weather.",
								"tool_calls": []map[string]interface{}{
									{
										"id":   "call_1",
										"type": "function",
										"function": map[string]interface{}{
											"name":      "get_weather",
											"arguments": `{"location": "Berlin"}`,
										},
									},
								},
							},
							"finish_reason": "tool_calls",
						},
					},
				}
				step++
			} else {
				// Second response: final answer
				resp = map[string]interface{}{
					"id": "chatcmpl-final",
					"choices": []map[string]interface{}{
						{
							"message": map[string]interface{}{
								"role":    "assistant",
								"content": "The weather in Berlin is cloudy.",
							},
							"finish_reason": "stop",
						},
					},
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
				Description: "Get weather",
				Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
				Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
					return "cloudy", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "Weather in Berlin?")
		require.NoError(t, err)
		assert.Equal(t, "The weather in Berlin is cloudy.", resp)
	})

	t.Run("max_steps_exceeded", func(t *testing.T) {
		maxStepsServer := NewMockServer()
		defer maxStepsServer.Close()

		maxStepsCfg := chat.Config{
			Provider:             chat.ProviderOpenAI,
			Token:                "test-key",
			BaseURL:              maxStepsServer.URL + "/",
			AllowInsecureBaseURL: true,
			MaxSteps:             2,
		}

		maxStepsClient, err := newTestClient(context.Background(), maxStepsCfg)
		require.NoError(t, err)

		// Always respond with a tool call, never a final text answer.
		maxStepsServer.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"id": "chatcmpl-loop",
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_loop",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "get_weather",
										"arguments": `{"location": "Berlin"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		type weatherArgs struct {
			Location string `json:"location"`
		}
		err = maxStepsClient.SetTools([]chat.Tool{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  chat.GenerateSchema[weatherArgs]().(*jsonschema.Schema),
				Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
					return "cloudy", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := maxStepsClient.Chat(context.Background(), "Weather in Berlin?")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maximum ReAct steps")
		assert.Contains(t, err.Error(), "2")
		assert.Empty(t, resp)
	})
}
