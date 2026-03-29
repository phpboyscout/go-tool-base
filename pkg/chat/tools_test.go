package chat

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func testLogger() logger.Logger {
	return logger.NewNoop()
}

func TestExecuteTool_Found(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"echo": {
			Name: "echo",
			Handler: func(_ context.Context, input json.RawMessage) (any, error) {
				return string(input), nil
			},
		},
	}

	result := executeTool(context.Background(), testLogger(), tools, "echo", json.RawMessage(`"hello"`))
	assert.Equal(t, `"hello"`, result)
}

func TestExecuteTool_NotFound(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{}
	result := executeTool(context.Background(), testLogger(), tools, "missing", nil)
	assert.Contains(t, result, "Tool missing not found")
}

func TestExecuteTool_HandlerError(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"fail": {
			Name: "fail",
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				return nil, assert.AnError
			},
		},
	}

	result := executeTool(context.Background(), testLogger(), tools, "fail", nil)
	assert.Contains(t, result, "Error:")
	assert.Contains(t, result, assert.AnError.Error())
}

func TestExecuteTool_NonStringResult(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"data": {
			Name: "data",
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				return map[string]string{"key": "value"}, nil
			},
		},
	}

	result := executeTool(context.Background(), testLogger(), tools, "data", nil)
	assert.JSONEq(t, `{"key":"value"}`, result)
}

func TestExecuteTool_MarshalError(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"bad": {
			Name: "bad",
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				return make(chan int), nil // channels can't be marshalled
			},
		},
	}

	result := executeTool(context.Background(), testLogger(), tools, "bad", nil)
	assert.Contains(t, result, "failed to marshal tool result")
}

func TestExecuteToolsParallel_SingleTool(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"echo": {Handler: func(_ context.Context, input json.RawMessage) (any, error) {
			return string(input), nil
		}},
	}

	calls := []ToolCall{{Name: "echo", Input: json.RawMessage(`"hi"`)}}
	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, 5)

	require.Len(t, results, 1)
	assert.Equal(t, "echo", results[0].Name)
	assert.Equal(t, `"hi"`, results[0].Result)
}

func TestExecuteToolsParallel_MultipleTools(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"a": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) { return "result-a", nil }},
		"b": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) { return "result-b", nil }},
		"c": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) { return "result-c", nil }},
	}

	calls := []ToolCall{
		{Name: "a", Input: nil},
		{Name: "b", Input: nil},
		{Name: "c", Input: nil},
	}
	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, 5)

	require.Len(t, results, 3)
	assert.Equal(t, "result-a", results[0].Result)
	assert.Equal(t, "result-b", results[1].Result)
	assert.Equal(t, "result-c", results[2].Result)
}

func TestExecuteToolsParallel_OrderPreserved(t *testing.T) {
	t.Parallel()

	// "slow" executes first but takes longer — result must still appear at index 0.
	tools := map[string]Tool{
		"slow": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			time.Sleep(50 * time.Millisecond)
			return "slow-result", nil
		}},
		"fast": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			return "fast-result", nil
		}},
	}

	calls := []ToolCall{
		{Name: "slow", Input: nil},
		{Name: "fast", Input: nil},
	}
	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, 5)

	require.Len(t, results, 2)
	assert.Equal(t, "slow-result", results[0].Result)
	assert.Equal(t, "fast-result", results[1].Result)
}

func TestExecuteToolsParallel_ConcurrencyBounded(t *testing.T) {
	t.Parallel()

	const maxConcurrency = 2
	const numTools = 5

	var current, peak atomic.Int64

	tools := make(map[string]Tool, numTools)
	calls := make([]ToolCall, numTools)

	for i := range numTools {
		name := string(rune('a' + i))
		tools[name] = Tool{
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				n := current.Add(1)
				if n > peak.Load() {
					peak.Store(n)
				}
				time.Sleep(20 * time.Millisecond)
				current.Add(-1)
				return "ok", nil
			},
		}
		calls[i] = ToolCall{Name: name}
	}

	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, maxConcurrency)

	require.Len(t, results, numTools)
	assert.LessOrEqual(t, peak.Load(), int64(maxConcurrency), "concurrent executions should not exceed maxConcurrency")
}

func TestExecuteToolsParallel_DefaultConcurrency(t *testing.T) {
	t.Parallel()

	// maxConcurrency=0 should default to 5 without panicking.
	tools := map[string]Tool{
		"a": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) { return "ok", nil }},
	}

	calls := []ToolCall{{Name: "a"}}
	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, 0)

	require.Len(t, results, 1)
	assert.Equal(t, "ok", results[0].Result)
}

func TestExecuteToolsParallel_ContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	tools := map[string]Tool{
		"blocking": {
			Handler: func(ctx context.Context, _ json.RawMessage) (any, error) {
				close(started)
				<-ctx.Done()
				return "", ctx.Err()
			},
		},
	}

	calls := []ToolCall{{Name: "blocking"}}

	done := make(chan []ToolResult, 1)
	go func() {
		done <- executeToolsParallel(ctx, testLogger(), tools, calls, 1)
	}()

	<-started
	cancel()

	select {
	case results := <-done:
		require.Len(t, results, 1)
		assert.Contains(t, results[0].Result, "context canceled")
	case <-time.After(2 * time.Second):
		t.Fatal("executeToolsParallel did not return after context cancellation")
	}
}

func TestExecuteToolsParallel_ToolError(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"ok":   {Handler: func(_ context.Context, _ json.RawMessage) (any, error) { return "good", nil }},
		"fail": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) { return nil, assert.AnError }},
	}

	calls := []ToolCall{
		{Name: "ok"},
		{Name: "fail"},
	}
	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, 5)

	require.Len(t, results, 2)
	assert.Equal(t, "good", results[0].Result)
	assert.Contains(t, results[1].Result, "Error:")
}
