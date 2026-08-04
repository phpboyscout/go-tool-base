package agent

import (
	"context"
	"encoding/json"

	"github.com/invopop/jsonschema"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"
)

// ErrQueryQuestionRequired is returned when the model calls query_user without
// a question.
var ErrQueryQuestionRequired = errors.NewSentinel("gtb.agent.query_question_required", "query_user requires a question")

// UserPrompter asks the user a question, offering the model's suggested
// answers, and returns the reply (a chosen suggestion or free text). It backs
// the query_user tool; the caller supplies an interactive implementation and
// omits the tool entirely in non-interactive runs.
type UserPrompter func(ctx context.Context, question string, suggestions []string) (string, error)

// QueryUserTool returns a tool that lets the model ask the human a clarifying
// question rather than guess. It is only registered for interactive runs; the
// prompter performs the actual interaction.
func QueryUserTool(prompter UserPrompter) gochat.Tool {
	return gochat.Tool{
		Name: "query_user",
		Description: "Ask the human a clarifying question and receive their answer. " +
			"Use this when the request is ambiguous, a design decision could reasonably " +
			"go more than one way, or a security-sensitive choice needs human judgement — " +
			"rather than guessing. Provide a concise question and 2-4 suggested answers; " +
			"the user can choose one or write their own.",
		Parameters: jsonschema.Reflect(struct {
			Question    string   `json:"question" jsonschema:"description=The clarifying question to ask the user"`
			Suggestions []string `json:"suggestions" jsonschema:"description=Two to four suggested answers; the user may also supply their own"`
		}{}),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var params struct {
				Question    string   `json:"question"`
				Suggestions []string `json:"suggestions"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, errors.Wrap(err, "invalid arguments")
			}

			if params.Question == "" {
				return nil, ErrQueryQuestionRequired
			}

			answer, err := prompter(ctx, params.Question, params.Suggestions)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get user answer")
			}

			return answer, nil
		},
	}
}
