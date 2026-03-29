package chat

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"

	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// Provider defines the AI service provider.
type Provider string

const (
	// ProviderOpenAI uses OpenAI's API.
	ProviderOpenAI Provider = "openai"
	// ProviderOpenAICompatible uses any OpenAI-compatible API endpoint (e.g. Ollama, Groq).
	ProviderOpenAICompatible Provider = "openai-compatible"
	// ProviderClaude uses Anthropic's Claude API.
	ProviderClaude Provider = "claude"
	// ProviderClaudeLocal uses a locally installed claude CLI binary.
	ProviderClaudeLocal Provider = "claude-local"
	// ProviderGemini uses Google's Gemini API.
	ProviderGemini Provider = "gemini"
)

const (
	// ConfigKeyAIProvider is the config key for the AI provider.
	ConfigKeyAIProvider = "ai.provider"
	// ConfigKeyOpenAIKey is the config key for the OpenAI API key.
	ConfigKeyOpenAIKey = "openai.api.key"
	// ConfigKeyClaudeKey is the config key for the Claude/Anthropic API key.
	ConfigKeyClaudeKey = "anthropic.api.key"
	// ConfigKeyGeminiKey is the config key for the Gemini API key.
	ConfigKeyGeminiKey = "gemini.api.key"
)

const (
	// EnvAIProvider is the environment variable for overriding the AI provider.
	EnvAIProvider = "AI_PROVIDER"
	// EnvOpenAIKey is the environment variable for overriding the OpenAI API key.
	EnvOpenAIKey = "OPENAI_API_KEY"
	// EnvClaudeKey is the environment variable for overriding the Claude API key.
	EnvClaudeKey = "ANTHROPIC_API_KEY"
	// EnvGeminiKey is the environment variable for overriding the Gemini API key.
	EnvGeminiKey = "GEMINI_API_KEY"
)

// Tool represents a function that the AI can call.
type Tool struct {
	Name        string                                                       `json:"name"`
	Description string                                                       `json:"description"`
	Parameters  *jsonschema.Schema                                           `json:"parameters"`
	Handler     func(ctx context.Context, args json.RawMessage) (any, error) `json:"-"`
}

// ChatClient defines the interface for interacting with a chat service.
//
// Implementations are NOT safe for concurrent use by multiple goroutines.
// Each goroutine should use its own ChatClient instance.
//
// Message history from Add() calls persists across Chat() and Ask() calls
// within the same client instance. To start a fresh conversation, create
// a new client via chat.New().
type ChatClient interface {
	// Add appends a user message to the conversation history without
	// triggering a completion. The message persists for subsequent
	// Chat() or Ask() calls.
	Add(ctx context.Context, prompt string) error
	// Ask sends a question and unmarshals the structured response into
	// target. If Config.ResponseSchema was set during construction, the
	// provider enforces that schema. If no schema is set, the provider
	// returns the raw text content unmarshalled into target (which must
	// be a *string or implement json.Unmarshaler).
	Ask(ctx context.Context, question string, target any) error
	// SetTools configures the tools available to the AI. This replaces
	// (not appends to) any previously set tools.
	SetTools(tools []Tool) error
	// Chat sends a message and returns the response content. If tools
	// are configured, the provider handles tool calls internally via a
	// ReAct loop bounded by Config.MaxSteps (default 20).
	Chat(ctx context.Context, prompt string) (string, error)
}

// Config holds configuration for a chat client.
type Config struct {
	// Provider is the AI service provider to use.
	Provider Provider
	// Model is the specific model to use (e.g., "gpt-4o", "claude-3-5-sonnet").
	Model string
	// Token is the API key or token for the service.
	Token string
	// BaseURL overrides the API endpoint. Required when using ProviderOpenAICompatible.
	// Example: "http://localhost:11434/v1" for Ollama, "https://api.groq.com/openai/v1" for Groq.
	BaseURL string
	// SystemPrompt is the initial system prompt to set the context for the AI.
	SystemPrompt string
	// ResponseSchema is the JSON schema used to force a structured output from the AI.
	ResponseSchema any
	// SchemaName is the name of the response schema (e.g., "error_analysis").
	SchemaName string
	// SchemaDescription is a description of the response schema.
	SchemaDescription string
	// MaxSteps limits the number of ReAct loop iterations in Chat().
	// Zero means use the default (DefaultMaxSteps = 20).
	MaxSteps int
	// MaxTokens sets the maximum tokens per response.
	// Zero means use the provider default (OpenAI: 4096, Claude: 8192, Gemini: 8192).
	MaxTokens int
	// ParallelTools enables concurrent execution of multiple tool calls
	// within a single ReAct step. Disabled by default.
	ParallelTools bool
	// MaxParallelTools limits the number of tools executing concurrently.
	// Zero means use the default (5). Only effective when ParallelTools is true.
	MaxParallelTools int
}

// ProviderFactory creates a ChatClient for a named provider.
// Register implementations via RegisterProvider in an init() function to allow
// external packages to add providers without modifying this file.
type ProviderFactory func(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error)

var (
	providerRegistry = map[Provider]ProviderFactory{}
	registryMu       sync.RWMutex
)

// RegisterProvider registers a factory function for a provider name.
// Call this from an init() function in your provider file or external package.
func RegisterProvider(name Provider, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	providerRegistry[name] = factory
}

// New creates a ChatClient for the configured provider.
func New(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
	if cfg.Provider == "" {
		if envProvider := os.Getenv(EnvAIProvider); envProvider != "" {
			cfg.Provider = Provider(envProvider)
		} else {
			cfg.Provider = ProviderOpenAI
		}
	}

	registryMu.RLock()

	factory, ok := providerRegistry[cfg.Provider]

	registryMu.RUnlock()

	if !ok {
		return nil, errors.Newf("unsupported provider: %s", cfg.Provider)
	}

	return factory(ctx, p, cfg)
}
