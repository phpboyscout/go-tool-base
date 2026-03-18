package chat

import "github.com/openai/openai-go/v3"

const (
	// DefaultModelGemini is the default model for the Gemini provider.
	DefaultModelGemini = "gemini-3-flash-preview"

	// DefaultModelClaude is the default model for the Claude provider.
	DefaultModelClaude = "claude-sonnet-4-5"

	// DefaultModelOpenAI is the default model for the OpenAI provider.
	DefaultModelOpenAI = openai.ChatModelGPT5
)
