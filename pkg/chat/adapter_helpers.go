package chat

import (
	"log/slog"
	"os"
	"time"
)

// resolveChatTimeout picks the effective per-request timeout: an explicit
// Config.RequestTimeout wins, else DefaultChatRequestTimeout. Reimplemented here
// because the module keeps it unexported.
func resolveChatTimeout(cfg Config) time.Duration {
	if cfg.RequestTimeout > 0 {
		return cfg.RequestTimeout
	}

	return DefaultChatRequestTimeout
}

// These helpers were unexported in the standalone chat module, so the adapter
// reimplements the small pieces it needs on the GTB side: default-provider
// resolution (needed before per-provider credential loading), the
// fallback-primary-override warning, and per-provider config derivation for a
// fallback chain.

// applyDefaultProvider fills an unset provider from AI_PROVIDER, else the Claude
// default — matching the module's own default resolution so credential loading
// (which is per-provider) sees the effective provider.
func applyDefaultProvider(log *slog.Logger, cfg *Config) {
	if cfg.Provider != "" {
		return
	}

	if envProvider := os.Getenv(EnvAIProvider); envProvider != "" {
		cfg.Provider = Provider(envProvider)
		log.Debug("provider not specified in config, using environment variable",
			"env", EnvAIProvider, "provider", cfg.Provider)

		return
	}

	cfg.Provider = ProviderClaude
	log.Debug("no provider specified, using default", "provider", cfg.Provider)
}

// warnFallbackPrimaryOverride logs a WARN when ai.fallback.providers[0] overrides
// an explicitly configured ai.provider.
func warnFallbackPrimaryOverride(log *slog.Logger, cfg Config, primary Provider) {
	if cfg.Provider == "" || cfg.Provider == primary {
		return
	}

	log.Warn("ai.fallback.providers[0] overrides ai.provider",
		"ai_provider", string(cfg.Provider),
		"fallback_primary", string(primary))
}

// fallbackProviderConfigs derives one Config per provider in the fallback chain,
// inheriting nothing provider-specific from the base (each provider resolves its
// own credentials and default model).
func fallbackProviderConfigs(base Config, providers []Provider) []Config {
	cfgs := make([]Config, 0, len(providers))
	for _, name := range providers {
		cfgs = append(cfgs, perProviderConfig(base, name))
	}

	return cfgs
}

// perProviderConfig clones base for a specific provider, clearing the fields that
// must be resolved per provider.
func perProviderConfig(base Config, provider Provider) Config {
	pc := base
	pc.Provider = provider
	pc.Token = ""
	pc.Credentials = CredentialConfig{}
	pc.Model = ""
	pc.BaseURL = ""

	return pc
}
