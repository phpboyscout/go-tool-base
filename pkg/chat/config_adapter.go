package chat

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	gtbcreds "gitlab.com/phpboyscout/go-tool-base/pkg/credentials"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// SettingsFromProps adapts GTB props and framework config into package-owned
// chat settings while preserving the existing config key layout and precedence.
func SettingsFromProps(p *props.Props, cfg Config) (Settings, error) {
	if err := applyRuntimeConfig(p, &cfg); err != nil {
		return Settings{}, err
	}

	log := loggerFromProps(p)
	applyDefaultProvider(log, &cfg)

	if err := applyCredentialConfig(p, &cfg); err != nil {
		return Settings{}, err
	}

	// Wire the GTB-side seams into the package-owned config: the OS-keychain
	// resolver and the hardened HTTP transport. The chat core stays free of
	// pkg/credentials and pkg/http; this adapter is the only place they enter.
	cfg.Credentials.Lookup = gtbcreds.Retrieve
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = newHardenedChatHTTPClient(resolveChatTimeout(cfg))
	}

	return Settings{Config: cfg, Logger: log}, nil
}

// newHardenedChatHTTPClient builds the chat HTTP client from go-tool-base's
// hardened transport, with the response-header timeout raised to the chat
// bound. This is the GTB counterpart to the module's plain default in
// httpclient.go, injected via Config.HTTPClient so provider code never sees
// pkg/http.
func newHardenedChatHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultChatRequestTimeout
	}

	transport := gtbhttp.NewTransport(nil)
	transport.ResponseHeaderTimeout = timeout

	return gtbhttp.NewClient(
		gtbhttp.WithTransport(transport),
		gtbhttp.WithTimeout(timeout),
	)
}

// NewFromProps adapts GTB props into typed chat settings, then constructs a
// provider client with the reusable package constructor.
func NewFromProps(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
	settings, err := SettingsFromProps(p, cfg)
	if err != nil {
		return nil, err
	}

	return New(ctx, settings)
}

// NewWithFallbackFromProps adapts GTB props into package-owned chat settings,
// then constructs either a single provider client or a fallback composite.
func NewWithFallbackFromProps(ctx context.Context, p *props.Props, cfg Config, opts ...FallbackOption) (ChatClient, error) {
	settings, err := SettingsFromProps(p, cfg)
	if err != nil {
		return nil, err
	}

	fallback, err := fallbackConfigFromProps(p)
	if err != nil {
		return nil, err
	}

	if !fallback.Enabled || len(fallback.Providers) == 0 {
		return New(ctx, settings)
	}

	log := settings.Logger
	warnFallbackPrimaryOverride(log, explicitProviderConfig(p, cfg), fallback.Providers[0])

	providerSettings := make([]Settings, 0, len(fallback.Providers))
	for _, providerConfig := range fallbackProviderConfigs(settings.Config, fallback.Providers) {
		next, err := SettingsFromProps(p, providerConfig)
		if err != nil {
			return nil, err
		}

		providerSettings = append(providerSettings, next)
	}

	opts = append([]FallbackOption{WithFallbackLogger(log)}, opts...)

	return NewFallbackFromSettings(ctx, providerSettings, opts...)
}

// NewWithFallback adapts GTB props and framework config before constructing a
// chat client with optional provider failover.
func NewWithFallback(ctx context.Context, p *props.Props, cfg Config, opts ...FallbackOption) (ChatClient, error) {
	return NewWithFallbackFromProps(ctx, p, cfg, opts...)
}

// explicitProviderConfig resolves the provider the operator actually configured
// — the caller-supplied Config.Provider or, failing that, ai.provider — without
// applying the package default. The fallback-override warning must fire only
// when an explicit provider is overridden by fallback.providers[0]; defaulting
// to claude first (as SettingsFromProps does) would warn spuriously when no
// provider was configured at all.
func explicitProviderConfig(p *props.Props, cfg Config) Config {
	explicit := Config{Provider: cfg.Provider}
	// applyRuntimeConfig only fills Provider from ai.provider when it is empty
	// and never defaults; the error here was already surfaced by the successful
	// SettingsFromProps call above, so it is safe to ignore.
	_ = applyRuntimeConfig(p, &explicit)

	return explicit
}

func fallbackConfigFromProps(p *props.Props) (FallbackConfig, error) {
	if p == nil || p.Config == nil {
		return FallbackConfig{}, nil
	}

	return loadFallbackConfig(p.Config)
}

func loggerFromProps(p *props.Props) *slog.Logger {
	if p != nil {
		return logger.ToSlog(p.Logger)
	}

	return slog.New(slog.DiscardHandler)
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
