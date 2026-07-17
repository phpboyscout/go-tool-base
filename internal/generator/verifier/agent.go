package verifier

import (
	"context"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/internal/agent"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// AgentVerifier implements the Verifier interface using an autonomous agent loop.
var ErrVerificationFailed = errors.New("verification failed or incomplete")

// queryOtherValue is the sentinel select value that routes the user to a
// free-text answer in the query_user prompt.
const queryOtherValue = "\x00other"

type AgentVerifier struct {
	props *props.Props
	// allowQuery registers the query_user tool so the agent can ask the user
	// for clarification. Off for non-interactive runs (CI, --non-interactive).
	allowQuery bool
}

// NewAgentVerifier creates a new AgentVerifier. allowQuery enables the
// query_user tool (interactive runs only).
func NewAgentVerifier(p *props.Props, allowQuery bool) *AgentVerifier {
	return &AgentVerifier{
		props:      p,
		allowQuery: allowQuery,
	}
}

// huhUserPrompt asks the user the model's question via charm's huh, offering
// the suggested answers plus an "other" free-text option, and returns the
// reply. It backs the query_user tool for interactive runs.
func huhUserPrompt(_ context.Context, question string, suggestions []string) (string, error) {
	if len(suggestions) == 0 {
		return huhFreeText(question)
	}

	options := make([]huh.Option[string], 0, len(suggestions)+1)
	for _, s := range suggestions {
		options = append(options, huh.NewOption(s, s))
	}

	options = append(options, huh.NewOption("Other (type my own answer)", queryOtherValue))

	var choice string

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(question).
			Description("The generator agent is asking for a decision.").
			Options(options...).
			Value(&choice),
	))
	if err := form.Run(); err != nil {
		return "", errors.Wrap(err, "query_user prompt failed")
	}

	if choice != queryOtherValue {
		return choice, nil
	}

	return huhFreeText(question)
}

func huhFreeText(question string) (string, error) {
	var answer string

	form := huh.NewForm(huh.NewGroup(
		huh.NewText().
			Title(question).
			Description("Type your answer for the generator agent.").
			Value(&answer),
	))
	if err := form.Run(); err != nil {
		return "", errors.Wrap(err, "query_user prompt failed")
	}

	return answer, nil
}

// VerifyAndFix runs the agentic verification loop.
func (v *AgentVerifier) VerifyAndFix(ctx context.Context, projectRoot, cmdDir string, data *templates.CommandData, aiClient gochat.ChatClient, genFunc GeneratorFunc) error {
	v.props.Logger.Info("Starting autonomous agentic verification and repair loop...")

	// 1. Register tools
	tools := []gochat.Tool{
		agent.ReadFileTool(v.props.FS, projectRoot),
		agent.ReadFilesTool(v.props.FS, projectRoot),
		agent.WriteFileTool(v.props.FS, projectRoot),
		agent.ListDirTool(v.props.FS, projectRoot),
		agent.TreeTool(v.props.FS, projectRoot),
		agent.GoBuildTool(v.props.FS, projectRoot),
		agent.GoTestTool(v.props.FS, projectRoot),
		agent.GoGetTool(v.props.FS, projectRoot),
		agent.GoModTidyTool(v.props.FS, projectRoot),
		agent.LinterTool(v.props.FS, projectRoot),
	}

	// The query_user tool is only available in interactive runs; in CI or with
	// --non-interactive the agent must proceed without asking.
	if v.allowQuery {
		tools = append(tools, agent.QueryUserTool(huhUserPrompt))
	}

	if err := aiClient.SetTools(tools); err != nil {
		return errors.Wrap(err, "failed to set tools")
	}

	clarification := "Do not ask for user permission; proceed autonomously and make a sensible choice."
	if v.allowQuery {
		clarification = "Proceed autonomously, but when the request is ambiguous, a design decision could reasonably go more than one way, or a security-sensitive choice needs human judgement, call 'query_user' to ask the human rather than guessing."
	}

	// 2. Construct the system/initial prompt
	prompt := fmt.Sprintf(`You are an autonomous coding agent.
Your task is to verify and fix the Go project located at: %s

The project was just generated. Please:
1. Use 'tree' to see the whole project layout in a single call.
2. Run 'go_build', 'go_test' and 'golangci_lint' in the project directory to find build failures, test failures and lint issues. Run all three; a clean build and passing tests do not imply clean lint.
3. If there are missing dependencies, use 'go_get'.
4. If there are errors or lint issues, read the relevant files ('read_files' reads several at once), analyze the code, and overwrite them with fixes. Treat lint issues as real work to fix, not noise to ignore.
5. Repeat verification ONLY if changes were made or if errors persist. Do NOT re-run a check that already passed if no code changed.
6. Reply with "SUCCESS" only once 'go_build', 'go_test' AND 'golangci_lint' all pass with no errors and no reported issues. If you cannot fix it after 5 attempts at fixing the same error, reply with "FAILURE" and the reason.

Work efficiently: prefer 'tree' over repeated 'list_dir', and 'read_files' over many single 'read_file' calls, to minimise round-trips.

%s Use the provided tools. Start by calling 'tree'.

IMPORTANT: Do NOT add any "// Code generated" markers, auto-generated headers, or machine-generated notices to any file you write. Write only idiomatic, hand-authored Go code.`, cmdDir, clarification)

	// 3. Start Chat Loop
	// The client.Chat method handles the ReAct loop (executing tools until text response).
	resp, err := aiClient.Chat(ctx, prompt)
	if err != nil {
		return errors.Wrap(err, "agent chat failed")
	}

	// 4. Check result
	if strings.Contains(strings.ToUpper(resp), "SUCCESS") {
		return nil
	}

	return errors.Wrapf(ErrVerificationFailed, "%s", resp)
}
