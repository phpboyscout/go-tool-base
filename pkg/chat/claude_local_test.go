package chat_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/internal/exectest"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestClaudeLocal_New(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Logger: logger.NewNoop(),
	}

	t.Run("binary_not_found", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.MissingLookPath(),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "claude binary not found in PATH")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestClaudeLocal_Add(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop()}
	cfg := chat.Config{
		Provider:     chat.ProviderClaudeLocal,
		ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
	}
	client, err := chat.New(context.Background(), p, cfg)
	require.NoError(t, err)

	t.Run("empty_prompt", func(t *testing.T) {
		t.Parallel()

		err := client.Add(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		err := client.Add(context.Background(), "Hello")
		assert.NoError(t, err)
	})
}

func TestClaudeLocal_Chat(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Logger: logger.NewNoop(),
	}

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand:  exectest.EchoCommand(`{"type": "message", "result": "Local response", "session_id": "session_123", "is_error": false}`),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "Hello")
		require.NoError(t, err)
		assert.Equal(t, "Local response", resp)
	})

	t.Run("claude_error", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand:  exectest.EchoCommand(`{"type": "error", "result": "something went wrong", "is_error": true}`),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "Hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "claude returned an error: something went wrong")
		assert.Empty(t, resp)
	})

	t.Run("subprocess_failure", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand:  exectest.FailCommand(),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "Hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "claude subprocess failed")
		assert.Empty(t, resp)
	})

	t.Run("invalid_json_output", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand:  exectest.EchoCommand(`invalid json`),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "Hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse claude output")
		assert.Empty(t, resp)
	})

	t.Run("add_pending", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				for i, arg := range args {
					if arg == "-p" && i+1 < len(args) {
						assert.Contains(t, args[i+1], "Buffered message")
						assert.Contains(t, args[i+1], "Actual chat")
					}
				}
				return exec.Command("echo", `{"type": "message", "result": "Buffered response", "is_error": false}`)
			},
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		err = client.Add(context.Background(), "Buffered message")
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "Actual chat")
		require.NoError(t, err)
		assert.Equal(t, "Buffered response", resp)
	})

	t.Run("chat_empty_prompt", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		resp, err := client.Chat(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
		assert.Empty(t, resp)
	})
}

func TestClaudeLocal_Ask(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Logger: logger.NewNoop(),
	}

	t.Run("success_structured", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand:  exectest.EchoCommand(`{"type": "message", "result": "{\"answer\": \"42\"}", "session_id": "session_123", "is_error": false}`),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		type response struct {
			Answer string `json:"answer"`
		}
		var target response
		err = client.Ask(context.Background(), "What is the answer?", &target)
		require.NoError(t, err)
		assert.Equal(t, "42", target.Answer)
	})

	t.Run("with_schema", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:       chat.ProviderClaudeLocal,
			ExecLookPath:   exectest.FakeLookPath("/usr/local/bin/claude"),
			ResponseSchema: map[string]interface{}{"type": "object"},
			ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				argsStr := fmt.Sprintf("%v", args)
				assert.Contains(t, argsStr, "--json-schema")
				return exec.Command("echo", `{"type": "message", "result": "{}", "is_error": false}`)
			},
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		var target map[string]interface{}
		err = client.Ask(context.Background(), "test", &target)
		assert.NoError(t, err)
	})

	t.Run("ask_empty_question", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		var target map[string]interface{}
		err = client.Ask(context.Background(), "", &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "question cannot be empty")
	})

	t.Run("add_pending", func(t *testing.T) {
		t.Parallel()

		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				for i, arg := range args {
					if arg == "-p" && i+1 < len(args) {
						assert.Contains(t, args[i+1], "Buffered message")
						assert.Contains(t, args[i+1], "Actual question")
					}
				}
				return exec.Command("echo", `{"type": "message", "result": "{}", "is_error": false}`)
			},
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		err = client.Add(context.Background(), "Buffered message")
		require.NoError(t, err)

		var target map[string]interface{}
		err = client.Ask(context.Background(), "Actual question", &target)
		assert.NoError(t, err)
	})

	t.Run("with_optional_args", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		cfg := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			SystemPrompt: "Be helpful",
			Model:        "claude-custom",
			ExecLookPath: exectest.FakeLookPath("/usr/local/bin/claude"),
			ExecCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				argsStr := fmt.Sprintf("%v", args)
				assert.Contains(t, argsStr, "--system-prompt")
				assert.Contains(t, argsStr, "Be helpful")
				assert.Contains(t, argsStr, "--model")
				assert.Contains(t, argsStr, "claude-custom")

				if callCount == 1 {
					assert.Contains(t, argsStr, "--resume")
					assert.Contains(t, argsStr, "session_123")
				} else {
					assert.NotContains(t, argsStr, "--resume")
				}

				callCount++
				return exec.Command("echo", `{"type": "message", "result": "{}", "session_id": "session_123", "is_error": false}`)
			},
		}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)

		var target map[string]interface{}
		err = client.Ask(context.Background(), "test 1", &target)
		require.NoError(t, err)
		err = client.Ask(context.Background(), "test 2", &target)
		assert.NoError(t, err)
	})
}
