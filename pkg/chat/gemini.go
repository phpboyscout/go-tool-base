package chat

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
	"google.golang.org/genai"

	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

var (
	// allow mocking in tests.
	ExportGenaiNewClient = genai.NewClient
)

func init() {
	RegisterProvider(ProviderGemini, newGemini)
}

// Gemini implements the ChatClient interface using Google's Generative AI SDK.
type Gemini struct {
	client  *genai.Client
	model   string
	config  *genai.GenerateContentConfig
	cfg     Config
	history []*genai.Content
	tools   map[string]Tool
	props   *props.Props
}

// newGemini initializes a new Gemini chat client.
func newGemini(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
	token := getGeminiToken(p, cfg)
	if token == "" {
		return nil, errors.New("Gemini API key is required")
	}

	clientConfig := buildGeminiClientConfig(token, cfg)

	client, err := ExportGenaiNewClient(ctx, clientConfig)
	if err != nil {
		return nil, errors.Newf("failed to create gemini client: %w", err)
	}

	modelName := cfg.Model
	if modelName == "" {
		modelName = DefaultModelGemini
	}

	baseCfg := buildGeminiGenerateConfig(cfg)

	return &Gemini{
		client:  client,
		model:   modelName,
		config:  baseCfg,
		cfg:     cfg,
		history: make([]*genai.Content, 0),
		tools:   make(map[string]Tool),
		props:   p,
	}, nil
}

func getGeminiToken(p *props.Props, cfg Config) string {
	token := cfg.Token
	if token == "" && p.Config != nil {
		token = p.Config.GetString(ConfigKeyGeminiKey)
	}

	if token == "" {
		token = os.Getenv(EnvGeminiKey)
	}

	return token
}

func buildGeminiClientConfig(token string, cfg Config) *genai.ClientConfig {
	clientConfig := &genai.ClientConfig{
		APIKey:     token,
		HTTPClient: gtbhttp.NewClient(),
	}

	if cfg.BaseURL != "" {
		clientConfig.HTTPOptions.BaseURL = cfg.BaseURL
	}

	return clientConfig
}

func buildGeminiGenerateConfig(cfg Config) *genai.GenerateContentConfig {
	baseCfg := &genai.GenerateContentConfig{}
	if cfg.SystemPrompt != "" {
		baseCfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: cfg.SystemPrompt}},
		}
	}

	if cfg.ResponseSchema != nil {
		if s, ok := cfg.ResponseSchema.(*jsonschema.Schema); ok {
			baseCfg.ResponseMIMEType = "application/json"
			baseCfg.ResponseSchema = convertToGeminiSchema(s)
		}
	}

	return baseCfg
}

// Add appends a user message to the conversation history.
func (g *Gemini) Add(_ context.Context, prompt string) error {
	if prompt == "" {
		return errors.New("prompt cannot be empty")
	}

	g.history = append(g.history, &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: prompt}},
	})

	return nil
}

// Ask sends a question to the Gemini chat client and expects a structured response.
func (g *Gemini) Ask(ctx context.Context, question string, target any) error {
	if question == "" {
		return errors.New("question cannot be empty")
	}

	askCfg := g.cloneConfig()
	askCfg.ResponseMIMEType = "application/json"

	chat, err := g.client.Chats.Create(ctx, g.model, askCfg, g.history)
	if err != nil {
		return errors.Newf("failed to create gemini chat session: %w", err)
	}

	resp, err := chat.Send(ctx, genai.NewPartFromText(question))
	if err != nil {
		return errors.Newf("gemini send message failed: %w", err)
	}

	text := resp.Text()
	if text == "" {
		return errors.New("empty response from Gemini")
	}

	if err := json.Unmarshal([]byte(text), target); err != nil {
		return errors.Newf("failed to unmarshal gemini response: %w", err)
	}

	return nil
}

// SetTools configures the tools available to the AI.
func (g *Gemini) SetTools(tools []Tool) error {
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))

	for _, t := range tools {
		g.tools[t.Name] = t

		decls = append(decls, &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  convertToGeminiSchema(t.Parameters),
		})
	}

	g.config.Tools = []*genai.Tool{{FunctionDeclarations: decls}}

	return nil
}

// Chat sends a message and returns the response content, handling tool calls internally.
func (g *Gemini) Chat(ctx context.Context, prompt string) (string, error) {
	if prompt == "" {
		return "", errors.New("prompt cannot be empty")
	}

	chatCfg := g.cloneConfig()
	chatCfg.ResponseMIMEType = ""
	chatCfg.ResponseSchema = nil

	chat, err := g.client.Chats.Create(ctx, g.model, chatCfg, g.history)
	if err != nil {
		return "", errors.Newf("failed to create gemini chat session: %w", err)
	}

	return g.chatNonStreaming(ctx, chat, []*genai.Part{genai.NewPartFromText(prompt)})
}

