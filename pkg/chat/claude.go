package chat

import (
	"context"
	"encoding/json"
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

	token := resolveAPIKey(cfg.Token, p.Config, ConfigKeyClaudeEnv, ConfigKeyClaudeKey, EnvClaudeKey)
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
	calls := make([]ToolCall, len(toolUses))
	for i, tu := range toolUses {
		calls[i] = ToolCall{Name: tu.Name, Input: tu.Input}
	}

	toolResults := dispatchToolExecution(ctx, c.props.Logger, c.tools, calls, c.cfg.ParallelTools, c.cfg.MaxParallelTools)
	results := make([]anthropic.ContentBlockParamUnion, len(toolUses))

	for i, r := range toolResults {
		results[i] = anthropic.NewToolResultBlock(toolUses[i].ID, r.Result, false)
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

// StreamChat implements StreamingChatClient.
func (c *Claude) StreamChat(ctx context.Context, prompt string, callback StreamCallback) (string, error) {
	if prompt == "" {
		return "", errors.New("prompt cannot be empty")
	}

	c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)))

	maxSteps := c.cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	maxTokens := c.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokensClaude
	}

	var fullText strings.Builder

	for step := range maxSteps {
		c.props.Logger.Debug("Claude streaming step", "step", step)

		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(c.cfg.Model),
			MaxTokens: int64(maxTokens),
			Messages:  c.messages,
			Tools:     c.toolParams,
		}

		stepResult, err := c.streamClaudeStep(ctx, params, callback, &fullText)
		if err != nil {
			return fullText.String(), err
		}

		if len(stepResult.tools) > 0 {
			c.messages = append(c.messages, c.buildClaudeStreamAssistantMsg(stepResult.stepText, stepResult.tools))

			toolResults, execErr := c.execClaudeStreamTools(ctx, stepResult.tools, callback)
			if execErr != nil {
				return fullText.String(), execErr
			}

			c.messages = append(c.messages, anthropic.NewUserMessage(toolResults...))

			continue
		}

		c.messages = append(c.messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(stepResult.stepText)))
		_ = callback(StreamEvent{Type: EventComplete})

		return fullText.String(), nil
	}

	return "", errors.Newf("Claude reached maximum ReAct steps (%d) without a final answer", maxSteps)
}

type claudeStreamResult struct {
	stepText string
	tools    []*claudePendingTool
}

type claudePendingTool struct {
	id     string
	name   string
	argBuf strings.Builder
}

func (c *Claude) streamClaudeStep(
	ctx context.Context,
	params anthropic.MessageNewParams,
	callback StreamCallback,
	fullText *strings.Builder,
) (*claudeStreamResult, error) {
	stream := c.client.Messages.NewStreaming(ctx, params)

	var stepText strings.Builder

	tools := make(map[int64]*claudePendingTool)

	var toolOrder []int64

	for stream.Next() {
		event := stream.Current()

		if err := c.processClaudeStreamEvent(event, tools, &toolOrder, &stepText, fullText, callback); err != nil {
			return nil, err
		}
	}

	if err := stream.Err(); err != nil {
		return nil, errors.Wrap(err, "claude stream error")
	}

	orderedTools := make([]*claudePendingTool, len(toolOrder))

	for i, idx := range toolOrder {
		orderedTools[i] = tools[idx]
	}

	return &claudeStreamResult{stepText: stepText.String(), tools: orderedTools}, nil
}

func (c *Claude) processClaudeStreamEvent(
	event anthropic.MessageStreamEventUnion,
	tools map[int64]*claudePendingTool,
	toolOrder *[]int64,
	stepText, fullText *strings.Builder,
	callback StreamCallback,
) error {
	switch event.Type {
	case "content_block_start":
		return c.handleClaudeBlockStart(event, tools, toolOrder, callback)
	case "content_block_delta":
		return c.handleClaudeBlockDelta(event, tools, stepText, fullText, callback)
	}

	return nil
}

