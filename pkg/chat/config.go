package chat

import "time"

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

// IsZero reports whether no credential config values were supplied.
func (c CredentialConfig) IsZero() bool {
	return c.Env == "" && c.Keychain == "" && c.Key == ""
}
