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
	"google.golang.org/genai"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

func TestGeminiProvider_New(t *testing.T) {
	t.Run("missing_api_key", func(t *testing.T) {
		t.Setenv(chat.EnvGeminiKey, "")
		cfg := chat.Config{
			Provider: chat.ProviderGemini,
			Token:    "",
		}
		_, err := newTestClient(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Gemini API key is required")
	})

	t.Run("success_initialization", func(t *testing.T) {
		cfg := chat.Config{
			Provider: chat.ProviderGemini,
			Token:    "test-key",
		}
		_, err := newTestClient(context.Background(), cfg)
		assert.NoError(t, err)
	})

	t.Run("success_from_env", func(t *testing.T) {
		t.Setenv(chat.EnvGeminiKey, "env-key")
		cfg := chat.Config{Provider: chat.ProviderGemini}
		_, err := newTestClient(context.Background(), cfg)
		assert.NoError(t, err)
	})

	t.Run("client_creation_error", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider: chat.ProviderGemini,
			Token:    "test-key",
			GenaiNewClient: func(ctx context.Context, config *genai.ClientConfig) (*genai.Client, error) {
				return nil, fmt.Errorf("simulated error")
			},
		}
		_, err := newTestClient(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create gemini client")
	})

	t.Run("genainewclient_wrong_type_returns_error_not_panic", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider: chat.ProviderGemini,
			Token:    "test-key",
			// A value that is non-nil but not the expected constructor signature.
			// The comma-ok assertion must turn this into a wrapped error rather
			// than panicking.
			GenaiNewClient: "definitely-not-a-constructor",
		}

		var (
			client chat.ChatClient
			err    error
		)
		require.NotPanics(t, func() {
			client, err = newTestClient(context.Background(), cfg)
		})
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "GenaiNewClient override has unexpected type")
	})
}

func TestGeminiProvider_Ask(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	cfg := chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	t.Run("success_structured", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"candidates": []map[string]interface{}{
					{
						"content": map[string]interface{}{
							"parts": []map[string]interface{}{
								{
									"text": `{"answer": "42"}`,
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		type response struct {
			Answer string `json:"answer"`
		}
		var target response
		err := client.Ask(context.Background(), "test", &target)
		require.NoError(t, err)
		assert.Equal(t, "42", target.Answer)
	})

	t.Run("with_config_options", func(t *testing.T) {
		type response struct {
			Result string `json:"result"`
		}
		cfgOptions := chat.Config{
			Provider:             chat.ProviderGemini,
			Token:                "test-key",
			BaseURL:              server.URL,
			AllowInsecureBaseURL: true,
			ResponseSchema:       chat.GenerateSchema[response](),
			MaxTokens:            100,
		}
		clientOptions, err := newTestClient(context.Background(), cfgOptions)
		require.NoError(t, err)

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"candidates": []map[string]interface{}{
					{
						"content": map[string]interface{}{
							"parts": []map[string]interface{}{
								{
									"text": `{"result": "ok"}`,
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		var target response
		err = clientOptions.Ask(context.Background(), "test", &target)
		require.NoError(t, err)
		assert.Equal(t, "ok", target.Result)
	})

	t.Run("api_error_bad_request", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": {"code": 400, "message": "Invalid request", "status": "INVALID_ARGUMENT"}}`))
		}

		var target map[string]interface{}
		err := client.Ask(context.Background(), "test", &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gemini send message failed")
	})

	t.Run("empty_question", func(t *testing.T) {
		var target map[string]interface{}
		err := client.Ask(context.Background(), "", &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "question cannot be empty")
	})
}

func TestGeminiProvider_Chat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	cfg := chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	t.Run("react_loop", func(t *testing.T) {
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
									{
										"text": "Checking weather...",
									},
									{
										"functionCall": map[string]interface{}{
											"name": "get_weather",
											"args": map[string]interface{}{
												"location": "Paris",
											},
										},
									},
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
									{
										"text": "The weather in Paris is rainy.",
									},
								},
							},
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
					return "rainy", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "Weather in Paris?")
		require.NoError(t, err)
		assert.Equal(t, "Checking weather...The weather in Paris is rainy.", resp)
	})

	t.Run("api_error_stream", func(t *testing.T) {
		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": {"code": 500, "message": "Internal error"}}`))
		}

		resp, err := client.Chat(context.Background(), "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gemini send message failed")
		assert.Empty(t, resp)
	})

	t.Run("empty_prompt", func(t *testing.T) {
		resp, err := client.Chat(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
		assert.Empty(t, resp)
	})

	t.Run("max_steps_exceeded", func(t *testing.T) {
		maxStepsServer := NewMockServer()
		defer maxStepsServer.Close()

		maxStepsCfg := chat.Config{
			Provider:             chat.ProviderGemini,
			Token:                "test-key",
			BaseURL:              maxStepsServer.URL,
			AllowInsecureBaseURL: true,
			MaxSteps:             2,
		}

		maxStepsClient, err := newTestClient(context.Background(), maxStepsCfg)
		require.NoError(t, err)

		// Always respond with a function call, never a final text answer.
		maxStepsServer.Handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"candidates": []map[string]interface{}{
					{
						"content": map[string]interface{}{
							"parts": []map[string]interface{}{
								{
									"functionCall": map[string]interface{}{
										"name": "get_weather",
										"args": map[string]interface{}{
											"location": "Paris",
										},
									},
								},
							},
						},
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
					return "rainy", nil
				},
			},
		})
		require.NoError(t, err)

		resp, err := maxStepsClient.Chat(context.Background(), "Weather in Paris?")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maximum ReAct steps")
		assert.Contains(t, err.Error(), "2")
		assert.Empty(t, resp)
	})
}

func TestGeminiProvider_Add(t *testing.T) {
	t.Parallel()
	cfg := chat.Config{
		Provider: chat.ProviderGemini,
		Token:    "test-key",
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		err := client.Add(context.Background(), "Hello")
		assert.NoError(t, err)
	})

	t.Run("empty_prompt", func(t *testing.T) {
		err := client.Add(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
	})
}

func TestGeminiProvider_AddThenChat(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	cfg := chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	t.Run("history_preserved_across_add_and_chat", func(t *testing.T) {
		var capturedBody map[string]interface{}

		server.Handler = func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)

			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"candidates": []map[string]interface{}{
					{
						"content": map[string]interface{}{
							"parts": []map[string]interface{}{
								{
									"text": "I remember your context. The answer is 42.",
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}

		// Buffer a message via Add, then send via Chat.
		err := client.Add(context.Background(), "Remember that my favourite number is 42.")
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "What is my favourite number?")
		require.NoError(t, err)
		assert.Equal(t, "I remember your context. The answer is 42.", resp)

		// Verify the request body contains history from Add().
		// The Gemini SDK sends "contents" which should include the
		// Add()-buffered message as history plus the Chat() prompt.
		contents, ok := capturedBody["contents"].([]interface{})
		require.True(t, ok, "request body should contain 'contents' array")
		require.GreaterOrEqual(t, len(contents), 2, "expected at least two content entries (Add + Chat)")

		// First content entry should be the Add() message.
		firstContent := contents[0].(map[string]interface{})
		firstParts := firstContent["parts"].([]interface{})
		firstPart := firstParts[0].(map[string]interface{})
		assert.Contains(t, firstPart["text"], "favourite number is 42")

		// Last content entry should be the Chat() message.
		lastContent := contents[len(contents)-1].(map[string]interface{})
		lastParts := lastContent["parts"].([]interface{})
		lastPart := lastParts[0].(map[string]interface{})
		assert.Contains(t, lastPart["text"], "favourite number")
	})
}

// TestGeminiProvider_MaxTokensWired proves Config.MaxTokens reaches the Gemini
// request as generationConfig.maxOutputTokens; it was previously ignored.
func TestGeminiProvider_MaxTokensWired(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	client, err := newTestClient(context.Background(), chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
		MaxTokens:            1234,
	})
	require.NoError(t, err)

	var captured float64

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if gc, ok := body["generationConfig"].(map[string]interface{}); ok {
			captured, _ = gc["maxOutputTokens"].(float64)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": "hi"}}}},
			},
		}
		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			t.Errorf("encode: %v", encErr)
		}
	}

	_, err = client.Chat(context.Background(), "hello")
	require.NoError(t, err)
	assert.InEpsilon(t, 1234, captured, 0.001, "Config.MaxTokens must be sent as maxOutputTokens")
}

