package chat

import "github.com/openai/openai-go/v3"

const (
	// DefaultModelGemini is the default model for the Gemini provider.
	DefaultModelGemini = "gemini-3-flash-preview"

	// DefaultModelClaude is the default model for the Claude provider.
	DefaultModelClaude = "claude-sonnet-4-5"

	// DefaultModelOpenAI is the default model for the OpenAI provider.
	DefaultModelOpenAI = openai.ChatModelGPT5

	// DefaultMaxSteps is the default maximum number of ReAct loop iterations.
	DefaultMaxSteps = 20

	// DefaultMaxTokensOpenAI is the default maximum tokens per response for OpenAI.
	DefaultMaxTokensOpenAI = 4096

	// DefaultMaxTokensClaude is the default maximum tokens per response for Claude.
	DefaultMaxTokensClaude = 8192

	// DefaultMaxTokensGemini is the default maximum tokens per response for Gemini.
	DefaultMaxTokensGemini = 8192
)
