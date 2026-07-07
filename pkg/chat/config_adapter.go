package chat

import (
	"time"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// RuntimeConfig is the package-owned shape for GTB's ai.* config section.
type RuntimeConfig struct {
	Provider       Provider       `mapstructure:"provider"`
	RequestTimeout time.Duration  `mapstructure:"request_timeout"`
	Fallback       FallbackConfig `mapstructure:"fallback"`
}

// FallbackConfig is the package-owned shape for GTB's ai.fallback.* config.
type FallbackConfig struct {
	Enabled   bool       `mapstructure:"enabled"`
	Providers []Provider `mapstructure:"providers"`
}

// CredentialConfig is the package-owned shape for provider api credentials.
type CredentialConfig struct {
	Env      string `mapstructure:"env"`
	Keychain string `mapstructure:"keychain"`
	Key      string `mapstructure:"key"`
}

func applyRuntimeConfig(p *props.Props, cfg *Config) error {
	if p == nil || p.Config == nil || cfg == nil {
		return nil
	}

	if _, ok := p.Config.(*config.Container); !ok {
		applyLegacyRuntimeConfig(p.Config, cfg)

		return nil
	}

	runtime, err := loadRuntimeConfig(p.Config)
	if err != nil {
		return err
	}

	if cfg.Provider == "" && runtime.Provider != "" {
		cfg.Provider = runtime.Provider
	}

	if cfg.RequestTimeout == 0 && runtime.RequestTimeout != 0 {
		cfg.RequestTimeout = runtime.RequestTimeout
	}

	return nil
}

func applyLegacyRuntimeConfig(source config.Containable, cfg *Config) {
	if cfg.Provider == "" {
		cfg.Provider = cfgProvider(source)
	}
}

func applyCredentialConfig(p *props.Props, cfg *Config) error {
	if p == nil || p.Config == nil || cfg == nil || cfg.Provider == ProviderClaudeLocal {
		return nil
	}

	if !cfg.Credentials.IsZero() {
		return nil
	}

	credentials, err := loadCredentialConfig(p.Config, cfg.Provider)
	if err != nil {
		return err
	}

	cfg.Credentials = credentials

	return nil
}

func loadRuntimeConfig(cfg config.Containable) (RuntimeConfig, error) {
	if cfg == nil {
		return RuntimeConfig{}, nil
	}

	if _, ok := cfg.(*config.Container); ok {
		section, err := config.UnmarshalSection[RuntimeConfig](cfg, "ai")
		if err != nil || !section.Exists {
			return RuntimeConfig{}, err
		}

		return section.Value, nil
	}

	return RuntimeConfig{
		Provider: cfgProvider(cfg),
		Fallback: FallbackConfig{
			Enabled: cfg.GetBool(ConfigKeyAIFallbackEnabled),
		},
	}, nil
}

func loadFallbackConfig(cfg config.Containable) (FallbackConfig, error) {
	if cfg == nil {
		return FallbackConfig{}, nil
	}

	if _, ok := cfg.(*config.Container); ok {
		section, err := config.UnmarshalSection[FallbackConfig](cfg, "ai.fallback")
		if err != nil || !section.Exists {
			return FallbackConfig{}, err
		}

		return section.Value, nil
	}

	return FallbackConfig{
		Enabled:   cfg.GetBool(ConfigKeyAIFallbackEnabled),
		Providers: providerList(cfg.GetViper().GetStringSlice(ConfigKeyAIFallbackProviders)),
	}, nil
}

func loadCredentialConfig(cfg config.Containable, provider Provider) (CredentialConfig, error) {
	if cfg == nil {
		return CredentialConfig{}, nil
	}

	root := credentialConfigRoot(provider)
	if root == "" {
		return CredentialConfig{}, nil
	}

	if _, ok := cfg.(*config.Container); ok {
		section, err := config.UnmarshalSection[CredentialConfig](cfg, root)
		if err != nil || !section.Exists {
			return CredentialConfig{}, err
		}

		return section.Value, nil
	}

	return legacyCredentialConfig(cfg, provider), nil
}

func cfgProvider(cfg config.Containable) Provider {
	if cfgProvider := cfg.GetString(ConfigKeyAIProvider); cfgProvider != "" {
		return Provider(cfgProvider)
	}

	return ""
}

func legacyCredentialConfig(cfg config.Containable, provider Provider) CredentialConfig {
	switch provider {
	case ProviderOpenAI, ProviderOpenAICompatible:
		return CredentialConfig{
			Env:      cfg.GetString(ConfigKeyOpenAIEnv),
			Keychain: cfg.GetString(ConfigKeyOpenAIKeychain),
			Key:      cfg.GetString(ConfigKeyOpenAIKey),
		}
	case ProviderClaude:
		return CredentialConfig{
			Env:      cfg.GetString(ConfigKeyClaudeEnv),
			Keychain: cfg.GetString(ConfigKeyClaudeKeychain),
			Key:      cfg.GetString(ConfigKeyClaudeKey),
		}
	case ProviderGemini:
		return CredentialConfig{
			Env:      cfg.GetString(ConfigKeyGeminiEnv),
			Keychain: cfg.GetString(ConfigKeyGeminiKeychain),
			Key:      cfg.GetString(ConfigKeyGeminiKey),
		}
	default:
		return CredentialConfig{}
	}
}

func credentialConfigRoot(provider Provider) string {
	switch provider {
	case ProviderOpenAI, ProviderOpenAICompatible:
		return configRootOpenAI
	case ProviderClaude:
		return configRootClaude
	case ProviderGemini:
		return configRootGemini
	default:
		return ""
	}
}

func providerList(values []string) []Provider {
	if len(values) == 0 {
		return nil
	}

	providers := make([]Provider, 0, len(values))
	for _, value := range values {
		providers = append(providers, Provider(value))
	}

	return providers
}

// IsZero reports whether no credential config values were supplied.
func (c CredentialConfig) IsZero() bool {
	return c.Env == "" && c.Keychain == "" && c.Key == ""
}
