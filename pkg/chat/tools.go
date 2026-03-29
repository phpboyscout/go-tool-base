package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// ToolCall represents a single tool invocation request.
type ToolCall struct {
	Name  string
	Input json.RawMessage
}

// ToolResult holds the result of a single tool execution.
type ToolResult struct {
	Name   string
	Result string
}

const defaultMaxParallelTools = 5

// executeToolsParallel executes multiple tool calls concurrently, bounded by
// maxConcurrency. Results are returned in the same order as the input calls.
// If maxConcurrency is zero or negative, it defaults to 5.
func executeToolsParallel(ctx context.Context, l logger.Logger, tools map[string]Tool, calls []ToolCall, maxConcurrency int) []ToolResult {
	if maxConcurrency <= 0 {
		maxConcurrency = defaultMaxParallelTools
	}

	results := make([]ToolResult, len(calls))
	sem := make(chan struct{}, maxConcurrency)

	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)

		sem <- struct{}{}

		go func(idx int, c ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()

			results[idx] = ToolResult{Name: c.Name, Result: executeTool(ctx, l, tools, c.Name, c.Input)}
		}(i, call)
	}

	wg.Wait()

	return results
}

// dispatchToolExecution runs tool calls sequentially or in parallel depending on
// parallelTools and the number of calls. It is the shared dispatch entry point
// for all provider ReAct loops.
func dispatchToolExecution(ctx context.Context, l logger.Logger, tools map[string]Tool, calls []ToolCall, parallelTools bool, maxParallelTools int) []ToolResult {
	if parallelTools && len(calls) > 1 {
		return executeToolsParallel(ctx, l, tools, calls, maxParallelTools)
	}

	results := make([]ToolResult, len(calls))

	for i, tc := range calls {
		results[i] = ToolResult{Name: tc.Name, Result: executeTool(ctx, l, tools, tc.Name, tc.Input)}
	}

	return results
}

// executeTool looks up a tool by name from the provided registry, executes it,
// and returns the result as a string. If the result is not a string, it is JSON
// marshalled. Errors at any stage are returned as formatted error strings suitable
// for feeding back into the AI conversation (matching existing provider behaviour
// where tool errors become conversation content rather than aborting the ReAct loop).
func executeTool(ctx context.Context, l logger.Logger, tools map[string]Tool, name string, input json.RawMessage) string {
	l.Info("Tool Call", "tool", name)
	l.Debug("Tool Parameters", "tool", name, "args", input)

	tool, ok := tools[name]
	if !ok {
		l.Warn("Tool not found", "tool", name)

		return fmt.Sprintf("Error: Tool %s not found", name)
	}

	out, err := tool.Handler(ctx, input)
	if err != nil {
		l.Warn("Tool execution failed", "tool", name, "error", err)

		return fmt.Sprintf("Error: %v", err)
	}

	if s, ok := out.(string); ok {
		l.Info("Tool executed successfully", "tool", name)

		return s
	}

	b, err := json.Marshal(out)
	if err != nil {
		l.Warn("Failed to marshal tool result", "tool", name, "error", err)

		return fmt.Sprintf("Error: failed to marshal tool result: %v", err)
	}

	l.Info("Tool executed successfully", "tool", name)

	return string(b)
}
