package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
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

// ConfigKeyAIProvider is the config key for the AI provider.
const ConfigKeyAIProvider = "ai.provider"

// ConfigKeyAIRequestTimeout is the config key for the per-request AI timeout
// (a duration like "8m"). Overrides DefaultChatRequestTimeout.
const ConfigKeyAIRequestTimeout = "ai.request_timeout"

// EnvAIProvider is the environment variable for overriding the AI provider.
const EnvAIProvider = "AI_PROVIDER"

// ConfigKeyAIFallbackEnabled toggles cross-provider failover. When false (the
// default), the single-provider path is byte-for-byte unchanged.
const ConfigKeyAIFallbackEnabled = "ai.fallback.enabled"

// ConfigKeyAIFallbackProviders is the ordered list of provider names ([]string)
// to fail over across; index 0 is the primary. See NewWithFallback.
const ConfigKeyAIFallbackProviders = "ai.fallback.providers"

// Per-provider credential config surface.
//
// Each provider exposes three related strings:
//
//   - ConfigKey<Provider>Key — full config path for the literal
//     API key (e.g. "openai.api.key").
//   - ConfigKey<Provider>Env — full config path for an env-var
//     reference; the value stored here is the NAME of an env var
//     (e.g. "OPENAI_API_KEY") that holds the actual secret.
//   - Env<Provider>Key — well-known unprefixed environment variable
//     used as the ecosystem fallback when no config is present.
//
// The per-provider root path (e.g. "openai.api") is defined once as
// an unexported constant and composed into the public constants so
// adding `.keychain` or renaming the root stays a single edit.
const (
	configRootOpenAI = "openai.api"
	configRootClaude = "anthropic.api"
	configRootGemini = "gemini.api"

	ConfigKeyOpenAIKey      = configRootOpenAI + ".key"
	ConfigKeyOpenAIEnv      = configRootOpenAI + ".env"
	ConfigKeyOpenAIKeychain = configRootOpenAI + ".keychain"
	EnvOpenAIKey            = "OPENAI_API_KEY"

	ConfigKeyClaudeKey      = configRootClaude + ".key"
	ConfigKeyClaudeEnv      = configRootClaude + ".env"
	ConfigKeyClaudeKeychain = configRootClaude + ".keychain"
	EnvClaudeKey            = "ANTHROPIC_API_KEY"

	ConfigKeyGeminiKey      = configRootGemini + ".key"
	ConfigKeyGeminiEnv      = configRootGemini + ".env"
	ConfigKeyGeminiKeychain = configRootGemini + ".keychain"
	EnvGeminiKey            = "GEMINI_API_KEY"
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
	Add(ctx context.Context, prompt string, media ...Media) error
	// Ask sends a question and unmarshals the structured response into
	// target. If Config.ResponseSchema was set during construction, the
	// provider enforces that schema. If no schema is set, the provider
	// returns the raw text content unmarshalled into target (which must
	// be a *string or implement json.Unmarshaler).
	Ask(ctx context.Context, question string, target any, media ...Media) error
	// SetTools configures the tools available to the AI. This replaces
	// (not appends to) any previously set tools.
	SetTools(tools []Tool) error
	// Chat sends a message and returns the response content. If tools
	// are configured, the provider handles tool calls internally via a
	// ReAct loop bounded by Config.MaxSteps (default 20).
	Chat(ctx context.Context, prompt string, media ...Media) (string, error)
	// Usage returns the cumulative token usage across every provider
	// round-trip made by this client instance since construction. A single
	// Chat/Ask/StreamChat call may make multiple round-trips (one per ReAct
	// step) and all are summed. Providers that do not report token counts
	// (e.g. ProviderClaudeLocal) contribute a zero, Known == false Usage.
	// Wire Config.UsageObserver to observe usage per round-trip instead.
	Usage() Usage
}

