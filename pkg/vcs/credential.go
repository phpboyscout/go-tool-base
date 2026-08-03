package vcs

import (
	"context"
	"os"
	"strings"

	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go/credentials"
	forge "gitlab.com/phpboyscout/go/forge"
)

// GTB states forge-credential precedence here, once, and composes it from
// sources it owns. go/forge v0.8.0 moved ordering out of the module and into
// the consumer's configuration stack; this is GTB's stack.
//
// The documented order — unchanged from before the move — is:
//
//  1. {forge}.auth.env names an environment variable; that variable's value
//  2. {forge}.auth.keychain names a keychain entry; that entry's value
//  3. {forge}.auth.value literal
//  4. the well-known fallback environment variable (e.g. GITHUB_TOKEN)
//
// Rungs 1 and 2 are pointers, not values: the config names where the credential
// lives, and the source dereferences it. That indirection is the capability the
// layer model cannot express — an env layer can supply a credential, but it
// cannot say "read whichever variable this deployment names", which is how a CI
// job redirects a token without rewriting config.
//
// forge.ConfigCredential is deliberately not used. Its stale-key report exempts
// only the single key it was pointed at and probes the relative auth.env /
// auth.keychain — precisely the keys GTB ships defaults for — so pointed at
// auth.value while auth.env is set, it reports a working configuration as
// stale. See spec 0183 D10.

// ForgeCredential composes the credential chain for one forge over an
// already-scoped config reader.
//
// sub is the forge's subtree — vcs.ConfigFromReader(cfg).Sub("github") — so the
// per-forge namespacing is stated by the Sub and never repeated in a key
// literal here. A nil sub (absent section) is not an error: the environment
// fallback still applies, which is what lets a tool be configured purely by
// environment.
//
// Every rung is lazy. Nothing is read, and no keychain is touched, until the
// returned source is called — so a repository authenticating over SSH never
// triggers an unlock prompt for a token it does not need.
func ForgeCredential(sub forge.Config, fallbackEnv string) forge.CredentialSource {
	return forge.FirstCredential(
		envRefCredential(sub, authEnvKey),
		keychainCredential(sub, authKeychainKey),
		literalCredential(sub, authValueKey),
		forge.EnvCredential(fallbackEnv),
	)
}

// Relative to the forge subtree; the Sub supplies the "github." part.
const (
	authEnvKey      = "auth.env"
	authKeychainKey = "auth.keychain"
	authValueKey    = "auth.value"
)

// envRefCredential dereferences a config-named environment variable: the config
// holds the variable's NAME, and this reads its value.
func envRefCredential(sub forge.Config, key string) forge.CredentialSource {
	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		name := configString(sub, key)
		if name == "" {
			return "", nil
		}

		// A named variable that is unset is not an error: it falls through to
		// the next rung, which is how a developer machine and a CI runner share
		// one config file.
		return strings.TrimSpace(os.Getenv(name)), nil
	}
}

// keychainCredential resolves a "service/account" keychain reference.
//
// It is composed as a caller-supplied source rather than wired as a
// config-keychain layer precisely so it stays lazy: that layer resolves eagerly
// at store construction, which would put an unlock prompt on the startup path
// of commands needing no credential at all — a bounded hang on a headless box,
// a dialog on a desktop, for `--help`. See spec 0183 D7.
func keychainCredential(sub forge.Config, key string) forge.CredentialSource {
	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		ref := configString(sub, key)
		if ref == "" {
			return "", nil
		}

		service, account, ok := splitKeychainRef(ref)
		if !ok {
			return "", errors.Newf("malformed keychain reference %q: want \"service/account\"", ref)
		}

		value, err := credentials.Retrieve(ctx, service, account)
		if err != nil {
			return "", errors.Wrapf(err, "keychain lookup %q", ref)
		}

		return strings.TrimSpace(value), nil
	}
}

// literalCredential reads a credential written straight into config.
func literalCredential(sub forge.Config, key string) forge.CredentialSource {
	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		return configString(sub, key), nil
	}
}

// configString reads a key from a possibly-nil subtree. Sub returns nil when the
// section is absent, and a tool configured purely by environment may legitimately
// have no section at all.
func configString(sub forge.Config, key string) string {
	if sub == nil {
		return ""
	}

	return strings.TrimSpace(sub.GetString(key))
}

// splitKeychainRef parses the "service/account" form the setup wizards write
// (toolName + "/" + KeychainAccount). A bare account is rejected rather than
// guessed at: without a service there is nothing to look the entry up under,
// and silently choosing one would read the wrong entry.
func splitKeychainRef(ref string) (service, account string, ok bool) {
	idx := strings.Index(ref, "/")
	if idx <= 0 || idx == len(ref)-1 {
		return "", "", false
	}

	return ref[:idx], ref[idx+1:], true
}
