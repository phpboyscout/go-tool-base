package chat

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/internal/testutil"
)

func TestIntegration_ExecuteToolsParallel_TrueParallelism(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")

	t.Parallel()

	const sleepDuration = 100 * time.Millisecond

	tools := map[string]Tool{
		"a": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			time.Sleep(sleepDuration)
			return "a", nil
		}},
		"b": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			time.Sleep(sleepDuration)
			return "b", nil
		}},
		"c": {Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			time.Sleep(sleepDuration)
			return "c", nil
		}},
	}

	calls := []ToolCall{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}

	start := time.Now()
	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, 5)
	elapsed := time.Since(start)

	require.Len(t, results, 3)
	assert.Equal(t, "a", results[0].Result)
	assert.Equal(t, "b", results[1].Result)
	assert.Equal(t, "c", results[2].Result)

	// Parallel: should take ~100ms, not ~300ms (serial sum).
	// Allow generous 2× margin for CI variability.
	assert.Less(t, elapsed, 2*sleepDuration, "parallel execution should be faster than serial sum")
}

func TestIntegration_ExecuteToolsParallel_SemaphoreEnforced(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")

	t.Parallel()

	const maxConcurrency = 2
	const numTools = 6

	var current, peak atomic.Int64

	tools := make(map[string]Tool, numTools)
	calls := make([]ToolCall, numTools)

	for i := range numTools {
		name := string(rune('a' + i))
		tools[name] = Tool{
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				n := current.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(30 * time.Millisecond)
				current.Add(-1)

				return "ok", nil
			},
		}
		calls[i] = ToolCall{Name: name}
	}

	results := executeToolsParallel(context.Background(), testLogger(), tools, calls, maxConcurrency)

	require.Len(t, results, numTools)
	assert.LessOrEqual(t, peak.Load(), int64(maxConcurrency),
		"peak concurrency %d exceeded maxConcurrency %d", peak.Load(), maxConcurrency)
}

func TestIntegration_ExecuteToolsParallel_ContextCancellationPropagates(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")

	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{}, 3)
	tools := map[string]Tool{
		"blocking": {
			Handler: func(ctx context.Context, _ json.RawMessage) (any, error) {
				started <- struct{}{}
				<-ctx.Done()

				return "", ctx.Err()
			},
		},
	}

	calls := []ToolCall{
		{Name: "blocking"},
		{Name: "blocking"},
		{Name: "blocking"},
	}

	done := make(chan []ToolResult, 1)

	go func() {
		done <- executeToolsParallel(ctx, testLogger(), tools, calls, 3)
	}()

	// Wait for all three goroutines to be blocking.
	for range 3 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("tool goroutines did not start in time")
		}
	}

	cancel()

	select {
	case results := <-done:
		require.Len(t, results, 3)

		for _, r := range results {
			assert.Contains(t, r.Result, "context canceled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executeToolsParallel did not return after context cancellation")
	}
}