// TestGeminiProvider_ChatPersistsHistoryAcrossCalls proves that turns from a
// Chat() call are written back into the conversation history, so a second
// Chat() remembers the first exchange. The provider previously created a
// throwaway session from g.history and never persisted the new turns, so
// multi-turn memory (and Save()) silently lost everything said via Chat().
func TestGeminiProvider_ChatPersistsHistoryAcrossCalls(t *testing.T) {
	t.Parallel()

	server := NewMockServer()
	defer server.Close()
	cfg := chat.Config{
		Provider:             chat.ProviderGemini,
		Token:                "test-key",
		BaseURL:              server.URL,
		AllowInsecureBaseURL: true,
	}

	client, err := newTestClient(context.Background(), cfg)
	require.NoError(t, err)

	call := 0

	var secondCallBody map[string]interface{}

	server.Handler = func(w http.ResponseWriter, r *http.Request) {
		if call == 1 {
			_ = json.NewDecoder(r.Body).Decode(&secondCallBody)
		}

		call++

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{"content": map[string]interface{}{"parts": []map[string]interface{}{{"text": "Nice to meet you, Alice."}}}},
			},
		}
		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			t.Errorf("encode response: %v", encErr)
		}
	}

	_, err = client.Chat(context.Background(), "My name is Alice.")
	require.NoError(t, err)

	_, err = client.Chat(context.Background(), "What is my name?")
	require.NoError(t, err)

	// The second request must carry the first exchange as history: the
	// user turn, the model reply, then the new user turn.
	contents, ok := secondCallBody["contents"].([]interface{})
	require.True(t, ok, "second request should contain a 'contents' array")
	require.GreaterOrEqual(t, len(contents), 3,
		"second Chat must include the prior user turn, model reply, and new prompt")

	first := contents[0].(map[string]interface{})["parts"].([]interface{})[0].(map[string]interface{})
	assert.Contains(t, first["text"], "My name is Alice")
}
