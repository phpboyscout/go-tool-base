package chat_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestClaudeLocal_New(t *testing.T) {
	oldLookPath := chat.ExportExecLookPath
	defer func() { chat.ExportExecLookPath = oldLookPath }()

	p := &props.Props{
		Logger: logger.NewNoop(),
	}

	t.Run("binary_not_found", func(t *testing.T) {
		chat.ExportExecLookPath = func(file string) (string, error) {
			return "", fmt.Errorf("not found")
		}
		cfg := chat.Config{Provider: chat.ProviderClaudeLocal}
		client, err := chat.New(context.Background(), p, cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "claude binary not found in PATH")
	})

	t.Run("success", func(t *testing.T) {
		chat.ExportExecLookPath = func(file string) (string, error) {
			return "/usr/local/bin/claude", nil
		}
		cfg := chat.Config{Provider: chat.ProviderClaudeLocal}
		client, err := chat.New(context.Background(), p, cfg)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestClaudeLocal_Add(t *testing.T) {
	oldLookPath := chat.ExportExecLookPath
	defer func() { chat.ExportExecLookPath = oldLookPath }()

	chat.ExportExecLookPath = func(file string) (string, error) {
		return "/usr/local/bin/claude", nil
	}

	p := &props.Props{Logger: logger.NewNoop()}
	cfg := chat.Config{Provider: chat.ProviderClaudeLocal}
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
}

func TestClaudeLocal_Chat(t *testing.T) {
	oldLookPath := chat.ExportExecLookPath
	oldExec := chat.ExportExecCommand
	defer func() {
		chat.ExportExecLookPath = oldLookPath
		chat.ExportExecCommand = oldExec
	}()

	chat.ExportExecLookPath = func(file string) (string, error) {
		return "/usr/local/bin/claude", nil
	}

	p := &props.Props{
		Logger: logger.NewNoop(),
	}

	cfg := chat.Config{Provider: chat.ProviderClaudeLocal}
	client, err := chat.New(context.Background(), p, cfg)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Mock successful claude output
			return exec.Command("echo", `{"type": "message", "result": "Local response", "session_id": "session_123", "is_error": false}`)
		}

		resp, err := client.Chat(context.Background(), "Hello")
		require.NoError(t, err)
		assert.Equal(t, "Local response", resp)
	})

	t.Run("claude_error", func(t *testing.T) {
		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("echo", `{"type": "error", "result": "something went wrong", "is_error": true}`)
		}

		resp, err := client.Chat(context.Background(), "Hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "claude returned an error: something went wrong")
		assert.Empty(t, resp)
	})

	t.Run("subprocess_failure", func(t *testing.T) {
		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("false")
		}

		resp, err := client.Chat(context.Background(), "Hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "claude subprocess failed")
		assert.Empty(t, resp)
	})

	t.Run("invalid_json_output", func(t *testing.T) {
		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("echo", `invalid json`)
		}

		resp, err := client.Chat(context.Background(), "Hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse claude output")
		assert.Empty(t, resp)
	})

	t.Run("add_pending", func(t *testing.T) {
		err := client.Add(context.Background(), "Buffered message")
		require.NoError(t, err)

		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Find the prompt argument (-p)
			for i, arg := range args {
				if arg == "-p" && i+1 < len(args) {
					assert.Contains(t, args[i+1], "Buffered message")
					assert.Contains(t, args[i+1], "Actual chat")
				}
			}
			return exec.Command("echo", `{"type": "message", "result": "Buffered response", "is_error": false}`)
		}

		resp, err := client.Chat(context.Background(), "Actual chat")
		require.NoError(t, err)
		assert.Equal(t, "Buffered response", resp)
	})

	t.Run("chat_empty_prompt", func(t *testing.T) {
		resp, err := client.Chat(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt cannot be empty")
		assert.Empty(t, resp)
	})
}

func TestClaudeLocal_Ask(t *testing.T) {
	oldLookPath := chat.ExportExecLookPath
	oldExec := chat.ExportExecCommand
	defer func() {
		chat.ExportExecLookPath = oldLookPath
		chat.ExportExecCommand = oldExec
	}()

	chat.ExportExecLookPath = func(file string) (string, error) {
		return "/usr/local/bin/claude", nil
	}

	p := &props.Props{
		Logger: logger.NewNoop(),
	}

	cfg := chat.Config{Provider: chat.ProviderClaudeLocal}
	client, err := chat.New(context.Background(), p, cfg)
	require.NoError(t, err)

	t.Run("success_structured", func(t *testing.T) {
		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("echo", `{"type": "message", "result": "{\"answer\": \"42\"}", "session_id": "session_123", "is_error": false}`)
		}

		type response struct {
			Answer string `json:"answer"`
		}
		var target response
		err := client.Ask(context.Background(), "What is the answer?", &target)
		require.NoError(t, err)
		assert.Equal(t, "42", target.Answer)
	})

	t.Run("with_schema", func(t *testing.T) {
		cfgSchema := chat.Config{
			Provider:       chat.ProviderClaudeLocal,
			ResponseSchema: map[string]interface{}{"type": "object"},
		}
		clientSchema, _ := chat.New(context.Background(), p, cfgSchema)

		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			argsStr := fmt.Sprintf("%v", args)
			assert.Contains(t, argsStr, "--json-schema")
			return exec.Command("echo", `{"type": "message", "result": "{}", "is_error": false}`)
		}

		var target map[string]interface{}
		err := clientSchema.Ask(context.Background(), "test", &target)
		assert.NoError(t, err)
	})

	t.Run("ask_empty_question", func(t *testing.T) {
		var target map[string]interface{}
		err := client.Ask(context.Background(), "", &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "question cannot be empty")
	})

	t.Run("add_pending", func(t *testing.T) {
		err := client.Add(context.Background(), "Buffered message")
		require.NoError(t, err)

		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Find the prompt argument (-p)
			for i, arg := range args {
				if arg == "-p" && i+1 < len(args) {
					assert.Contains(t, args[i+1], "Buffered message")
					assert.Contains(t, args[i+1], "Actual question")
				}
			}
			return exec.Command("echo", `{"type": "message", "result": "{}", "is_error": false}`)
		}

		var target map[string]interface{}
		err = client.Ask(context.Background(), "Actual question", &target)
		assert.NoError(t, err)
	})

	t.Run("with_optional_args", func(t *testing.T) {
		cfgFull := chat.Config{
			Provider:     chat.ProviderClaudeLocal,
			SystemPrompt: "Be helpful",
			Model:        "claude-custom",
		}
		clientFull, _ := chat.New(context.Background(), p, cfgFull)

		callCount := 0
		chat.ExportExecCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
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
		}

		// First call sets sessionID
		var target map[string]interface{}
		err := clientFull.Ask(context.Background(), "test 1", &target)
		require.NoError(t, err)
		// Second call uses sessionID
		err = clientFull.Ask(context.Background(), "test 2", &target)
		assert.NoError(t, err)
	})
}
