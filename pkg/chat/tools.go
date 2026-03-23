package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/phpboyscout/gtb/pkg/logger"
)

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