// Config holds configuration for a chat client.
type Config struct {
	// Provider is the AI service provider to use.
	Provider Provider
	// Model is the specific model to use (e.g., "gpt-4o", "claude-3-5-sonnet").
	Model string
	// Token is the API key or token for the service.
	Token string
	// Credentials carries provider-specific credential config resolved by the
	// host application. Token still wins when both are set.
	Credentials CredentialConfig `json:"-"`
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
	// RequestTimeout bounds a single HTTP request to the provider. Zero falls
	// back to the ai.request_timeout config key, then DefaultChatRequestTimeout.
	// Bounded on purpose so a stuck model fails rather than hanging forever.
	RequestTimeout time.Duration
	// ParallelTools enables concurrent execution of multiple tool calls
	// within a single ReAct step. Disabled by default.
	ParallelTools bool
	// MaxParallelTools limits the number of tools executing concurrently.
	// Zero means use the default (5). Only effective when ParallelTools is true.
	MaxParallelTools int

	// Seed, when non-nil, requests deterministic sampling from providers
	// that support a seed parameter (currently OpenAI / OpenAI-compatible).
	// Nil — the default — omits the seed entirely so the provider samples
	// normally. A previous build hardcoded seed=0, silently pinning every
	// OpenAI request to one sampling path; set this only when you need
	// reproducible-ish completions.
	Seed *int64

	// ExecLookPath overrides exec.LookPath for the ClaudeLocal provider.
	// Nil means use the real exec.LookPath.
	ExecLookPath func(string) (string, error) `json:"-"`
	// ExecCommand overrides exec.CommandContext for the ClaudeLocal provider
	// and the update command's config re-init.
	// Nil means use the real exec.CommandContext.
	ExecCommand func(context.Context, string, ...string) *exec.Cmd `json:"-"`
	// GenaiNewClient overrides the Gemini client constructor for testing.
	// Must be func(context.Context, *genai.ClientConfig) (*genai.Client, error).
	// Nil means use the real genai.NewClient.
	GenaiNewClient any `json:"-"`

	// UsageObserver, when non-nil, is invoked once per provider round-trip
	// with that round-trip's token usage. A ReAct tool-calling loop fires it
	// once per step. This is the opt-in hook for emitting a telemetry event,
	// metric, or log line; the chat client never depends on a telemetry
	// collector. Providers that do not report usage still fire the observer
	// with a Known == false Usage. The callback runs synchronously on the
	// calling goroutine, so keep it fast and non-blocking.
	UsageObserver func(Usage) `json:"-"`

	// AllowInsecureBaseURL permits HTTP (non-HTTPS) BaseURLs. This is
	// exclusively for tests that point at an httptest.Server. Production
	// callers must leave this false. The field is tagged json:"-" so
	// config files cannot enable it.
	AllowInsecureBaseURL bool `json:"-"`
}

// Settings contains the package-owned construction dependencies for a chat
// client. Config carries provider behaviour; Logger is optional and defaults to
// a no-op logger.
type Settings struct {
	Config Config
	Logger *slog.Logger
}

func (s Settings) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}

	return slog.New(slog.DiscardHandler)
}

// ProviderFactory creates a ChatClient for a named provider.
// Register implementations via RegisterProvider in an init() function to allow
// external packages to add providers without modifying this file.
type ProviderFactory func(ctx context.Context, settings Settings) (ChatClient, error)

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
func New(ctx context.Context, settings Settings) (ChatClient, error) {
	cfg := settings.Config
	log := settings.logger()

	applyDefaultProvider(log, &cfg)

	if err := validateProviderConfig(cfg); err != nil {
		return nil, err
	}

	factory, err := lookupProviderFactory(cfg.Provider)
	if err != nil {
		return nil, err
	}

	settings.Config = cfg

	client, err := factory(ctx, settings)
	if err != nil {
		return nil, err
	}

	logProviderEndpoint(log, cfg)

	return client, nil
}

func applyDefaultProvider(log *slog.Logger, cfg *Config) {
	if cfg.Provider != "" {
		return
	}

	if envProvider := os.Getenv(EnvAIProvider); envProvider != "" {
		cfg.Provider = Provider(envProvider)
		log.Debug("provider not specified in config, using environment variable",
			"env", EnvAIProvider, "provider", cfg.Provider)

		return
	}

	cfg.Provider = ProviderClaude
	log.Debug("no provider specified, using default", "provider", cfg.Provider)
}

func validateProviderConfig(cfg Config) error {
	// M-3 from the 2026-04-17 security audit: validate the provider
	// endpoint before any credentials hit the wire. See pkg/chat/baseurl.go.
	if err := ValidateBaseURL(cfg.BaseURL, cfg.AllowInsecureBaseURL); err != nil {
		return err
	}

	if cfg.Provider == ProviderOpenAICompatible && cfg.BaseURL == "" {
		return errors.WithHint(ErrInvalidBaseURL,
			"ProviderOpenAICompatible requires Config.BaseURL to be set")
	}

	return nil
}

func lookupProviderFactory(provider Provider) (ProviderFactory, error) {
	registryMu.RLock()

	factory, ok := providerRegistry[provider]

	registryMu.RUnlock()

	if !ok {
		return nil, errors.Newf("unsupported provider: %s", provider)
	}

	return factory, nil
}

func logProviderEndpoint(log *slog.Logger, cfg Config) {
	// Audit-log the endpoint host (never the full URL) so operators can
	// see which host each tool instance targets. Hostname only — the
	// path/query may carry provider-specific identifiers.
	if host := baseURLHost(cfg.BaseURL); host != "" {
		log.Info("chat provider initialised",
			"provider", string(cfg.Provider),
			"endpoint_host", host)
	} else {
		log.Info("chat provider initialised",
			"provider", string(cfg.Provider))
	}
}
