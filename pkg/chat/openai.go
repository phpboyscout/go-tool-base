package chat

import (
	"context"
	"encoding/json"
	"os"

	"strings"

	"github.com/cockroachdb/errors"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/tiktoken-go/tokenizer"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func init() {
	RegisterProvider(ProviderOpenAI, newOpenAI)
	RegisterProvider(ProviderOpenAICompatible, newOpenAI)
}

// OpenAI implements the ChatClient interface for interacting with OpenAI's API
// and any OpenAI-compatible API endpoint.
type OpenAI struct {
	oai    openai.Client
	params openai.ChatCompletionNewParams
	logger logger.Logger
	config config.Containable
	cfg    Config
	tools  map[string]Tool
}

// newOpenAI initializes a new OpenAI (or OpenAI-compatible) chat client.
func newOpenAI(ctx context.Context, props *props.Props, cfg Config) (ChatClient, error) {
	props.Logger.Info("Initialising OpenAI")

	if cfg.Provider == ProviderOpenAICompatible && cfg.Model == "" {
		return nil, errors.New("Model is required for ProviderOpenAICompatible: specify the model name for your backend (e.g. \"llama3.2\" for Ollama)")
	}

	token, err := getOpenAICredentials(cfg.Token, props.Config)
	if err != nil {
		return nil, errors.Newf("failed to get OpenAI credentials: %w", err)
	}

	if token == "" {
		return nil, errors.New("OpenAI token is required but not provided")
	}

	props.Logger.Debug("Initialising OpenAI client")

	clientOpts := []option.RequestOption{
		option.WithAPIKey(token),
		option.WithHTTPClient(gtbhttp.NewClient()),
	}
	if cfg.BaseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
	}

	client := openai.NewClient(clientOpts...)

	props.Logger.Debug("Using setup prompt", "prompt", cfg.SystemPrompt)

	setup := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(cfg.SystemPrompt),
	}

	model := cfg.Model
	if model == "" {
		model = DefaultModelOpenAI
	}

	params := openai.ChatCompletionNewParams{
		Model:    model,
		Messages: setup,
		Seed:     openai.Int(0),
	}

	if cfg.ResponseSchema != nil {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        cfg.SchemaName,
					Description: openai.String(cfg.SchemaDescription),
					Schema:      cfg.ResponseSchema,
					Strict:      openai.Bool(true),
				},
			},
		}
	}

	return &OpenAI{
		config: props.Config,
		logger: props.Logger,
		oai:    client,
		cfg:    cfg,
		params: params,
	}, nil
}

// Add appends a new user message to the chat session.
func (a *OpenAI) Add(_ context.Context, prompt string) error {
	if prompt == "" {
		return errors.New("prompt cannot be empty")
	}

	msgs, err := chunkByTokens(prompt, DefaultMaxTokensOpenAI, a.params.Model)
	if err != nil {
		return err
	}

	if len(msgs) == 0 {
		return errors.New("no messages to add after tokenization")
	}

	for i, msg := range msgs {
		if msg == "" {
			continue
		}

		a.logger.Debug("Adding prompt to OpenAI chat", "prompt", msgs[i])
		a.params.Messages = append(a.params.Messages, openai.UserMessage(msgs[i]))
	}

	return nil
}

// Ask sends a question to the OpenAI chat client and expects a structured response
// which is unmarshalled into the target interface.
func (a *OpenAI) Ask(ctx context.Context, question string, target any) error {
	if question == "" {
		return errors.New("question cannot be empty")
	}

	a.params.Messages = append(a.params.Messages, openai.UserMessage(question))

	res, err := a.oai.Chat.Completions.New(ctx, a.params)
	if err != nil {
		return errors.Wrap(err, "AI completion request failed")
	}

	a.params.Messages = append(a.params.Messages, res.Choices[0].Message.ToParam())

	err = json.Unmarshal([]byte(res.Choices[0].Message.Content), target)
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal analysis response")
	}

	return nil
}

func getOpenAICredentials(token string, cfg config.Containable) (string, error) {
	if token != "" {
		return token, nil
	}

	if cfg != nil {
		if token = cfg.GetString(ConfigKeyOpenAIKey); token != "" {
			return token, nil
		}
	}

	if envToken := os.Getenv(EnvOpenAIKey); envToken != "" {
		return envToken, nil
	}

	return "", errors.New("OpenAI token is required but not provided")
}

