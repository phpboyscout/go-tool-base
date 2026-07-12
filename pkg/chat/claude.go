package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
)

func init() {
	RegisterProvider(ProviderClaude, newClaude)
}

// Claude implements the ChatClient interface using Anthropic's official Go SDK.
type Claude struct {
	usageTracker

	client     anthropic.Client
	logger     *slog.Logger
	messages   []anthropic.MessageParam
	cfg        Config
	system     []anthropic.TextBlockParam
	tools      map[string]Tool
	toolParams []anthropic.ToolUnionParam
}

// claudeUsage maps an Anthropic SDK Usage into the provider-neutral Usage.
func claudeUsage(u anthropic.Usage) Usage {
	return newUsage(
		int(u.InputTokens),
		int(u.OutputTokens),
		0, // Anthropic does not return a total; compute it.
		int(u.CacheReadInputTokens),
		0,
	)
}

// claudeDeltaUsage maps the cumulative usage carried on a streaming
// message_delta event into the provider-neutral Usage.
func claudeDeltaUsage(u anthropic.MessageDeltaUsage) Usage {
	return newUsage(
		int(u.InputTokens),
		int(u.OutputTokens),
		0,
		int(u.CacheReadInputTokens),
		0,
	)
}

// newClaude initializes a new Claude chat client.
func newClaude(ctx context.Context, settings Settings) (ChatClient, error) {
	cfg := settings.Config
	log := settings.logger()
	log.Info("Initialising Claude Chat")

	token := resolveAPIKey(
		ctx,
		cfg.Token,
		cfg.Credentials,
		EnvClaudeKey,
	)
	if token == "" {
		return nil, errors.New("Anthropic API key is required but not provided")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(token),
		option.WithHTTPClient(chatHTTPClient(cfg)),
	}

	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	client := anthropic.NewClient(opts...)

	// Resolve the default model once so every path (Ask, Chat, StreamChat)
	// sends a non-empty model; Chat/StreamChat previously sent the empty
	// Config.Model verbatim, which the API rejects with a 400.
	if cfg.Model == "" {
		cfg.Model = DefaultModelClaude
	}

	c := &Claude{
		logger: log,
		client: client,
		cfg:    cfg,
	}
	c.observer = cfg.UsageObserver

	// Carry the system prompt in the dedicated System field on every request
	// rather than smuggling it in as a user turn (audit:
	// claude-system-prompt-as-user-message). The API treats System with
	// higher priority and keeps the conversation history clean.
	if cfg.SystemPrompt != "" {
		c.system = []anthropic.TextBlockParam{{Text: cfg.SystemPrompt}}
	}

	return c, nil
}

// Add appends a new user message to the chat session.
// claudeUserMessage builds a user turn from the prompt text and any validated
// media, mapping each attachment to a base64 image block (spec §6; Claude v1
// accepts images).
func claudeUserMessage(prompt string, media []resolvedMedia) anthropic.MessageParam {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(media)+1)

	if prompt != "" {
		blocks = append(blocks, anthropic.NewTextBlock(prompt))
	}

	for _, m := range media {
		b64 := base64.StdEncoding.EncodeToString(m.Data)

		if m.MIMEType == mimePDF {
			blocks = append(blocks, anthropic.ContentBlockParamUnion{
				OfDocument: &anthropic.DocumentBlockParam{
					Source: anthropic.DocumentBlockParamSourceUnion{
						OfBase64: &anthropic.Base64PDFSourceParam{Data: b64},
					},
				},
			})

			continue
		}

		blocks = append(blocks, anthropic.NewImageBlockBase64(m.MIMEType, b64))
	}

	return anthropic.NewUserMessage(blocks...)
}

func (c *Claude) Add(_ context.Context, prompt string, media ...Media) error {
	if prompt == "" {
		return errors.New("prompt cannot be empty")
	}

	resolved, err := validateMediaSet(ProviderClaude, media)
	if err != nil {
		return err
	}

	c.messages = append(c.messages, claudeUserMessage(prompt, resolved))

	return nil
}

// Ask sends a question to the Claude chat client and expects a structured response.
func (c *Claude) Ask(ctx context.Context, question string, target any, media ...Media) error {
	if question == "" {
		return errors.New("question cannot be empty")
	}

	resolved, err := validateMediaSet(ProviderClaude, media)
	if err != nil {
		return err
	}

	c.messages = append(c.messages, claudeUserMessage(question, resolved))

	params := c.buildAskParams()

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return errors.Newf("failed to call Anthropic API: %w", err)
	}

	c.recordUsage(claudeUsage(resp.Usage))

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
		Model:     model,
		MaxTokens: int64(maxTokens),
		Messages:  c.messages,
		System:    c.system,
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

	// No schema: return the raw text content into target, which per the
	// ChatClient contract must be a *string or a json.Unmarshaler.
	return assignText(collectText(resp.Content), target)
}

// collectText concatenates the text of every text content block.
func collectText(content []anthropic.ContentBlockUnion) string {
	var b strings.Builder

	for _, block := range content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}

	return b.String()
}

