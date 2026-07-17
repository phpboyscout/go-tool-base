package generator

import (
	gochat "gitlab.com/phpboyscout/go/chat"
)

func (g *Generator) resolveProvider() gochat.Provider {
	provider := gochat.ProviderClaude

	if g.props.Config != nil {
		if p := g.props.Config.GetString("ai.provider"); p != "" {
			provider = gochat.Provider(p)
		}
	}

	if g.config.AIProvider != "" {
		provider = gochat.Provider(g.config.AIProvider)
	}

	return provider
}

func (g *Generator) resolveToken(provider gochat.Provider) string {
	if g.props.Config == nil {
		return ""
	}

	switch provider {
	case gochat.ProviderOpenAI, gochat.ProviderOpenAICompatible:
		return g.props.Config.GetString("openai.api.key")
	case gochat.ProviderClaude:
		return g.props.Config.GetString("anthropic.api.key")
	case gochat.ProviderGemini:
		return g.props.Config.GetString("gemini.api.key")
	case gochat.ProviderClaudeLocal:
		return ""
	default:
		return ""
	}
}

func (g *Generator) resolveModel(provider gochat.Provider) string {
	model := ""
	if g.props.Config != nil {
		model = g.props.Config.GetString("ai.model")
	}

	if g.config.AIModel != "" {
		model = g.config.AIModel
	}

	if model == "" {
		switch provider {
		case gochat.ProviderOpenAI, gochat.ProviderOpenAICompatible:
			model = gochat.DefaultModelOpenAI
		case gochat.ProviderGemini:
			model = gochat.DefaultModelGemini
		case gochat.ProviderClaude:
			model = gochat.DefaultModelClaude
		case gochat.ProviderClaudeLocal:
			// no default model; the claude binary selects its own default
		}
	}

	return model
}
