package chat

// Shared AI-provider credential resolution.
//
// Each provider (Claude, OpenAI, Gemini) resolves an API key from
// four possible sources in a fixed precedence order. Consolidating
// that logic in one helper keeps the security contract auditable:
// every provider routes through the same function, and adding a new
// provider is a two-line change to the call site rather than a
// copy-paste of the whole cascade.
//
// Resolution order:
//
//  1. Direct token passed by the caller (e.g. Config.Token in tests).
//     Always wins when non-empty.
//  2. Environment-variable reference in config (<provider>.api.env).
//     The config records the NAME of an env var; the secret lives
//     outside the config file. Recommended default set by the
//     interactive setup wizard.
//  3. Literal value in config (<provider>.api.key). Supported for
//     backward compatibility; the doctor `credentials.no-literal`
//     check warns when this path is used.
//  4. Well-known environment variable (ANTHROPIC_API_KEY,
//     OPENAI_API_KEY, GEMINI_API_KEY). The ecosystem fallback used by
//     most provider SDKs and CI platforms.
//
// Closes the AI half of the input-collection changes in
// docs/development/specs/2026-04-02-credential-storage-hardening.md
// (Phase 1).

import (
	"os"
	"strings"

	"github.com/phpboyscout/go-tool-base/pkg/config"
)

// resolveAPIKey implements the four-step precedence above. Whitespace
// is trimmed at every step and empty values fall through so a
// half-configured key cannot mask a fully-configured one at a lower
// priority.
//
// direct is the caller-supplied value (typically Config.Token); pass
// "" to skip it. cfg is the loaded config; pass nil to skip the
// config-lookup steps. envKey is the config key that records an env
// var NAME (e.g. ConfigKeyClaudeEnv). literalKey is the config key
// that records a literal secret (e.g. ConfigKeyClaudeKey).
// envFallback is the well-known environment variable name for the
// provider (e.g. EnvClaudeKey).
func resolveAPIKey(direct string, cfg config.Containable, envKey, literalKey, envFallback string) string {
	if v := strings.TrimSpace(direct); v != "" {
		return v
	}

	if cfg != nil {
		if name := strings.TrimSpace(cfg.GetString(envKey)); name != "" {
			if v := strings.TrimSpace(os.Getenv(name)); v != "" {
				return v
			}
		}

		if v := strings.TrimSpace(cfg.GetString(literalKey)); v != "" {
			return v
		}
	}

	if envFallback != "" {
		if v := strings.TrimSpace(os.Getenv(envFallback)); v != "" {
			return v
		}
	}

	return ""
}
