package config

import (
	"strings"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// literalCredential describes a single credential the scanner
// discovered in the config, along with the destination keys and
// keychain account to use when migrating it.
//
// Dual-credential pairs (Bitbucket username + app_password today)
// surface as a single entry with PartnerKey / PartnerValue /
// PartnerEnvTargetKey populated — the migrator treats them as an
// atomic unit.
type literalCredential struct {
	// Key is the source config key currently holding the literal
	// credential (e.g. `anthropic.api.key`, `github.auth.value`).
	Key string

	// Value is the literal secret currently stored at Key. Never
	// logged or embedded in user-facing messages.
	Value string

	// EnvTargetKey is the destination config key written when
	// migrating to env-var mode (e.g. `anthropic.api.env`).
	EnvTargetKey string

	// KeychainTargetKey is the destination config key written when
	// migrating to keychain mode (e.g. `anthropic.api.keychain`,
	// `bitbucket.keychain`).
	KeychainTargetKey string

	// KeychainAccount is the account portion of the
	// `<service>/<account>` reference written to KeychainTargetKey.
	// Matches the account strings used by the setup wizards so
	// migrate and wizard share a single keychain entry per
	// credential.
	KeychainAccount string

	// PartnerKey, set only for dual-credential pairs, points at the
	// second half (e.g. `bitbucket.app_password` when Key is
	// `bitbucket.username`). Zero value means "single-value credential".
	PartnerKey string

	// PartnerEnvTargetKey is the env-target key for the partner
	// field. Only set when PartnerKey is set.
	PartnerEnvTargetKey string
}

// credentialDescriptor captures enough about a known credential for
// the scanner to build a [literalCredential] entry from it, and for the
// migration to name the env var and keychain blob field it writes.
type credentialDescriptor struct {
	key               string
	envTargetKey      string
	keychainTargetKey string
	keychainAccount   string
	// fallbackEnv is the upstream-standard env var an env-mode migration
	// suggests for the key (GITHUB_TOKEN, ANTHROPIC_API_KEY).
	fallbackEnv string
	// blobField names the key's field in a shared keychain JSON blob; empty
	// for a credential stored on its own.
	blobField string
}

// knownCredentials enumerates every config key GTB recognises as a
// literal credential: one row per chat provider credential root, derived
// the way chat derives its own keys, and one per single-token forge.
var knownCredentials = append(chatCredentials(),
	forgeCredential("github", "GITHUB_TOKEN"),
	forgeCredential("gitlab", "GITLAB_TOKEN"),
	forgeCredential("gitea", "GITEA_TOKEN"),
	forgeCredential("codeberg", "CODEBERG_TOKEN"),
	forgeCredential("direct", "DIRECT_TOKEN"),
)

func chatCredentials() []credentialDescriptor {
	keys := chat.ProviderCredentialKeys()
	out := make([]credentialDescriptor, 0, len(keys))

	for _, k := range keys {
		out = append(out, credentialDescriptor{
			key:               k.Literal,
			envTargetKey:      k.Env,
			keychainTargetKey: k.Keychain,
			keychainAccount:   k.Root,
			fallbackEnv:       k.FallbackEnv,
		})
	}

	return out
}

func forgeCredential(prefix, fallbackEnv string) credentialDescriptor {
	keys := credentialposture.SingleToken(prefix)

	return credentialDescriptor{
		key:               keys.Literal,
		envTargetKey:      keys.Env,
		keychainTargetKey: keys.Keychain,
		keychainAccount:   keys.Account,
		fallbackEnv:       fallbackEnv,
	}
}

// bitbucketPrimary + bitbucketPartner describe the Bitbucket dual-
// credential pair. Split out so the scanner can detect and pair both
// halves; the keychain target is the shared `bitbucket.keychain`
// entry that holds a JSON blob.
var (
	bitbucketKeys    = credentialposture.DualCredential("bitbucket")
	bitbucketPrimary = credentialDescriptor{
		key:               bitbucketKeys.User,
		envTargetKey:      bitbucketKeys.UserEnv,
		keychainTargetKey: bitbucketKeys.Keychain,
		keychainAccount:   bitbucketKeys.Account,
		fallbackEnv:       "BITBUCKET_USERNAME",
		blobField:         "username",
	}
	bitbucketPartner = credentialDescriptor{
		key:          bitbucketKeys.Password,
		envTargetKey: bitbucketKeys.PasswordEnv,
		fallbackEnv:  "BITBUCKET_APP_PASSWORD",
		blobField:    "app_password",
		// Partner does not contribute its own keychain target: the
		// shared `bitbucket.keychain` entry covers both halves.
	}
)

// knownCredential finds the descriptor for a key, the Bitbucket halves
// included.
func knownCredential(key string) (credentialDescriptor, bool) {
	for _, c := range knownCredentials {
		if key == c.key {
			return c, true
		}
	}

	for _, c := range []credentialDescriptor{bitbucketPrimary, bitbucketPartner} {
		if key == c.key {
			return c, true
		}
	}

	return credentialDescriptor{}, false
}

// scanLiteralCredentials walks the loaded config and returns every
// literal credential with its destination metadata populated. The
// Bitbucket dual-credential pair is returned as a single entry; all
// other credentials are single-value entries.
//
// Empty / whitespace-only values are ignored so a cleared key does
// not surface as a migration candidate.
func scanLiteralCredentials(cfg config.Reader) []literalCredential {
	var out []literalCredential

	for _, d := range knownCredentials {
		if v := strings.TrimSpace(cfg.GetString(d.key)); v != "" {
			out = append(out, literalCredential{
				Key:               d.key,
				Value:             v,
				EnvTargetKey:      d.envTargetKey,
				KeychainTargetKey: d.keychainTargetKey,
				KeychainAccount:   d.keychainAccount,
			})
		}
	}

	if bb := scanBitbucketPair(cfg); bb != nil {
		out = append(out, *bb)
	}

	return out
}

// scanBitbucketPair returns a dual-credential literalCredential when
// EITHER half of the Bitbucket username / app_password pair is set.
// The caller is responsible for handling an asymmetric pair (only
// one half set) gracefully — the keychain target requires both, so
// the migrator reports a partial-pair skip.
//
// When only one half is present we still emit the entry so the
// migrator can tell the user what's missing; this keeps the output
// discoverable rather than silently dropping half a configuration.
func scanBitbucketPair(cfg config.Reader) *literalCredential {
	user := strings.TrimSpace(cfg.GetString(bitbucketPrimary.key))
	pw := strings.TrimSpace(cfg.GetString(bitbucketPartner.key))

	if user == "" && pw == "" {
		return nil
	}

	return &literalCredential{
		Key:                 bitbucketPrimary.key,
		Value:               user,
		EnvTargetKey:        bitbucketPrimary.envTargetKey,
		KeychainTargetKey:   bitbucketPrimary.keychainTargetKey,
		KeychainAccount:     bitbucketPrimary.keychainAccount,
		PartnerKey:          bitbucketPartner.key,
		PartnerEnvTargetKey: bitbucketPartner.envTargetKey,
	}
}
