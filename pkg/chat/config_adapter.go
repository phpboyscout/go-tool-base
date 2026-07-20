package chat

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/httpclient"

	gtbcreds "gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// SettingsFromProps adapts GTB props and framework config into package-owned
// chat settings while preserving the existing config key layout and precedence.
func SettingsFromProps(p *props.Props, cfg gochat.Config) (gochat.Settings, error) {
	// One view for the whole adaptation, so the runtime and credential reads
	// resolve against the same snapshot.
	view := readerFromProps(p)

	if err := applyRuntimeConfig(view, &cfg); err != nil {
		return gochat.Settings{}, err
	}

	log := loggerFromProps(p)
	applyDefaultProvider(log, &cfg)

	if err := applyCredentialConfig(view, &cfg); err != nil {
		return gochat.Settings{}, err
	}

	// Wire the GTB-side seams into the package-owned config: the OS-keychain
	// resolver and the hardened HTTP transport. The chat core stays free of
	// pkg/credentials and pkg/http; this adapter is the only place they enter.
	cfg.Credentials.Lookup = gtbcreds.Retrieve
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = newHardenedChatHTTPClient(resolveChatTimeout(cfg))
	}

	return gochat.Settings{Config: cfg, Logger: log}, nil
}

// newHardenedChatHTTPClient builds the chat HTTP client from go-tool-base's
// hardened transport, with the response-header timeout raised to the chat
// bound. This is the GTB counterpart to the module's plain default in
// httpclient.go, injected via Config.HTTPClient so provider code never sees
// pkg/http.
func newHardenedChatHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = gochat.DefaultChatRequestTimeout
	}

	transport := httpclient.NewTransport(nil)
	transport.ResponseHeaderTimeout = timeout

	return httpclient.NewClient(
		httpclient.WithTransport(transport),
		httpclient.WithTimeout(timeout),
	)
}

// NewFromProps adapts GTB props into typed chat settings, then constructs a
// provider client with the reusable package constructor.
func NewFromProps(ctx context.Context, p *props.Props, cfg gochat.Config) (gochat.ChatClient, error) {
	settings, err := SettingsFromProps(p, cfg)
	if err != nil {
		return nil, err
	}

	return gochat.New(ctx, settings)
}

// NewWithFallbackFromProps adapts GTB props into package-owned chat settings,
// then constructs either a single provider client or a fallback composite.
func NewWithFallbackFromProps(ctx context.Context, p *props.Props, cfg gochat.Config, opts ...gochat.FallbackOption) (gochat.ChatClient, error) {
	settings, err := SettingsFromProps(p, cfg)
	if err != nil {
		return nil, err
	}

	fallback, err := fallbackConfigFromProps(p)
	if err != nil {
		return nil, err
	}

	if !fallback.Enabled || len(fallback.Providers) == 0 {
		return gochat.New(ctx, settings)
	}

	log := settings.Logger
	warnFallbackPrimaryOverride(log, explicitProviderConfig(p, cfg), fallback.Providers[0])

	providerSettings := make([]gochat.Settings, 0, len(fallback.Providers))
	for _, providerConfig := range fallbackProviderConfigs(settings.Config, fallback.Providers) {
		next, err := SettingsFromProps(p, providerConfig)
		if err != nil {
			return nil, err
		}

		providerSettings = append(providerSettings, next)
	}

	opts = append([]gochat.FallbackOption{gochat.WithFallbackLogger(log)}, opts...)

	return gochat.NewFallbackFromSettings(ctx, providerSettings, opts...)
}

// NewWithFallback adapts GTB props and framework config before constructing a
// chat client with optional provider failover.
func NewWithFallback(ctx context.Context, p *props.Props, cfg gochat.Config, opts ...gochat.FallbackOption) (gochat.ChatClient, error) {
	return NewWithFallbackFromProps(ctx, p, cfg, opts...)
}

// explicitProviderConfig resolves the provider the operator actually configured
// — the caller-supplied Config.Provider or, failing that, ai.provider — without
// applying the package default. The fallback-override warning must fire only
// when an explicit provider is overridden by fallback.providers[0]; defaulting
// to claude first (as SettingsFromProps does) would warn spuriously when no
// provider was configured at all.
func explicitProviderConfig(p *props.Props, cfg gochat.Config) gochat.Config {
	explicit := gochat.Config{Provider: cfg.Provider}
	// applyRuntimeConfig only fills Provider from ai.provider when it is empty
	// and never defaults; the error here was already surfaced by the successful
	// SettingsFromProps call above, so it is safe to ignore.
	_ = applyRuntimeConfig(readerFromProps(p), &explicit)

	return explicit
}

func fallbackConfigFromProps(p *props.Props) (gochat.FallbackConfig, error) {
	return loadFallbackConfig(readerFromProps(p))
}

// readerFromProps pins a view of the live store, or nil when Props carries no
// store — nil is a valid Reader input to every load function below.
func readerFromProps(p *props.Props) config.Reader {
	if p == nil || p.Config == nil {
		return nil
	}

	return p.Config.View()
}

func loggerFromProps(p *props.Props) *slog.Logger {
	if p != nil {
		return logger.ToSlog(p.Logger)
	}

	return slog.New(slog.DiscardHandler)
}

func applyRuntimeConfig(cfg config.Reader, target *gochat.Config) error {
	if cfg == nil || target == nil {
		return nil
	}

	runtime, err := loadRuntimeConfig(cfg)
	if err != nil {
		return err
	}

	if target.Provider == "" && runtime.Provider != "" {
		target.Provider = runtime.Provider
	}

	if target.RequestTimeout == 0 && runtime.RequestTimeout != 0 {
		target.RequestTimeout = runtime.RequestTimeout
	}

	return nil
}

func applyCredentialConfig(cfg config.Reader, target *gochat.Config) error {
	if cfg == nil || target == nil || target.Provider == gochat.ProviderClaudeLocal {
		return nil
	}

	if !target.Credentials.IsZero() {
		return nil
	}

	credentials, err := loadCredentialConfig(cfg, target.Provider)
	if err != nil {
		return err
	}

	target.Credentials = credentials

	return nil
}

func loadRuntimeConfig(cfg config.Reader) (gochat.RuntimeConfig, error) {
	section, err := config.UnmarshalSection[gochat.RuntimeConfig](cfg, "ai")
	if err != nil || !section.Exists {
		return gochat.RuntimeConfig{}, err
	}

	return section.Value, nil
}

func loadFallbackConfig(cfg config.Reader) (gochat.FallbackConfig, error) {
	section, err := config.UnmarshalSection[gochat.FallbackConfig](cfg, "ai.fallback")
	if err != nil || !section.Exists {
		return gochat.FallbackConfig{}, err
	}

	return section.Value, nil
}

func loadCredentialConfig(cfg config.Reader, provider gochat.Provider) (gochat.CredentialConfig, error) {
	root := credentialConfigRoot(provider)
	if root == "" {
		return gochat.CredentialConfig{}, nil
	}

	section, err := config.UnmarshalSection[gochat.CredentialConfig](cfg, root)
	if err != nil || !section.Exists {
		return gochat.CredentialConfig{}, err
	}

	return section.Value, nil
}

func credentialConfigRoot(provider gochat.Provider) string {
	switch provider {
	case gochat.ProviderOpenAI, gochat.ProviderOpenAICompatible:
		return configRootOpenAI
	case gochat.ProviderClaude:
		return configRootClaude
	case gochat.ProviderGemini:
		return configRootGemini
	default:
		return ""
	}
}