// assignText writes raw text into target per the no-schema Ask contract:
// target must be a *string or a json.Unmarshaler; any other pointer is given a
// best-effort JSON unmarshal so a caller expecting structured text still works.
func assignText(text string, target any) error {
	switch t := target.(type) {
	case *string:
		*t = text

		return nil
	case json.Unmarshaler:
		if err := t.UnmarshalJSON([]byte(text)); err != nil {
			return errors.Newf("failed to unmarshal Claude text response: %w", err)
		}

		return nil
	default:
		if err := json.Unmarshal([]byte(text), target); err != nil {
			return errors.Newf("Ask without a schema requires a *string or json.Unmarshaler target: %w", err)
		}

		return nil
	}
}

// SetTools configures the tools available to the AI.
func (c *Claude) SetTools(tools []Tool) error {
	claudeTools := make([]anthropic.ToolUnionParam, 0, len(tools))

	for _, t := range tools {
		inputSchema := anthropic.ToolInputSchemaParam{Type: "object"}
		if t.Parameters != nil {
			inputSchema.Properties = t.Parameters.Properties
			inputSchema.Required = t.Parameters.Required
		}

		claudeTools = append(claudeTools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: inputSchema,
			},
		})
	}

	// Reset the response schema, handler map, and tool params together so a
	// second SetTools call fully replaces the first — no stale handlers or
	// tool params survive (audit: settools-accumulates-stale-handlers).
	c.cfg.ResponseSchema = nil

	c.tools = make(map[string]Tool, len(tools))
	for _, t := range tools {
		c.tools[t.Name] = t
	}

	c.toolParams = claudeTools

	return nil
}

// Chat sends a message and returns the response content.
func (c *Claude) Chat(ctx context.Context, prompt string, media ...Media) (string, error) {
	if err := c.Add(ctx, prompt, media...); err != nil {
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
		c.logger.Debug("Claude History State", "step", step)
		c.logHistory()

		params := anthropic.MessageNewParams{
			Model:     c.cfg.Model,
			MaxTokens: int64(maxTokens),
			Messages:  c.messages,
			System:    c.system,
			Tools:     c.toolParams,
		}

		resp, err := c.client.Messages.New(ctx, params)
		if err != nil {
			return "", errors.Newf("failed to call Anthropic API: %w", err)
		}

		c.recordUsage(claudeUsage(resp.Usage))

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

	toolResults := dispatchToolExecution(ctx, c.logger, c.tools, calls, c.cfg.ParallelTools, c.cfg.MaxParallelTools)
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
		c.logger.Debug("Turn", "idx", i, "role", m.Role)
	}
}

func (c *Claude) logContent(content []anthropic.ContentBlockUnion) {
	for _, b := range content {
		if b.Type == "text" {
			c.logger.Debug("Claude Reasoning", "text", b.Text)
		}
	}
}

// StreamChat implements StreamingChatClient.
func (c *Claude) StreamChat(ctx context.Context, prompt string, callback StreamCallback, media ...Media) (string, error) {
	if prompt == "" {
		return "", errors.New("prompt cannot be empty")
	}

	resolved, err := validateMediaSet(ProviderClaude, media)
	if err != nil {
		return "", err
	}

	c.messages = append(c.messages, claudeUserMessage(prompt, resolved))

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
		c.logger.Debug("Claude streaming step", "step", step)

		params := anthropic.MessageNewParams{
			Model:     c.cfg.Model,
			MaxTokens: int64(maxTokens),
			Messages:  c.messages,
			System:    c.system,
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

// args returns the accumulated tool-argument JSON, normalising an empty
// buffer to "{}". A streamed tool call with no input_json_delta events
// leaves argBuf empty; emitting "" downstream is invalid JSON and breaks
// both the assistant turn echoed back to the API and the handler's
// json.Unmarshal (audit: claude-stream-empty-tool-args-invalid-json).
func (t *claudePendingTool) args() string {
	if s := t.argBuf.String(); s != "" {
		return s
	}

	return "{}"
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

		if event.Type == "message_delta" {
			c.recordUsage(claudeDeltaUsage(event.Usage))
		}

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
		blocks = append(blocks, anthropic.NewToolUseBlock(t.id, json.RawMessage(t.args()), t.name))
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
		argStrings[i] = t.args()
		toolCalls[i] = ToolCall{Name: t.name, Input: json.RawMessage(argStrings[i])}
	}

	results := dispatchToolExecution(ctx, c.logger, c.tools, toolCalls, c.cfg.ParallelTools, c.cfg.MaxParallelTools)
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
		c.logger.Warn("Failed to marshal schema", "error", err)
	} else {
		c.logger.Debug("Claude Tool Schema", "schema", string(schemaBytes))
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

	// Rebuild the System field from the restored prompt. The prompt is no
	// longer carried inside c.messages (it lives in the dedicated System
	// field), so a restore must repopulate it explicitly or subsequent
	// requests would drop the system instruction.
	if snapshot.SystemPrompt != "" {
		c.system = []anthropic.TextBlockParam{{Text: snapshot.SystemPrompt}}
	} else {
		c.system = nil
	}

	return nil
}

// Compile-time check: Claude implements PersistentChatClient.
var _ PersistentChatClient = (*Claude)(nil)
