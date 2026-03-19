package vcs

import (
	"os"

	"github.com/phpboyscout/gtb/pkg/config"
)

// ResolveToken resolves an authentication token from a config subtree.
// Resolution order:
//  1. cfg.auth.env  — name of an environment variable to read
//  2. cfg.auth.value — literal token value stored in config
//  3. fallbackEnv  — a well-known environment variable (pass "" to skip)
//
// Returns an empty string when no token is found; callers decide whether
// that is an error condition (e.g. private repositories require a token,
// public repositories can proceed without one).
func ResolveToken(cfg config.Containable, fallbackEnv string) string {
	if token := tokenFromConfig(cfg); token != "" {
		return token
	}

	if fallbackEnv != "" {
		return os.Getenv(fallbackEnv)
	}

	return ""
}

// tokenFromConfig reads auth.env and auth.value from the config subtree.
func tokenFromConfig(cfg config.Containable) string {
	if cfg == nil {
		return ""
	}

	if cfg.Has("auth.env") {
		if token := os.Getenv(cfg.GetString("auth.env")); token != "" {
			return token
		}
	}

	if cfg.Has("auth.value") {
		return cfg.GetString("auth.value")
	}

	return ""
}
