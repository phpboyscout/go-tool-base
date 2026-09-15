package chat

import (
	"log/slog"
	"os"
	"time"

	gochat "gitlab.com/phpboyscout/go/chat"
)

// resolveChatTimeout picks the effective per-request timeout: an explicit
// Config.RequestTimeout wins, else gochat.DefaultChatRequestTimeout. Reimplemented here
// because the module keeps it unexported.
func resolveChatTimeout(cfg gochat.Config) time.Duration {
	if cfg.RequestTimeout > 0 {
		return cfg.RequestTimeout
	}

	return gochat.DefaultChatRequestTimeout
}

// Default-provider resolution is reimplemented here because the module keeps
// it unexported and credential loading, which is per provider, needs the
// effective provider first. The fallback chain itself is the module's
// (NewWithFallbackSettings); GTB no longer derives per-member configs.

// applyDefaultProvider fills an unset provider from AI_PROVIDER, else the Claude
// default — matching the module's own default resolution so credential loading
// (which is per-provider) sees the effective provider.
func applyDefaultProvider(log *slog.Logger, cfg *gochat.Config) {
	if cfg.Provider != "" {
		return
	}

	if envProvider := os.Getenv(EnvAIProvider); envProvider != "" {
		cfg.Provider = gochat.Provider(envProvider)
		log.Debug("provider not specified in config, using environment variable",
			"env", EnvAIProvider, "provider", cfg.Provider)

		return
	}

	cfg.Provider = gochat.ProviderClaude
	log.Debug("no provider specified, using default", "provider", cfg.Provider)
}
