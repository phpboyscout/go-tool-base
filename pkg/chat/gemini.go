package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
	"github.com/phpboyscout/gtb/pkg/props"
	"google.golang.org/genai"
)

func init() {
	RegisterProvider(ProviderGemini, newGemini)
}

// Gemini implements the ChatClient interface using Google's Generative AI SDK.
type Gemini struct {
	ctx     context.Context
	client  *genai.Client
	model   string
	config  *genai.GenerateContentConfig
	history []*genai.Content
	tools   map[string]Tool
	props   *props.Props
}

// newGemini initializes a new Gemini chat client.
func newGemini(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
	token := cfg.Token
	if token == "" {
		token = p.Config.GetString(ConfigKeyGeminiKey)
	}

	if token == "" {
		return nil, errors.New("Gemini API key is required")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: token})
	if err != nil {
		return nil, errors.Newf("failed to create gemini client: %w", err)
	}

	modelName := cfg.Model
	if modelName == "" {
		modelName = DefaultModelGemini
	}

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

	return &Gemini{
		ctx:     ctx,
		client:  client,
		model:   modelName,
		config:  baseCfg,
		history: make([]*genai.Content, 0),
		tools:   make(map[string]Tool),
		props:   p,
	}, nil
}

// Add appends a user message to the conversation history.
func (g *Gemini) Add(prompt string) error {
	g.history = append(g.history, &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: prompt}},
	})

	return nil
}

// Ask sends a question to the Gemini chat client and expects a structured response.
func (g *Gemini) Ask(question string, target any) error {
	askCfg := g.cloneConfig()
	askCfg.ResponseMIMEType = "application/json"

	chat, err := g.client.Chats.Create(g.ctx, g.model, askCfg, g.history)
	if err != nil {
		return errors.Newf("failed to create gemini chat session: %w", err)
	}

	resp, err := chat.Send(g.ctx, genai.NewPartFromText(question))
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
	const maxSteps = 20

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
			g.props.Logger.Info("Gemini Reasoning", "text", text)
		}

		funcCalls := resp.FunctionCalls()
		if len(funcCalls) == 0 {
			return textResponse.String(), nil
		}

		g.props.Logger.Info("Gemini tool calls", "count", len(funcCalls))

		var toolResultParts []*genai.Part

		for _, fc := range funcCalls {
			toolResultParts = append(toolResultParts, g.executeTool(ctx, fc))
		}

		currentParts = toolResultParts
	}

	return "", errors.New("Gemini reached maximum ReAct steps (20) without a final answer")
}

func (g *Gemini) handleGeminiError(err error, step int) error {
	var apiErr *genai.APIError
	if errors.As(err, &apiErr) {
		return errors.Newf("Gemini API Error (%d): %s", apiErr.Code, apiErr.Message)
	}

	return errors.Newf("gemini send message failed (step %d): %v", step, err)
}

func (g *Gemini) executeTool(ctx context.Context, fc *genai.FunctionCall) *genai.Part {
	g.props.Logger.Info("Gemini Tool Call", "tool", fc.Name)
	g.props.Logger.Debug("Gemini Tool Parameters", "tool", fc.Name, "args", fc.Args)

	tool, ok := g.tools[fc.Name]
	if !ok {
		g.props.Logger.Warn("Tool not found", "tool", fc.Name)

		return genai.NewPartFromFunctionResponse(fc.Name, map[string]any{"error": "tool not found"})
	}

	argsB, err := json.Marshal(fc.Args)
	if err != nil {
		g.props.Logger.Error("Failed to marshal tool arguments", "tool", fc.Name, "error", err)

		return genai.NewPartFromFunctionResponse(fc.Name, map[string]any{"error": "failed to marshal arguments"})
	}

	out, err := tool.Handler(ctx, argsB)
	if err != nil {
		g.props.Logger.Error("Tool execution failed", "tool", fc.Name, "error", err)

		return genai.NewPartFromFunctionResponse(fc.Name, map[string]any{"error": err.Error()})
	}

	g.props.Logger.Info("Tool executed successfully", "tool", fc.Name)

	return genai.NewPartFromFunctionResponse(fc.Name, map[string]any{"result": out})
}

func (g *Gemini) cloneConfig() *genai.GenerateContentConfig {
	if g.config == nil {
		return &genai.GenerateContentConfig{}
	}

	cp := *g.config

	return &cp
}
