package generator

import (
	"github.com/phpboyscout/gtb/pkg/chat"
)

func (g *Generator) resolveProvider() chat.Provider {
	provider := chat.ProviderClaude

	if g.props.Config != nil {
		if p := g.props.Config.GetString("ai.provider"); p != "" {
			provider = chat.Provider(p)
		}
	}

	if g.config.AIProvider != "" {
		provider = chat.Provider(g.config.AIProvider)
	}

	return provider
}

func (g *Generator) resolveToken(provider chat.Provider) string {
	if g.props.Config == nil {
		return ""
	}

	switch provider {
	case chat.ProviderOpenAI:
		return g.props.Config.GetString("openai.api.key")
	case chat.ProviderClaude:
		return g.props.Config.GetString("anthropic.api.key")
	case chat.ProviderGemini:
		return g.props.Config.GetString("gemini.api.key")
	default:
		return ""
	}
}

func (g *Generator) resolveModel(provider chat.Provider) string {
	model := ""
	if g.props.Config != nil {
		model = g.props.Config.GetString("ai.model")
	}

	if g.config.AIModel != "" {
		model = g.config.AIModel
	}

	if model == "" {
		switch provider {
		case chat.ProviderOpenAI:
			model = chat.DefaultModelOpenAI
		case chat.ProviderGemini:
			model = chat.DefaultModelGemini
		case chat.ProviderClaude:
			model = chat.DefaultModelClaude
		}
	}

	return model
}
