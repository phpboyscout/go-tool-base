package chat

// Shared AI-provider credential resolution.
//
// Each provider (Claude, OpenAI, Gemini) resolves an API key from
// five possible sources in a fixed precedence order. Consolidating
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
//  3. OS keychain reference in config (<provider>.api.keychain).
//     Value is a "<service>/<account>" pair passed through
//     [credentials.Retrieve]. Active only when
//     pkg/credentials/keychain is imported (or another backend is
//     registered); without a backend, this step falls through
//     silently so env-var / literal / fallback-env still resolve.
//  4. Literal value in config (<provider>.api.key). Supported for
//     backward compatibility; the doctor `credentials.no-literal`
//     check warns when this path is used.
//  5. Well-known environment variable (ANTHROPIC_API_KEY,
//     OPENAI_API_KEY, GEMINI_API_KEY). The ecosystem fallback used by
//     most provider SDKs and CI platforms.
//
// Phase 1 closed steps 1-2-4-5; Phase 2 adds the keychain step (3)
// per
// docs/development/specs/2026-04-02-credential-storage-hardening.md.

import (
	"context"
	"os"
	"strings"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

// resolveAPIKey implements the five-step precedence above. Whitespace
// is trimmed at every step and empty values fall through so a
// half-configured key cannot mask a fully-configured one at a lower
// priority.
//
// direct is the caller-supplied value (typically Config.Token); pass
// "" to skip it. cfg is the loaded config; pass nil to skip the
// config-lookup steps. envKey/keychainKey/literalKey identify the
// provider-specific config paths (e.g. ConfigKeyClaude{Env,Keychain,Key}).
// envFallback is the well-known unprefixed environment variable
// name for the provider (e.g. EnvClaudeKey).
func resolveAPIKey(
	ctx context.Context,
	direct string,
	cfg config.Containable,
	envKey, keychainKey, literalKey, envFallback string,
) string {
	if v := strings.TrimSpace(direct); v != "" {
		return v
	}

	if cfg != nil {
		if v := resolveFromConfig(ctx, cfg, envKey, keychainKey, literalKey); v != "" {
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

// resolveFromConfig walks the three config-based resolution steps.
// Extracted from resolveAPIKey to keep the top-level function under
// the cyclomatic-complexity budget now that keychain adds a step.
func resolveFromConfig(ctx context.Context, cfg config.Containable, envKey, keychainKey, literalKey string) string {
	if name := strings.TrimSpace(cfg.GetString(envKey)); name != "" {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}

	if ref := strings.TrimSpace(cfg.GetString(keychainKey)); ref != "" {
		if v := retrieveFromKeychainRef(ctx, ref); v != "" {
			return v
		}
	}

	return strings.TrimSpace(cfg.GetString(literalKey))
}

// retrieveFromKeychainRef fetches a secret named by a
// "<service>/<account>" reference. Returns "" on any error
// (unavailable keychain, missing entry, parse failure) so the
// caller falls through to the next resolution step. Without a
// registered keychain backend this always returns "" because
// [credentials.Retrieve] returns ErrCredentialUnsupported. The
// context is propagated to the backend so remote-store backends
// (Vault, SSM) honour the calling provider's deadline.
func retrieveFromKeychainRef(ctx context.Context, ref string) string {
	i := strings.Index(ref, "/")
	if i <= 0 || i == len(ref)-1 {
		return ""
	}

	service, account := ref[:i], ref[i+1:]

	secret, err := credentials.Retrieve(ctx, service, account)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(secret)
}
