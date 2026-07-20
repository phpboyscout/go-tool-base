package chat

// GTB config-key schema. These constants describe how go-tool-base's Viper
// configuration names AI provider settings; they are owned by this adapter, not
// the standalone chat module (which is config-system-agnostic). config_adapter.go
// maps these keys into the module's typed Config/CredentialConfig.

// ConfigKeyAIProvider is the config key for the AI provider.
const ConfigKeyAIProvider = "ai.provider"

// ConfigKeyAIRequestTimeout is the config key for the per-request AI timeout
// (a duration like "8m"). Overrides DefaultChatRequestTimeout.
const ConfigKeyAIRequestTimeout = "ai.request_timeout"

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
