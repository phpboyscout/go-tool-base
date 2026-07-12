package chat

import (
	"context"
	"time"
)

// KeychainLookup resolves an OS-keychain (or remote secret-store) reference of
// the form "service/account" to its secret value. The host application injects
// it — go-tool-base wires pkg/credentials.Retrieve — so the chat core needs no
// keychain backend of its own. A nil lookup means the keychain resolution step
// is skipped (the caller falls through to the next credential source).
type KeychainLookup func(ctx context.Context, service, account string) (string, error)

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
	// Lookup resolves the Keychain reference. Injected by the host (nil ⇒ the
	// keychain step is skipped); never decoded from config.
	Lookup KeychainLookup `mapstructure:"-" json:"-"`
}

// IsZero reports whether no credential config values were supplied.
func (c CredentialConfig) IsZero() bool {
	return c.Env == "" && c.Keychain == "" && c.Key == ""
}
