package verifier

import (
	"context"
	"fmt"
	"strings"

	"github.com/phpboyscout/gtb/internal/agent"
	"github.com/phpboyscout/gtb/internal/generator/templates"
	"github.com/phpboyscout/gtb/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/props"
)

// AgentVerifier implements the Verifier interface using an autonomous agent loop.
var ErrVerificationFailed = fmt.Errorf("verification failed or incomplete")

type AgentVerifier struct {
	props *props.Props
}

// NewAgentVerifier creates a new AgentVerifier.
func NewAgentVerifier(p *props.Props) *AgentVerifier {
	return &AgentVerifier{
		props: p,
	}
}

// VerifyAndFix runs the agentic verification loop.
func (v *AgentVerifier) VerifyAndFix(ctx context.Context, projectRoot, cmdDir string, data *templates.CommandData, aiClient chat.ChatClient, genFunc GeneratorFunc) error {
	v.props.Logger.Info("Starting autonomous agentic verification and repair loop...")

	// 1. Register tools
	tools := []chat.Tool{
		agent.ReadFileTool(projectRoot),
		agent.WriteFileTool(projectRoot),
		agent.ListDirTool(projectRoot),
		agent.GoBuildTool(projectRoot),
		agent.GoTestTool(projectRoot),
		agent.GoGetTool(projectRoot),
		agent.GoModTidyTool(projectRoot),
		agent.LinterTool(projectRoot),
	}

	if err := aiClient.SetTools(tools); err != nil {
		return fmt.Errorf("failed to set tools: %w", err)
	}

	// 2. Construct the system/initial prompt
	prompt := fmt.Sprintf(`You are an autonomous coding agent.
Your task is to verify and fix the Go project located at: %s

The project was just generated. Please:
1. List the files to understand the structure.
2. Run 'go_build' and 'go_test' in the project directory to identify issues.
3. If there are missing dependencies, use 'go_get'.
4. If there are lint issues, use 'golangci_lint'.
5. If there are errors, read the relevant files, analyze the code, and overwrite them with fixes.
6. Repeat verification ONLY if changes were made or if errors persist. Do NOT re-run builds or tests if they already succeeded and no code was changed.
7. When the project builds successfully and tests pass, reply with "SUCCESS". If you cannot fix it after 5 attempts at fixing the same error, reply with "FAILURE" and the reason.

Do not ask for user permission. Use the provided tools. Start by listing the directory.

IMPORTANT: Do NOT add any "// Code generated" markers, auto-generated headers, or machine-generated notices to any file you write. Write only idiomatic, hand-authored Go code.`, cmdDir)

	// 3. Start Chat Loop
	// The client.Chat method handles the ReAct loop (executing tools until text response).
	resp, err := aiClient.Chat(ctx, prompt)
	if err != nil {
		return fmt.Errorf("agent chat failed: %w", err)
	}

	// 4. Check result
	if strings.Contains(strings.ToUpper(resp), "SUCCESS") {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrVerificationFailed, resp)
}
