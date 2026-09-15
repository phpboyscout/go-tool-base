package chat

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// The provider credentials declared where their keys are defined.
//
// Spec 0189 R1/R4: `doctor` reported resolution for forges only, so an operator
// could be told exactly which rung supplies their GitHub token and nothing at
// all about the Anthropic key sitting in the same file. These registrations
// close that, and they live here rather than in doctor because this package owns
// the keys — a fourth hand-maintained list somewhere else is the problem, not
// the fix.
func init() {
	for _, d := range providerCredentials() {
		credentialposture.Register(d)
	}
}

// providerCredentials is the declaration itself, split out so it is testable
// without relying on init having run.
func providerCredentials() []credentialposture.Descriptor {
	return []credentialposture.Descriptor{
		{
			Owner:       "chat:anthropic",
			Feature:     string(props.AiCmd),
			Label:       "Anthropic API key",
			EnvKey:      ConfigKeyClaudeEnv,
			KeychainKey: ConfigKeyClaudeKeychain,
			LiteralKey:  ConfigKeyClaudeKey,
			FallbackEnv: EnvClaudeKey,
		},
		{
			Owner:       "chat:openai",
			Feature:     string(props.AiCmd),
			Label:       "OpenAI API key",
			EnvKey:      ConfigKeyOpenAIEnv,
			KeychainKey: ConfigKeyOpenAIKeychain,
			LiteralKey:  ConfigKeyOpenAIKey,
			FallbackEnv: EnvOpenAIKey,
		},
		{
			Owner:       "chat:gemini",
			Feature:     string(props.AiCmd),
			Label:       "Gemini API key",
			EnvKey:      ConfigKeyGeminiEnv,
			KeychainKey: ConfigKeyGeminiKeychain,
			LiteralKey:  ConfigKeyGeminiKey,
			FallbackEnv: EnvGeminiKey,
		},
		{
			Owner:       "chat:azure",
			Feature:     string(props.AiCmd),
			Label:       "Azure OpenAI API key",
			EnvKey:      ConfigKeyAzureEnv,
			KeychainKey: ConfigKeyAzureKeychain,
			LiteralKey:  ConfigKeyAzureKey,
			FallbackEnv: EnvAzureKey,
		},
	}
}
