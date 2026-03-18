package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
	"github.com/phpboyscout/gtb/pkg/props"
)

func init() {
	RegisterProvider(ProviderClaude, newClaude)
}

// Claude implements the ChatClient interface using Anthropic's official Go SDK.
type Claude struct {
	ctx        context.Context
	client     anthropic.Client
	props      *props.Props
	messages   []anthropic.MessageParam
	cfg        Config
	tools      map[string]Tool
	toolParams []anthropic.ToolUnionParam
}

// newClaude initializes a new Claude chat client.
func newClaude(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
	p.Logger.Info("Initialising Claude Chat")

	token := cfg.Token
	if token == "" {
		token = p.Config.GetString(ConfigKeyClaudeKey)
	}

	if token == "" {
		return nil, errors.New("Anthropic API key is required but not provided")
	}

	client := anthropic.NewClient(
		option.WithAPIKey(token),
	)

	c := &Claude{
		ctx:    ctx,
		props:  p,
		client: client,
		cfg:    cfg,
	}

	if cfg.SystemPrompt != "" {
		c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(cfg.SystemPrompt)))
	}

	return c, nil
}

// Add appends a new user message to the chat session.
func (c *Claude) Add(prompt string) error {
	c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)))

	return nil
}

// Ask sends a question to the Claude chat client and expects a structured response.
func (c *Claude) Ask(question string, target any) error {
	c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(question)))

	model := c.cfg.Model
	if model == "" {
		model = DefaultModelClaude
	}

	toolName := "submit_response"
	if c.cfg.SchemaName != "" {
		toolName = c.cfg.SchemaName
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(DefaultMaxTokensPerChunk),
		Messages:  c.messages,
	}

	if c.cfg.ResponseSchema != nil {
		c.applyResponseSchema(&params, toolName)
	}

	resp, err := c.client.Messages.New(c.ctx, params)
	if err != nil {
		return errors.Newf("failed to call Anthropic API: %w", err)
	}

	for _, content := range resp.Content {
		if content.Type == "tool_use" {
			err = json.Unmarshal(content.Input, target)
			if err != nil {
				return errors.Newf("failed to unmarshal Claude response: %w", err)
			}

			return nil
		}
	}

	if c.cfg.ResponseSchema != nil {
		return errors.New("Claude did not provide a tool use response as expected")
	}

	return nil
}

// SetTools configures the tools available to the AI.
func (c *Claude) SetTools(tools []Tool) error {
	claudeTools := make([]anthropic.ToolUnionParam, 0, len(tools))

	for _, t := range tools {
		inputSchema := anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: t.Parameters.Properties,
			Required:   t.Parameters.Required,
		}

		claudeTools = append(claudeTools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: inputSchema,
			},
		})
	}

	c.cfg.ResponseSchema = nil

	if c.tools == nil {
		c.tools = make(map[string]Tool)
	}

	for _, t := range tools {
		c.tools[t.Name] = t
	}

	c.toolParams = claudeTools

	return nil
}

// Chat sends a message and returns the response content.
func (c *Claude) Chat(ctx context.Context, prompt string) (string, error) {
	if err := c.Add(prompt); err != nil {
		return "", err
	}

	const maxSteps = 20
	for step := range maxSteps {
		c.props.Logger.Debug("Claude History State", "step", step)
		c.logHistory()

		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(c.cfg.Model),
			MaxTokens: int64(DefaultMaxTokensPerChunk),
			Messages:  c.messages,
			Tools:     c.toolParams,
		}

		resp, err := c.client.Messages.New(ctx, params)
		if err != nil {
			return "", errors.Newf("failed to call Anthropic API: %w", err)
		}

		c.messages = append(c.messages, anthropic.NewAssistantMessage(resContentToBlocks(resp.Content)...))

		c.logContent(resp.Content)

		var toolResults []anthropic.ContentBlockParamUnion

		hasToolUse := false

		var fullText strings.Builder

		for _, content := range resp.Content {
			switch content.Type {
			case "tool_use":
				hasToolUse = true
				result := c.executeTool(ctx, content.Name, content.Input)
				toolResults = append(toolResults, anthropic.NewToolResultBlock(content.ID, result, false))
			case "text":
				fullText.WriteString(content.Text)
			}
		}

		if hasToolUse {
			c.messages = append(c.messages, anthropic.NewUserMessage(toolResults...))

			continue
		}

		return fullText.String(), nil
	}

	return "", errors.New("Claude reached maximum ReAct steps (20) without a final answer")
}

func (c *Claude) executeTool(ctx context.Context, toolName string, input json.RawMessage) string {
	c.props.Logger.Info("Claude Tool Call", "tool", toolName)
	c.props.Logger.Debug("Claude Tool Parameters", "tool", toolName, "args", input)

	tool, ok := c.tools[toolName]
	if !ok {
		return fmt.Sprintf("Error: Tool %s not found", toolName)
	}

	out, err := tool.Handler(ctx, input)
	if err != nil {
		c.props.Logger.Warn("Tool execution failed", "tool", toolName, "error", err)

		return fmt.Sprintf("Error: %v", err)
	}

	if s, ok := out.(string); ok {
		return s
	}

	b, err := json.Marshal(out)
	if err != nil {
		c.props.Logger.Warn("Failed to marshal tool result", "tool", toolName, "error", err)

		return fmt.Sprintf("Error: failed to marshal tool result: %v", err)
	}

	return string(b)
}

func resContentToBlocks(content []anthropic.ContentBlockUnion) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	for _, c := range content {
		switch c.Type {
		case "text":
			blocks = append(blocks, anthropic.NewTextBlock(c.Text))
		case "tool_use":
			blocks = append(blocks, anthropic.NewToolUseBlock(c.ID, c.Input, c.Name))
		}
	}

	return blocks
}

func (c *Claude) logHistory() {
	for i, m := range c.messages {
		c.props.Logger.Debug("Turn", "idx", i, "role", m.Role)
	}
}

func (c *Claude) logContent(content []anthropic.ContentBlockUnion) {
	for _, b := range content {
		if b.Type == "text" {
			c.props.Logger.Info("Claude Reasoning", "text", b.Text)
		}
	}
}

func (c *Claude) applyResponseSchema(params *anthropic.MessageNewParams, toolName string) {
	var inputSchema anthropic.ToolInputSchemaParam

	if schema, ok := c.cfg.ResponseSchema.(*jsonschema.Schema); ok {
		inputSchema = anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: schema.Properties,
			Required:   schema.Required,
		}
	} else {
		inputSchema = anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: c.cfg.ResponseSchema,
		}
	}

	params.Tools = []anthropic.ToolUnionParam{
		{
			OfTool: &anthropic.ToolParam{
				Name:        toolName,
				Description: anthropic.String(c.cfg.SchemaDescription),
				InputSchema: inputSchema,
			},
		},
	}
	params.ToolChoice = anthropic.ToolChoiceUnionParam{
		OfTool: &anthropic.ToolChoiceToolParam{
			Type: "tool",
			Name: toolName,
		},
	}

	schemaBytes, err := json.Marshal(inputSchema)
	if err != nil {
		c.props.Logger.Warn("Failed to marshal schema", "error", err)
	} else {
		c.props.Logger.Debug("Claude Tool Schema", "schema", string(schemaBytes))
	}
}
