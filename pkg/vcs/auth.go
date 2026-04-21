package vcs

import (
	"os"
	"strings"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

// ResolveToken resolves an authentication token from a config subtree.
//
// Resolution order:
//
//  1. cfg.auth.env      — NAME of an environment variable to read
//  2. cfg.auth.keychain — "<service>/<account>" reference in the OS
//     keychain; active only when the process has imported
//     pkg/credentials/keychain (or otherwise registered a backend).
//     Silently skipped when no backend is registered or when the
//     keychain is unreachable at the time of the call.
//  3. cfg.auth.value    — literal token value stored in config.
//     Viper's AutomaticEnv + tool prefix also surfaces
//     <TOOL>_AUTH_VALUE style env vars via this step.
//  4. fallbackEnv       — well-known unprefixed environment variable
//     (pass "" to skip).
//
// Returns an empty string when no token is found; callers decide
// whether that is an error condition (e.g. private repositories
// require a token, public repositories can proceed without one).
//
// The callers-supplied `cfg` is typically `props.Config.Sub("<vcs>")`
// — GTB's env-aware Sub ensures prefix-aware env binding fires even
// inside sub-containers (see pkg/config).
func ResolveToken(cfg config.Containable, fallbackEnv string) string {
	if token := tokenFromConfig(cfg); token != "" {
		return token
	}

	if fallbackEnv != "" {
		return os.Getenv(fallbackEnv)
	}

	return ""
}

// tokenFromConfig walks the three config-based steps of the resolution
// chain. Each step short-circuits on a non-empty value; empty or
// whitespace-only results fall through so a partially-populated entry
// cannot mask a fully-populated one at a lower priority.
func tokenFromConfig(cfg config.Containable) string {
	if cfg == nil {
		return ""
	}

	if v := tokenFromEnvRef(cfg); v != "" {
		return v
	}

	if v := tokenFromKeychain(cfg); v != "" {
		return v
	}

	return strings.TrimSpace(cfg.GetString("auth.value"))
}

// tokenFromEnvRef reads the env-var NAME recorded in auth.env and
// returns the value of that env var, or empty string.
func tokenFromEnvRef(cfg config.Containable) string {
	name := strings.TrimSpace(cfg.GetString("auth.env"))
	if name == "" {
		return ""
	}

	return strings.TrimSpace(os.Getenv(name))
}

// tokenFromKeychain reads the "<service>/<account>" reference in
// auth.keychain and returns the stored secret. Without the
// pkg/credentials/keychain subpackage imported, [credentials.Retrieve]
// always returns [credentials.ErrCredentialUnsupported] and the
// caller falls through to the literal-config step. Errors are
// intentionally swallowed — the returned empty string is the
// fall-through signal, and we must not leak raw backend error text
// (which can include paths or partial credential hints).
//
// R3 distinction: ErrCredentialNotFound also falls through silently.
// Corrupted-entry errors (JSON unmarshal for Bitbucket blob) are
// handled by the caller, not here — this function is for single-
// value secrets only.
func tokenFromKeychain(cfg config.Containable) string {
	ref := strings.TrimSpace(cfg.GetString("auth.keychain"))
	if ref == "" {
		return ""
	}

	service, account, ok := parseKeychainRef(ref)
	if !ok {
		return ""
	}

	secret, err := credentials.Retrieve(service, account)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(secret)
}

// parseKeychainRef splits a keychain reference of the form
// "<service>/<account>" into its components. The account portion
// may itself contain "/" — only the first "/" is treated as the
// separator, so "mytool/github.auth" and
// "mytool/bitbucket/auth.blob" both parse correctly.
func parseKeychainRef(ref string) (service, account string, ok bool) {
	i := strings.Index(ref, "/")
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}

	return ref[:i], ref[i+1:], true
}
