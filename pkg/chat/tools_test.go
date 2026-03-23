package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/stretchr/testify/assert"
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