func (c *Claude) handleClaudeBlockStart(
	event anthropic.MessageStreamEventUnion,
	tools map[int64]*claudePendingTool,
	toolOrder *[]int64,
	callback StreamCallback,
) error {
	cb := event.ContentBlock
	if cb.Type != "tool_use" {
		return nil
	}

	tools[event.Index] = &claudePendingTool{id: cb.ID, name: cb.Name}
	*toolOrder = append(*toolOrder, event.Index)

	return callback(StreamEvent{
		Type:     EventToolCallStart,
		ToolCall: &StreamToolCall{ID: cb.ID, Name: cb.Name},
	})
}

func (c *Claude) handleClaudeBlockDelta(
	event anthropic.MessageStreamEventUnion,
	tools map[int64]*claudePendingTool,
	stepText, fullText *strings.Builder,
	callback StreamCallback,
) error {
	switch event.Delta.Type {
	case "text_delta":
		text := event.Delta.Text
		stepText.WriteString(text)
		fullText.WriteString(text)

		return callback(StreamEvent{Type: EventTextDelta, Delta: text})
	case "input_json_delta":
		if t, ok := tools[event.Index]; ok {
			t.argBuf.WriteString(event.Delta.PartialJSON)
		}
	}

	return nil
}

func (c *Claude) buildClaudeStreamAssistantMsg(stepText string, tools []*claudePendingTool) anthropic.MessageParam {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(tools)+1)

	if stepText != "" {
		blocks = append(blocks, anthropic.NewTextBlock(stepText))
	}

	for _, t := range tools {
		blocks = append(blocks, anthropic.NewToolUseBlock(t.id, json.RawMessage(t.argBuf.String()), t.name))
	}

	return anthropic.NewAssistantMessage(blocks...)
}

func (c *Claude) execClaudeStreamTools(
	ctx context.Context,
	tools []*claudePendingTool,
	callback StreamCallback,
) ([]anthropic.ContentBlockParamUnion, error) {
	toolCalls := make([]ToolCall, len(tools))
	argStrings := make([]string, len(tools))

	for i, t := range tools {
		argStrings[i] = t.argBuf.String()
		toolCalls[i] = ToolCall{Name: t.name, Input: json.RawMessage(argStrings[i])}
	}

	results := dispatchToolExecution(ctx, c.props.Logger, c.tools, toolCalls, c.cfg.ParallelTools, c.cfg.MaxParallelTools)
	resultBlocks := make([]anthropic.ContentBlockParamUnion, len(tools))

	for i, r := range results {
		if err := callback(StreamEvent{
			Type: EventToolCallEnd,
			ToolCall: &StreamToolCall{
				ID:        tools[i].id,
				Name:      tools[i].name,
				Arguments: argStrings[i],
				Result:    r.Result,
			},
		}); err != nil {
			return nil, err
		}

		resultBlocks[i] = anthropic.NewToolResultBlock(tools[i].id, r.Result, false)
	}

	return resultBlocks, nil
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

// Save captures the current Claude conversation state as a snapshot.
func (c *Claude) Save() (*Snapshot, error) {
	messages, err := json.Marshal(c.messages)
	if err != nil {
		return nil, errors.Wrap(err, "marshalling Claude messages")
	}

	return NewSnapshot(ProviderClaude, c.cfg.Model, c.cfg.SystemPrompt, messages, c.tools, nil), nil
}

// Restore replaces the current conversation state with a previously saved snapshot.
func (c *Claude) Restore(snapshot *Snapshot) error {
	if snapshot.Provider != ProviderClaude {
		return errors.Newf("provider mismatch: snapshot is %s, client is claude", snapshot.Provider)
	}

	var messages []anthropic.MessageParam
	if err := json.Unmarshal(snapshot.Messages, &messages); err != nil {
		return errors.Wrap(err, "unmarshalling Claude messages")
	}

	c.messages = messages
	c.cfg.SystemPrompt = snapshot.SystemPrompt

	return nil
}

// Compile-time check: Claude implements PersistentChatClient.
var _ PersistentChatClient = (*Claude)(nil)
