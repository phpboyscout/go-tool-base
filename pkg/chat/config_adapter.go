package chat

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gitlab.com/phpboyscout/go/features"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/httpclient"

	gtbcreds "gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// SettingsFromProps adapts GTB props and framework config into package-owned
// chat settings while preserving the existing config key layout and precedence.
func SettingsFromProps(p *props.Props, cfg gochat.Config) (gochat.Settings, error) {
	// One view for the whole adaptation, so the runtime and credential reads
	// resolve against the same snapshot.
	view := props.ViewOrNil(p)

	if err := applyRuntimeConfig(view, &cfg); err != nil {
		return gochat.Settings{}, err
	}

	// The rung reads the providers the tool declares (spec 0196 D5), the same
	// view doctor and init ai use: a module registers more than the author
	// chose, and "the only one this binary links" is the author's one.
	log := props.SlogLogger(p)

	if err := applyDefaultProvider(log, &cfg, defaultCandidates(p.GetFeatures(), gochat.RegisteredProviders())); err != nil {
		return gochat.Settings{}, err
	}

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

	client, err := gochat.New(ctx, settings)

	return client, hintUnsupportedProvider(err, settings.Config.Provider)
}

// NewWithFallbackFromProps adapts GTB props into package-owned chat settings,
// then constructs either a single provider client or a fallback composite.
//
// The chain itself is the module's: NewWithFallbackSettings keeps the primary's
// model and addressing and lets every other member self-resolve, and the
// credential resolver below is how each member finds GTB's credential for it.
// GTB used to derive the per-member configs itself with a denylist that
// cleared the primary's model and would have carried addressing fields to
// every member (spec 0196 D6).
func NewWithFallbackFromProps(ctx context.Context, p *props.Props, cfg gochat.Config, opts ...gochat.FallbackOption) (gochat.ChatClient, error) {
	fallback, err := fallbackConfigFromProps(p)
	if err != nil {
		return nil, err
	}

	// fallback.providers[0] is the primary and stands in for an unset
	// provider, so the settings resolve for it rather than failing as unset.
	explicit := explicitProviderConfig(p, cfg).Provider
	if explicit == "" && fallback.Enabled && len(fallback.Providers) > 0 {
		cfg.Provider = fallback.Providers[0]
	}

	settings, err := SettingsFromProps(p, cfg)
	if err != nil {
		return nil, err
	}

	if !fallback.Enabled || len(fallback.Providers) == 0 {
		client, err := gochat.New(ctx, settings)

		return client, hintUnsupportedProvider(err, settings.Config.Provider)
	}

	// The module warns when fallback.providers[0] overrides Config.Provider;
	// hand it the provider the operator actually configured so an unset one
	// warns nothing.
	settings.Config.Provider = explicit

	opts = append([]gochat.FallbackOption{
		gochat.WithProviderCredentials(providerCredentialsFrom(props.ViewOrNil(p))),
	}, opts...)

	return gochat.NewWithFallbackSettings(ctx, settings, fallback, opts...)
}

// providerCredentialsFrom resolves a chain member's credential from GTB's
// config the way SettingsFromProps does for a single provider.
func providerCredentialsFrom(cfg config.Reader) gochat.ProviderCredentials {
	return func(provider gochat.Provider) (gochat.CredentialConfig, bool) {
		if !NeedsCredential(provider) {
			return gochat.CredentialConfig{}, false
		}

		credentials, err := loadCredentialConfig(cfg, provider)
		if err != nil || credentials.IsZero() {
			return gochat.CredentialConfig{}, false
		}

		credentials.Lookup = gtbcreds.Retrieve

		return credentials, true
	}
}

// NewWithFallback adapts GTB props and framework config before constructing a
// chat client with optional provider failover.
func NewWithFallback(ctx context.Context, p *props.Props, cfg gochat.Config, opts ...gochat.FallbackOption) (gochat.ChatClient, error) {
	return NewWithFallbackFromProps(ctx, p, cfg, opts...)
}

// explicitProviderConfig resolves the provider the operator actually configured
// — the caller-supplied Config.Provider or, failing that, ai.provider — without
// the AI_PROVIDER fallback. The fallback-override warning must fire only when
// an explicit provider is overridden by fallback.providers[0], never for one
// that was merely filled in.
func explicitProviderConfig(p *props.Props, cfg gochat.Config) gochat.Config {
	explicit := gochat.Config{Provider: cfg.Provider}
	// applyRuntimeConfig only fills Provider from ai.provider when it is empty
	// and never defaults; the error here was already surfaced by the successful
	// SettingsFromProps call above, so it is safe to ignore.
	_ = applyRuntimeConfig(props.ViewOrNil(p), &explicit)

	return explicit
}

func fallbackConfigFromProps(p *props.Props) (gochat.FallbackConfig, error) {
	return loadFallbackConfig(props.ViewOrNil(p))
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

	fillEmpty(&target.Model, runtime.Model)
	fillEmpty(&target.BaseURL, runtime.BaseURL)
	fillEmpty(&target.APIVersion, runtime.apiVersion())
	fillEmpty(&target.Project, runtime.Project)
	fillEmpty(&target.Location, runtime.Location)

	return nil
}

func fillEmpty(target *string, value string) {
	if *target == "" {
		*target = value
	}
}

// aiSection is the whole `ai:` block as GTB's config spells it. The module's
// RuntimeConfig stops at provider, timeout and fallback; the addressing keys
// are GTB's own schema (constants.go) and apply to the primary provider only.
type aiSection struct {
	gochat.RuntimeConfig `mapstructure:",squash"`

	Model   string `mapstructure:"model"`
	BaseURL string `mapstructure:"base_url"`
	// APIVersion is untyped because Azure's versions are dates (2024-10-21),
	// and an unquoted date in a YAML file arrives as a time.Time.
	APIVersion any    `mapstructure:"api_version"`
	Project    string `mapstructure:"project"`
	Location   string `mapstructure:"location"`
}

// apiVersion renders the configured API version as the dated string the
// provider expects, whether the file quoted it or not.
func (s aiSection) apiVersion() string {
	switch v := s.APIVersion.(type) {
	case nil:
		return ""
	case time.Time:
		return v.UTC().Format(time.DateOnly)
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func applyCredentialConfig(cfg config.Reader, target *gochat.Config) error {
	if cfg == nil || target == nil || !NeedsCredential(target.Provider) {
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

func loadRuntimeConfig(cfg config.Reader) (aiSection, error) {
	section, err := config.UnmarshalSection[aiSection](cfg, configSectionAI)
	if err != nil || !section.Exists {
		return aiSection{}, err
	}

	return section.Value, nil
}

func loadFallbackConfig(cfg config.Reader) (gochat.FallbackConfig, error) {
	section, err := config.UnmarshalSection[gochat.FallbackConfig](cfg, ConfigKeyAIFallback)
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
	case gochat.ProviderGemini, gochat.ProviderGeminiVertex:
		return configRootGemini
	case gochat.ProviderAzureOpenAI:
		return configRootAzure
	default:
		return ""
	}
}

// defaultCandidates is the list the default-provider rung chooses from: the
// providers the tool declares that its binary registers, or every registered
// provider for a binary that declares none (a hand-wired tool).
func defaultCandidates(set features.Set, registered []gochat.Provider) []gochat.Provider {
	linked, _ := LinkedProvidersIn(set, registered)

	return linked
}
