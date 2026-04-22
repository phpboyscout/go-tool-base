package config

import (
	"regexp"
	"strings"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/config"
)

// envVarNameRe matches the conservative POSIX env var shape shared
// with pkg/setup/ai, pkg/setup/github, and pkg/setup/bitbucket.
//
//nolint:gochecknoglobals // compiled once; pattern is a constant
var envVarNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

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
// the scanner to build a [literalCredential] entry from it.
type credentialDescriptor struct {
	key               string
	envTargetKey      string
	keychainTargetKey string
	keychainAccount   string
}

// knownCredentials enumerates every config key GTB recognises as a
// literal credential. Kept in sync with doctor's literalCredentialKeys
// — adding a new entry here also warrants a corresponding doctor
// check entry so the no-literal warning fires for it.
//
//nolint:gochecknoglobals // lookup table consumed read-only
var knownCredentials = []credentialDescriptor{
	{
		key:               chat.ConfigKeyClaudeKey,
		envTargetKey:      chat.ConfigKeyClaudeEnv,
		keychainTargetKey: chat.ConfigKeyClaudeKeychain,
		keychainAccount:   "anthropic.api",
	},
	{
		key:               chat.ConfigKeyOpenAIKey,
		envTargetKey:      chat.ConfigKeyOpenAIEnv,
		keychainTargetKey: chat.ConfigKeyOpenAIKeychain,
		keychainAccount:   "openai.api",
	},
	{
		key:               chat.ConfigKeyGeminiKey,
		envTargetKey:      chat.ConfigKeyGeminiEnv,
		keychainTargetKey: chat.ConfigKeyGeminiKeychain,
		keychainAccount:   "gemini.api",
	},
	{
		key:               "github.auth.value",
		envTargetKey:      "github.auth.env",
		keychainTargetKey: "github.auth.keychain",
		keychainAccount:   "github.auth",
	},
	{
		key:               "gitlab.auth.value",
		envTargetKey:      "gitlab.auth.env",
		keychainTargetKey: "gitlab.auth.keychain",
		keychainAccount:   "gitlab.auth",
	},
	{
		key:               "gitea.auth.value",
		envTargetKey:      "gitea.auth.env",
		keychainTargetKey: "gitea.auth.keychain",
		keychainAccount:   "gitea.auth",
	},
	{
		key:               "codeberg.auth.value",
		envTargetKey:      "codeberg.auth.env",
		keychainTargetKey: "codeberg.auth.keychain",
		keychainAccount:   "codeberg.auth",
	},
	{
		key:               "direct.auth.value",
		envTargetKey:      "direct.auth.env",
		keychainTargetKey: "direct.auth.keychain",
		keychainAccount:   "direct.auth",
	},
}

// bitbucketPrimary + bitbucketPartner describe the Bitbucket dual-
// credential pair. Split out so the scanner can detect and pair both
// halves; the keychain target is the shared `bitbucket.keychain`
// entry that holds a JSON blob.
//
//nolint:gochecknoglobals // lookup constants
var (
	bitbucketPrimary = credentialDescriptor{
		key:               "bitbucket.username",
		envTargetKey:      "bitbucket.username.env",
		keychainTargetKey: "bitbucket.keychain",
		keychainAccount:   "bitbucket.auth",
	}
	bitbucketPartner = credentialDescriptor{
		key:          "bitbucket.app_password",
		envTargetKey: "bitbucket.app_password.env",
		// Partner does not contribute its own keychain target — the
		// shared `bitbucket.keychain` entry covers both halves.
	}
)

// scanLiteralCredentials walks the loaded config and returns every
// literal credential with its destination metadata populated. The
// Bitbucket dual-credential pair is returned as a single entry; all
// other credentials are single-value entries.
//
// Empty / whitespace-only values are ignored so a cleared key does
// not surface as a migration candidate.
func scanLiteralCredentials(cfg config.Containable) []literalCredential {
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
func scanBitbucketPair(cfg config.Containable) *literalCredential {
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
