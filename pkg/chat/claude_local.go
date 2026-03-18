package chat

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/phpboyscout/gtb/pkg/props"
)

func init() {
	RegisterProvider(ProviderClaudeLocal, newClaudeLocal)
}

// ClaudeLocal implements the ChatClient interface using a locally installed claude CLI binary.
// This provider is useful in environments where direct API access to api.anthropic.com is
// blocked but the pre-authenticated claude binary is permitted.
type ClaudeLocal struct {
	ctx       context.Context
	props     *props.Props
	cfg       Config
	sessionID string   // captured after first Chat/Ask call; used for --resume
	pending   []string // buffered Add() messages, prepended to next prompt
}

// newClaudeLocal initializes a new ClaudeLocal chat client.
// No API key is required — authentication is handled by the claude binary itself.
func newClaudeLocal(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return nil, errors.New(
			"claude binary not found in PATH: install Claude Code from https://claude.ai/download " +
				"and ensure it is authenticated before using ProviderClaudeLocal",
		)
	}

	return &ClaudeLocal{
		ctx:   ctx,
		props: p,
		cfg:   cfg,
	}, nil
}

// Add buffers a user message to be prepended to the next Chat or Ask call.
func (c *ClaudeLocal) Add(prompt string) error {
	c.pending = append(c.pending, prompt)

	return nil
}

// Ask sends a question to the local claude binary and unmarshals the structured response
// into the target using --json-schema for schema-enforced output.
func (c *ClaudeLocal) Ask(question string, target any) error {
	combined := c.buildPrompt(question)

	args := c.buildArgs(combined)

	if c.cfg.ResponseSchema != nil {
		schemaBytes, err := json.Marshal(c.cfg.ResponseSchema)
		if err != nil {
			return errors.Newf("failed to marshal response schema: %w", err)
		}

		args = append(args, "--json-schema", string(schemaBytes))
	}

	result, sessionID, err := c.runClaude(args)
	if err != nil {
		return err
	}

	if sessionID != "" {
		c.sessionID = sessionID
	}

	if err := json.Unmarshal([]byte(result), target); err != nil {
		return errors.Newf("failed to unmarshal claude response: %w", err)
	}

	return nil
}

// SetTools is not supported in Phase 1 of ProviderClaudeLocal.
// Tool integration via MCP server is planned for a future release.
func (c *ClaudeLocal) SetTools(_ []Tool) error {
	return errors.New(
		"ProviderClaudeLocal does not support SetTools in this version; " +
			"tool integration via MCP server is planned for a future release",
	)
}

// Chat sends a message to the local claude binary and returns the text response.
func (c *ClaudeLocal) Chat(ctx context.Context, prompt string) (string, error) {
	combined := c.buildPrompt(prompt)
	args := c.buildArgs(combined)

	result, sessionID, err := c.runClaude(args)
	if err != nil {
		return "", err
	}

	if sessionID != "" {
		c.sessionID = sessionID
	}

	return result, nil
}

// buildPrompt combines any buffered Add() messages with the current prompt and clears pending.
func (c *ClaudeLocal) buildPrompt(prompt string) string {
	if len(c.pending) == 0 {
		return prompt
	}

	parts := append(c.pending, prompt)
	c.pending = nil

	return strings.Join(parts, "\n\n---\n\n")
}

// buildArgs constructs the base argument list for a claude subprocess invocation.
func (c *ClaudeLocal) buildArgs(prompt string) []string {
	args := []string{"-p", prompt, "--output-format", "json"}

	if c.cfg.SystemPrompt != "" {
		args = append(args, "--system-prompt", c.cfg.SystemPrompt)
	}

	if c.cfg.Model != "" {
		args = append(args, "--model", c.cfg.Model)
	}

	if c.sessionID != "" {
		args = append(args, "--resume", c.sessionID)
	}

	return args
}

// claudeResult is the JSON structure returned by claude --output-format json.
type claudeResult struct {
	Type      string `json:"type"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
}

// runClaude executes the claude subprocess and returns the result text and session ID.
func (c *ClaudeLocal) runClaude(args []string) (result string, sessionID string, err error) {
	c.props.Logger.Debug("ClaudeLocal subprocess", "args", args)

	cmd := exec.CommandContext(c.ctx, "claude", args...)

	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		// Try to extract stderr for a better error message
		var exitErr *exec.ExitError
		if errors.As(cmdErr, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", "", errors.Newf("claude subprocess failed: %s", string(exitErr.Stderr))
		}

		return "", "", errors.Newf("claude subprocess failed: %w", cmdErr)
	}

	var res claudeResult
	if jsonErr := json.Unmarshal(out, &res); jsonErr != nil {
		return "", "", errors.Newf("failed to parse claude output: %w", jsonErr)
	}

	if res.IsError {
		return "", "", errors.Newf("claude returned an error: %s", res.Result)
	}

	c.props.Logger.Debug("ClaudeLocal response received", "session_id", res.SessionID)

	return res.Result, res.SessionID, nil
}
