package chat

import (
	"log/slog"
	"os"
	"strings"
	"time"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"
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

// ErrProviderUnset is a chat client asked for with no provider anywhere: not
// the caller's Config, not ai.provider, not AI_PROVIDER. The framework names
// no vendor as its default (spec 0196 D5); a tool's author does, through its
// embedded defaults, and an end user does through init ai.
var ErrProviderUnset = errors.NewSentinel("gtb.chat.provider_unset", "no AI provider is configured")

// applyDefaultProvider fills an unset provider from AI_PROVIDER (the legacy
// fallback read only when every config layer left ai.provider empty), else
// from the registry when the binary links exactly one provider, so credential
// loading (which is per provider) sees the effective provider. It never names
// a vendor: with several linked and nothing configured it returns
// ErrProviderUnset naming them (spec 0196 D5).
func applyDefaultProvider(log *slog.Logger, cfg *gochat.Config, registered []gochat.Provider) error {
	if cfg.Provider != "" {
		return nil
	}

	if envProvider := os.Getenv(EnvAIProvider); envProvider != "" {
		cfg.Provider = gochat.Provider(envProvider)
		log.Debug("provider not specified in config, using environment variable",
			"env", EnvAIProvider, "provider", cfg.Provider)

		return nil
	}

	if len(registered) == 1 {
		cfg.Provider = registered[0]
		log.Debug("provider not specified, using the only one this binary links", "provider", cfg.Provider)

		return nil
	}

	hint := "Set " + ConfigKeyAIProvider + " in the tool's config, or run its `init ai` to choose one."

	if len(registered) > 1 {
		names := make([]string, len(registered))
		for i, p := range registered {
			names[i] = string(p)
		}

		hint += " This binary links: " + strings.Join(names, ", ") + "."
	}

	return errors.WithHint(ErrProviderUnset, hint)
}
