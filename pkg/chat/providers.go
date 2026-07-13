package chat

// go-tool-base ships every chat provider, so this adapter blank-imports each
// provider module to register it with the core registry (via its init()).
// Importing a provider module links exactly that provider's vendor SDK; a
// regulated downstream that wants a lighter binary can build its own adapter and
// import only the providers it needs.
import (
	_ "gitlab.com/phpboyscout/go/chat-anthropic" // registers "claude"
	_ "gitlab.com/phpboyscout/go/chat-gemini"    // registers "gemini"
	_ "gitlab.com/phpboyscout/go/chat-openai"    // registers "openai", "openai-compatible"
)