func (g *Gemini) chatNonStreaming(ctx context.Context, chat *genai.Chat, parts []*genai.Part) (string, error) {
	maxSteps := g.cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	var textResponse strings.Builder

	currentParts := parts

	for step := range maxSteps {
		g.props.Logger.Debug("Gemini step", "step", step, "parts", len(currentParts))

		resp, err := chat.Send(ctx, currentParts...)
		if err != nil {
			return "", g.handleGeminiError(err, step)
		}

		if text := resp.Text(); text != "" {
			textResponse.WriteString(text)
			g.props.Logger.Debug("Gemini Reasoning", "text", text)
		}

		funcCalls := resp.FunctionCalls()
		if len(funcCalls) == 0 {
			return textResponse.String(), nil
		}

		g.props.Logger.Info("Gemini tool calls", "count", len(funcCalls))

		currentParts = g.executeFuncCalls(ctx, funcCalls)
	}

	return "", errors.Newf("Gemini reached maximum ReAct steps (%d) without a final answer", maxSteps)
}

func (g *Gemini) handleGeminiError(err error, step int) error {
	var apiErr *genai.APIError
	if errors.As(err, &apiErr) {
		return errors.Newf("Gemini API Error (%d): %s", apiErr.Code, apiErr.Message)
	}

	return errors.Newf("gemini send message failed (step %d): %v", step, err)
}

// geminiMarshaledCall holds a pre-marshaled function call argument payload.
type geminiMarshaledCall struct {
	name   string
	input  json.RawMessage
	hasErr bool
}

func (g *Gemini) marshalFuncCalls(funcCalls []*genai.FunctionCall) []geminiMarshaledCall {
	out := make([]geminiMarshaledCall, len(funcCalls))

	for i, fc := range funcCalls {
		argsB, err := json.Marshal(fc.Args)
		if err != nil {
			g.props.Logger.Error("Failed to marshal tool arguments", "tool", fc.Name, "error", err)
			out[i] = geminiMarshaledCall{name: fc.Name, hasErr: true}
		} else {
			out[i] = geminiMarshaledCall{name: fc.Name, input: argsB}
		}
	}

	return out
}

func (g *Gemini) executeFuncCalls(ctx context.Context, funcCalls []*genai.FunctionCall) []*genai.Part {
	mCalls := g.marshalFuncCalls(funcCalls)

	if g.cfg.ParallelTools && len(funcCalls) > 1 {
		return g.executeFuncCallsParallel(ctx, mCalls)
	}

	return g.executeFuncCallsSequential(ctx, mCalls)
}

func (g *Gemini) executeFuncCallsParallel(ctx context.Context, mCalls []geminiMarshaledCall) []*genai.Part {
	calls := make([]ToolCall, 0, len(mCalls))
	indices := make([]int, 0, len(mCalls))

	// allParts preserves insertion order; error entries are pre-filled.
	allParts := make([]*genai.Part, len(mCalls))

	for i, mc := range mCalls {
		if mc.hasErr {
			allParts[i] = genai.NewPartFromFunctionResponse(mc.name, map[string]any{"error": "failed to marshal arguments"})
		} else {
			calls = append(calls, ToolCall{Name: mc.name, Input: mc.input})
			indices = append(indices, i)
		}
	}

	for j, r := range executeToolsParallel(ctx, g.props.Logger, g.tools, calls, g.cfg.MaxParallelTools) {
		allParts[indices[j]] = genai.NewPartFromFunctionResponse(r.Name, map[string]any{"result": r.Result})
	}

	parts := make([]*genai.Part, 0, len(allParts))

	for _, p := range allParts {
		if p != nil {
			parts = append(parts, p)
		}
	}

	return parts
}

func (g *Gemini) executeFuncCallsSequential(ctx context.Context, mCalls []geminiMarshaledCall) []*genai.Part {
	parts := make([]*genai.Part, 0, len(mCalls))

	for _, mc := range mCalls {
		if mc.hasErr {
			parts = append(parts, genai.NewPartFromFunctionResponse(mc.name, map[string]any{"error": "failed to marshal arguments"}))

			continue
		}

		result := executeTool(ctx, g.props.Logger, g.tools, mc.name, mc.input)
		parts = append(parts, genai.NewPartFromFunctionResponse(mc.name, map[string]any{"result": result}))
	}

	return parts
}

func (g *Gemini) cloneConfig() *genai.GenerateContentConfig {
	if g.config == nil {
		return &genai.GenerateContentConfig{}
	}

	cp := *g.config

	return &cp
}