func chunkByTokens(text string, maxTokens int, model string) ([]string, error) {
	if maxTokens <= 0 {
		return []string{}, nil
	}

	if text == "" {
		return []string{""}, nil
	}

	enc, err := tokenizer.ForModel(tokenizer.Model(model))
	if err != nil {
		// Unknown model name (e.g. from an OpenAI-compatible backend) — fall back to cl100k_base.
		enc, err = tokenizer.Get(tokenizer.Cl100kBase)
		if err != nil {
			return nil, errors.Newf("failed to get fallback tokenizer: %w", err)
		}
	}

	tokens, _, err := enc.Encode(text)
	if err != nil {
		return nil, errors.Newf("failed to encode text: %w", err)
	}

	if len(tokens) <= maxTokens {
		return []string{text}, nil
	}

	chunks, err := splitAndDecodeTokens(enc, tokens, maxTokens)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 && text != "" {
		chunks = []string{text}
	}

	return chunks, nil
}

func splitAndDecodeTokens(enc tokenizer.Codec, tokens []uint, maxTokens int) ([]string, error) {
	var chunks []string

	for i := 0; i < len(tokens); i += maxTokens {
		end := min(i+maxTokens, len(tokens))

		decoded, err := enc.Decode(tokens[i:end])
		if err != nil {
			return nil, errors.Newf("failed to decode tokens: %w", err)
		}

		if decoded != "" {
			chunks = append(chunks, decoded)
		}
	}

	return chunks, nil
}

func (a *OpenAI) appendToolResults(ctx context.Context, toolCalls []openai.ChatCompletionMessageToolCallUnion) {
	calls := make([]ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		calls[i] = ToolCall{Name: tc.Function.Name, Input: json.RawMessage(tc.Function.Arguments)}
	}

	for i, r := range dispatchToolExecution(ctx, a.logger, a.tools, calls, a.cfg.ParallelTools, a.cfg.MaxParallelTools) {
		a.params.Messages = append(a.params.Messages, openai.ToolMessage(r.Result, toolCalls[i].ID))
	}
}

// SetTools configures the tools available to the AI.
func (a *OpenAI) SetTools(tools []Tool) error {
	oaiTools := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))

	for _, t := range tools {
		if t.Parameters == nil {
			return errors.Newf("tool %s parameters cannot be nil", t.Name)
		}

		params := map[string]any{
			"type":       "object",
			"properties": t.Parameters.Properties,
			"required":   t.Parameters.Required,
		}

		oaiTools = append(oaiTools, openai.ChatCompletionFunctionTool(
			openai.FunctionDefinitionParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters:  openai.FunctionParameters(params),
			},
		))
	}

	a.params.Tools = oaiTools

	if a.tools == nil {
		a.tools = make(map[string]Tool)
	}

	for _, t := range tools {
		a.tools[t.Name] = t
	}

	return nil
}

// StreamChat implements StreamingChatClient.
func (a *OpenAI) StreamChat(ctx context.Context, prompt string, callback StreamCallback) (string, error) {
	if err := a.Add(ctx, prompt); err != nil {
		return "", err
	}

	a.params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{}

	maxSteps := a.cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	var fullText strings.Builder

	for step := range maxSteps {
		a.logger.Debug("OpenAI streaming step", "step", step)

		stepResult, err := a.streamOpenAIStep(ctx, callback, &fullText)
		if err != nil {
			return fullText.String(), err
		}

		if len(stepResult.tools) > 0 {
			a.appendStreamOpenAIAssistantMsg(stepResult.stepText, stepResult.tools)

			if execErr := a.execOpenAIStreamTools(ctx, stepResult.tools, callback); execErr != nil {
				return fullText.String(), execErr
			}

			continue
		}

		a.params.Messages = append(a.params.Messages, openai.AssistantMessage(stepResult.stepText))
		_ = callback(StreamEvent{Type: EventComplete})

		return fullText.String(), nil
	}

	return "", errors.Newf("OpenAI reached maximum ReAct steps (%d) without a final answer", maxSteps)
}

type openaiStreamResult struct {
	stepText string
	tools    []*openaiPendingTool
}

type openaiPendingTool struct {
	id   string
	name string
	args strings.Builder
}

