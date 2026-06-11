package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryUserTool_ReturnsAnswer(t *testing.T) {
	t.Parallel()

	var (
		gotQuestion    string
		gotSuggestions []string
	)

	prompter := func(_ context.Context, question string, suggestions []string) (string, error) {
		gotQuestion = question
		gotSuggestions = suggestions

		return "embedded", nil
	}

	tool := QueryUserTool(prompter)
	args, err := json.Marshal(map[string]any{
		"question":    "Which trust-anchor source should the command default to?",
		"suggestions": []string{"embedded", "both"},
	})
	require.NoError(t, err)

	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)

	assert.Equal(t, "embedded", result)
	assert.Equal(t, "Which trust-anchor source should the command default to?", gotQuestion)
	assert.Equal(t, []string{"embedded", "both"}, gotSuggestions)
}

func TestQueryUserTool_RequiresQuestion(t *testing.T) {
	t.Parallel()

	tool := QueryUserTool(func(context.Context, string, []string) (string, error) {
		return "", nil
	})
	args, err := json.Marshal(map[string]any{"question": "", "suggestions": []string{}})
	require.NoError(t, err)

	_, err = tool.Handler(context.Background(), args)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrQueryQuestionRequired)
}

func TestQueryUserTool_PropagatesPrompterError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("user declined")
	tool := QueryUserTool(func(context.Context, string, []string) (string, error) {
		return "", sentinel
	})
	args, err := json.Marshal(map[string]any{"question": "proceed?"})
	require.NoError(t, err)

	_, err = tool.Handler(context.Background(), args)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}
