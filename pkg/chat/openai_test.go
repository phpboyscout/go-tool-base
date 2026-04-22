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

func TestOpenAIProvider_New(t *testing.T) {
	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	t.Run("missing_api_key", func(t *testing.T) {
		t.Setenv(chat.EnvOpenAIKey, "")
		cfg := chat.Config{
			Provider: chat.ProviderOpenAI,
			Token:    "",
		}
		_, err := chat.New(context.Background(), p, cfg)
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
		_, err := chat.New(context.Background(), p, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Model is required for ProviderOpenAICompatible")
	})

	t.Run("compatible_missing_baseurl", func(t *testing.T) {
		cfg := chat.Config{
			Provider: chat.ProviderOpenAICompatible,
			Token:    "test-key",
			Model:    "llama-3.1",
		}
		_, err := chat.New(context.Background(), p, cfg)
		require.Error(t, err)
		require.ErrorIs(t, err, chat.ErrInvalidBaseURL)
	})

	t.Run("success_from_props", func(t *testing.T) {
		cfgMockInternal := mockConfig.NewMockContainable(t)
		cfgMockInternal.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("")
		cfgMockInternal.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("")
		cfgMockInternal.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key")
		pWithKey := &props.Props{
			Logger: logger.NewNoop(),
			Config: cfgMockInternal,
		}
		cfg := chat.Config{Provider: chat.ProviderOpenAI}
		client, err := chat.New(context.Background(), pWithKey, cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("success_from_env", func(t *testing.T) {
		t.Setenv(chat.EnvOpenAIKey, "env-key")
		cfg := chat.Config{Provider: chat.ProviderOpenAI}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestOpenAIProvider_Ask(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	cfg := chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	}

	client, err := chat.New(context.Background(), p, cfg)
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
}
func TestOpenAIProvider_Add(t *testing.T) {
	t.Parallel()

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	cfg := chat.Config{
		Provider: chat.ProviderOpenAI,
		Token:    "test-key",
	}

	client, err := chat.New(context.Background(), p, cfg)
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

	t.Run("success_with_config", func(t *testing.T) {
		// New client with nil token but valid config
		cfgNoToken := chat.Config{Provider: chat.ProviderOpenAI}
		clientWithConfig, _ := chat.New(context.Background(), p, cfgNoToken)
		err := clientWithConfig.Add(context.Background(), "Hello")
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

	cfgMock := mockConfig.NewMockContainable(t)
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("").Maybe()
	cfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfgMock,
	}

	cfg := chat.Config{
		Provider:             chat.ProviderOpenAI,
		Token:                "test-key",
		BaseURL:              server.URL + "/",
		AllowInsecureBaseURL: true,
	}

	client, err := chat.New(context.Background(), p, cfg)
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

		maxStepsCfgMock := mockConfig.NewMockContainable(t)
		maxStepsCfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIEnv).Return("").Maybe()
		maxStepsCfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKeychain).Return("").Maybe()
		maxStepsCfgMock.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("test-key").Maybe()

		maxStepsProps := &props.Props{
			Logger: logger.NewNoop(),
			Config: maxStepsCfgMock,
		}

		maxStepsCfg := chat.Config{
			Provider:             chat.ProviderOpenAI,
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