func (a *OpenAI) streamOpenAIStep(
	ctx context.Context,
	callback StreamCallback,
	fullText *strings.Builder,
) (*openaiStreamResult, error) {
	stream := a.oai.Chat.Completions.NewStreaming(ctx, a.params)

	var stepText strings.Builder

	tools := make(map[int64]*openaiPendingTool)

	var toolOrder []int64

	for stream.Next() {
		chunk := stream.Current()

		for _, choice := range chunk.Choices {
			if err := a.handleOpenAIChoice(choice, tools, &toolOrder, &stepText, fullText, callback); err != nil {
				return nil, err
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, errors.Wrap(err, "openai stream error")
	}

	orderedTools := make([]*openaiPendingTool, len(toolOrder))

	for i, idx := range toolOrder {
		orderedTools[i] = tools[idx]
	}

	return &openaiStreamResult{stepText: stepText.String(), tools: orderedTools}, nil
}

func (a *OpenAI) handleOpenAIChoice(
	choice openai.ChatCompletionChunkChoice,
	tools map[int64]*openaiPendingTool,
	toolOrder *[]int64,
	stepText, fullText *strings.Builder,
	callback StreamCallback,
) error {
	if content := choice.Delta.Content; content != "" {
		stepText.WriteString(content)
		fullText.WriteString(content)

		if err := callback(StreamEvent{Type: EventTextDelta, Delta: content}); err != nil {
			return err
		}
	}

	for _, tc := range choice.Delta.ToolCalls {
		if err := a.handleOpenAIToolCallDelta(tc, tools, toolOrder, callback); err != nil {
			return err
		}
	}

	return nil
}

func (a *OpenAI) handleOpenAIToolCallDelta(
	tc openai.ChatCompletionChunkChoiceDeltaToolCall,
	tools map[int64]*openaiPendingTool,
	toolOrder *[]int64,
	callback StreamCallback,
) error {
	if _, exists := tools[tc.Index]; !exists {
		tools[tc.Index] = &openaiPendingTool{id: tc.ID, name: tc.Function.Name}
		*toolOrder = append(*toolOrder, tc.Index)

		if err := callback(StreamEvent{
			Type:     EventToolCallStart,
			ToolCall: &StreamToolCall{ID: tc.ID, Name: tc.Function.Name},
		}); err != nil {
			return err
		}
	}

	tools[tc.Index].args.WriteString(tc.Function.Arguments)

	return nil
}

func (a *OpenAI) appendStreamOpenAIAssistantMsg(stepText string, tools []*openaiPendingTool) {
	toolCallParams := make([]openai.ChatCompletionMessageToolCallUnionParam, len(tools))

	for i, t := range tools {
		toolCallParams[i] = openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: t.id,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      t.name,
					Arguments: t.args.String(),
				},
			},
		}
	}

	asst := &openai.ChatCompletionAssistantMessageParam{
		ToolCalls: toolCallParams,
	}

	if stepText != "" {
		asst.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
			OfString: openai.String(stepText),
		}
	}

	a.params.Messages = append(a.params.Messages, openai.ChatCompletionMessageParamUnion{OfAssistant: asst})
}

func (a *OpenAI) execOpenAIStreamTools(ctx context.Context, tools []*openaiPendingTool, callback StreamCallback) error {
	toolCalls := make([]ToolCall, len(tools))
	argStrings := make([]string, len(tools))

	for i, t := range tools {
		argStrings[i] = t.args.String()
		toolCalls[i] = ToolCall{Name: t.name, Input: json.RawMessage(argStrings[i])}
	}

	results := dispatchToolExecution(ctx, a.logger, a.tools, toolCalls, a.cfg.ParallelTools, a.cfg.MaxParallelTools)

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
			return err
		}

		a.params.Messages = append(a.params.Messages, openai.ToolMessage(r.Result, tools[i].id))
	}

	return nil
}

// Chat sends a message and returns the response content.
// It handles tool calls internally.
func (a *OpenAI) Chat(ctx context.Context, prompt string) (string, error) {
	if err := a.Add(ctx, prompt); err != nil {
		return "", err
	}

	// Clear structured output mode if it was set (e.g. from initialisation)
	a.params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{}

	maxSteps := a.cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	for step := range maxSteps {
		a.logger.Debug("OpenAI History State", "step", step)

		for i := range a.params.Messages {
			a.logger.Debug("Turn", "idx", i)
		}

		resp, err := a.oai.Chat.Completions.New(ctx, a.params)
		if err != nil {
			return "", err
		}

		msg := resp.Choices[0].Message
		a.params.Messages = append(a.params.Messages, msg.ToParam())

		if msg.Content != "" {
			a.logger.Debug("OpenAI Reasoning", "text", msg.Content)
		}

		if len(msg.ToolCalls) > 0 {
			a.logger.Info("OpenAI Tool Call count", "count", len(msg.ToolCalls))
			a.appendToolResults(ctx, msg.ToolCalls)

			continue
		}

		return msg.Content, nil
	}

	return "", errors.Newf("OpenAI reached maximum ReAct steps (%d) without a final answer", maxSteps)
}
