package chat

// GTB config-key schema. These constants describe how go-tool-base's Viper
// configuration names AI provider settings; they are owned by this adapter, not
// the standalone chat module (which is config-system-agnostic). config_adapter.go
// maps these keys into the module's typed Config/CredentialConfig.

// configSectionAI is the config section every ai.* key lives under.
const configSectionAI = "ai"

// ConfigKeyAIProvider is the config key for the AI provider.
const ConfigKeyAIProvider = configSectionAI + ".provider"

// ConfigKeyAIRequestTimeout is the config key for the per-request AI timeout
// (a duration like "8m"). Overrides DefaultChatRequestTimeout.
const ConfigKeyAIRequestTimeout = "ai.request_timeout"

// ConfigKeyAIModel is the config key for the default model name.
const ConfigKeyAIModel = "ai.model"

// The addressing keys a few providers need. They apply to ai.provider only; a
// fallback member resolves its own endpoint (spec 0196 D6).
const (
	// ConfigKeyAIBaseURL overrides the API endpoint; required by
	// openai-compatible and azure-openai.
	ConfigKeyAIBaseURL = "ai.base_url"
	// ConfigKeyAIAPIVersion is the dated API version azure-openai requires.
	ConfigKeyAIAPIVersion = "ai.api_version"
	// ConfigKeyAIProject is the cloud project gemini-vertex addresses.
	ConfigKeyAIProject = "ai.project"
	// ConfigKeyAILocation is the region gemini-vertex and bedrock address.
	ConfigKeyAILocation = "ai.location"
)

// ConfigKeyAIFallback is the config section holding the fallback-provider chain.
const ConfigKeyAIFallback = "ai.fallback"

// ConfigKeyAIClaudeLocal is the legacy switch that routes generation through
// the local claude CLI.
const ConfigKeyAIClaudeLocal = "ai.claude.local"

// EnvAIProvider is the environment variable for overriding the AI provider.
const EnvAIProvider = "AI_PROVIDER"

// Per-provider credential config keys.
//
//   - ConfigKey<Provider>Key — full config path for the literal API key.
//   - ConfigKey<Provider>Env — full config path for an env-var reference (the
//     value stored is the NAME of an env var holding the secret).
//   - ConfigKey<Provider>Keychain — full config path for an OS-keychain reference.
//   - Env<Provider>Key — well-known unprefixed environment variable used as the
//     ecosystem fallback when no config is present.
const (
	configRootOpenAI = "openai.api"
	configRootClaude = "anthropic.api"
	configRootGemini = "gemini.api"
	configRootAzure  = "azure.api"

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

	ConfigKeyAzureKey      = configRootAzure + ".key"
	ConfigKeyAzureEnv      = configRootAzure + ".env"
	ConfigKeyAzureKeychain = configRootAzure + ".keychain"
	EnvAzureKey            = "AZURE_OPENAI_API_KEY"
)
