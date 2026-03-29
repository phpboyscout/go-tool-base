package chat

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"

	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func init() {
	RegisterProvider(ProviderClaude, newClaude)
}

// Claude implements the ChatClient interface using Anthropic's official Go SDK.
type Claude struct {
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
	if token == "" && p.Config != nil {
		token = p.Config.GetString(ConfigKeyClaudeKey)
	}

	if token == "" {
		token = os.Getenv(EnvClaudeKey)
	}

	if token == "" {
		return nil, errors.New("Anthropic API key is required but not provided")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(token),
		option.WithHTTPClient(gtbhttp.NewClient()),
	}

	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	client := anthropic.NewClient(opts...)

	c := &Claude{
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
func (c *Claude) Add(_ context.Context, prompt string) error {
	if prompt == "" {
		return errors.New("prompt cannot be empty")
	}

	c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)))

	return nil
}

// Ask sends a question to the Claude chat client and expects a structured response.
func (c *Claude) Ask(ctx context.Context, question string, target any) error {
	if question == "" {
		return errors.New("question cannot be empty")
	}

	c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(question)))

	params := c.buildAskParams()

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return errors.Newf("failed to call Anthropic API: %w", err)
	}

	return c.parseAskResponse(resp, target)
}

func (c *Claude) buildAskParams() anthropic.MessageNewParams {
	model := c.cfg.Model
	if model == "" {
		model = DefaultModelClaude
	}

	toolName := "submit_response"
	if c.cfg.SchemaName != "" {
		toolName = c.cfg.SchemaName
	}

	maxTokens := c.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokensClaude
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		Messages:  c.messages,
	}

	if c.cfg.ResponseSchema != nil {
		c.applyResponseSchema(&params, toolName)
	}

	return params
}

func (c *Claude) parseAskResponse(resp *anthropic.Message, target any) error {
	for _, content := range resp.Content {
		if content.Type == "tool_use" {
			err := json.Unmarshal(content.Input, target)
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
	if err := c.Add(ctx, prompt); err != nil {
		return "", err
	}

	maxSteps := c.cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	maxTokens := c.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokensClaude
	}

	for step := range maxSteps {
		c.props.Logger.Debug("Claude History State", "step", step)
		c.logHistory()

		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(c.cfg.Model),
			MaxTokens: int64(maxTokens),
			Messages:  c.messages,
			Tools:     c.toolParams,
		}

		resp, err := c.client.Messages.New(ctx, params)
		if err != nil {
			return "", errors.Newf("failed to call Anthropic API: %w", err)
		}

		c.messages = append(c.messages, anthropic.NewAssistantMessage(resContentToBlocks(resp.Content)...))

		c.logContent(resp.Content)

		var fullText strings.Builder

		var toolUses []anthropic.ContentBlockUnion

		for _, content := range resp.Content {
			switch content.Type {
			case "tool_use":
				toolUses = append(toolUses, content)
			case "text":
				fullText.WriteString(content.Text)
			}
		}

		if len(toolUses) > 0 {
			c.messages = append(c.messages, anthropic.NewUserMessage(c.processToolUses(ctx, toolUses)...))

			continue
		}

		return fullText.String(), nil
	}

	return "", errors.Newf("Claude reached maximum ReAct steps (%d) without a final answer", maxSteps)
}

func (c *Claude) processToolUses(ctx context.Context, toolUses []anthropic.ContentBlockUnion) []anthropic.ContentBlockParamUnion {
	results := make([]anthropic.ContentBlockParamUnion, 0, len(toolUses))

	if c.cfg.ParallelTools && len(toolUses) > 1 {
		calls := make([]ToolCall, len(toolUses))
		for i, tu := range toolUses {
			calls[i] = ToolCall{Name: tu.Name, Input: tu.Input}
		}

		for i, r := range executeToolsParallel(ctx, c.props.Logger, c.tools, calls, c.cfg.MaxParallelTools) {
			results = append(results, anthropic.NewToolResultBlock(toolUses[i].ID, r.Result, false))
		}
	} else {
		for _, tu := range toolUses {
			result := executeTool(ctx, c.props.Logger, c.tools, tu.Name, tu.Input)
			results = append(results, anthropic.NewToolResultBlock(tu.ID, result, false))
		}
	}

	return results
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
			c.props.Logger.Debug("Claude Reasoning", "text", b.Text)
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
