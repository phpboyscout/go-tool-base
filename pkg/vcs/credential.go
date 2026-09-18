package vcs

import (
	"context"

	forge "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
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
	desc := forgeDescriptor(fallbackEnv)
	reader := readerFor(sub)

	// One walk, not a chain of independent sources. The secure-store invariant
	// (spec 0189 R5/D7) is a judgement ACROSS rungs — a keychain that was
	// configured and failed, with a plaintext copy below it — which a chain of
	// first-non-empty sources cannot express, because each source only knows
	// about itself. Composing them here would have left the supplying path
	// falling through to the literal while the reporting path refused it.
	return func(ctx context.Context) (string, error) {
		value, _, err := credentialposture.ResolveCredential(ctx, reader, desc)

		return value, err
	}
}

// CredentialOrigin names the rung that supplied a credential. It is an alias
// for the general vocabulary in pkg/credentialposture rather than a second
// copy: forges and AI providers report the same facts, and two enumerations
// that mean the same thing eventually disagree. Spec 0189 D4.
type CredentialOrigin = credentialposture.Origin

const (
	// OriginNone means no rung produced a credential.
	OriginNone = credentialposture.OriginNone
	// OriginEnvRef is {forge}.auth.env, dereferenced.
	OriginEnvRef = credentialposture.OriginEnvRef
	// OriginKeychain is {forge}.auth.keychain, dereferenced.
	OriginKeychain = credentialposture.OriginKeychain
	// OriginLiteral is the {forge}.auth.value literal.
	OriginLiteral = credentialposture.OriginLiteral
	// OriginFallbackEnv is the well-known fallback variable (e.g. GITHUB_TOKEN).
	OriginFallbackEnv = credentialposture.OriginFallbackEnv
)

// forgeDescriptor states the forge credential shape in the shared vocabulary.
//
// The keys are relative because sub is already scoped to the forge — the Sub
// supplies the "github." part — so the per-forge namespacing is stated by the
// caller and never repeated in a key literal here.
func forgeDescriptor(fallbackEnv string) credentialposture.Descriptor {
	return credentialposture.Descriptor{
		Owner:       "forge",
		EnvKey:      authEnvKey,
		KeychainKey: authKeychainKey,
		LiteralKey:  authValueKey,
		FallbackEnv: fallbackEnv,
	}
}

// ResolveForgeCredentialOrigin reports WHICH rung supplies a forge's credential,
// without returning the credential itself.
//
// It exists so a diagnostic can tell an operator that their configuration
// resolves, and from where, without printing a secret to a terminal or a support
// bundle. "It resolves, from auth.env" and "nothing resolves" are the two facts
// worth having, and neither needs the value.
//
// The walk itself lives in pkg/credentialposture, shared with every other
// credential GTB reports on. Error handling is that package's: a rung that
// fails does not stop the walk, because a later rung may still supply a working
// credential — and the retained error is returned only when nothing resolved,
// which is exactly when a bare "no credential" would otherwise hide the reason.
func ResolveForgeCredentialOrigin(
	ctx context.Context,
	sub forge.Config,
	fallbackEnv string,
) (CredentialOrigin, error) {
	posture, err := credentialposture.Resolve(ctx, readerFor(sub), forgeDescriptor(fallbackEnv))

	return posture.Origin, err
}

// readerFor adapts a forge subtree to the narrow reader the shared walk needs.
// A nil sub (absent section) is not an error: the environment fallback still
// applies, which is what lets a tool be configured purely by environment.
func readerFor(sub forge.Config) credentialposture.Reader {
	if sub == nil {
		return nil
	}

	return sub
}

// CredentialOptions hands a provider factory GTB's credential chain for the
// endpoint's section of cfg, so the factory consults it and nothing else
// (go/forge spec 0025 D2, spec 0026): auth.env and auth.keychain are
// dereferenced by GTB, never read by the factory, and the construction
// context bounds the resolution. cfg is the ROOT configuration, as the
// factory receives it.
//
// A single-token forge gets its chain through forge.WithCredential and no
// username; a factory ignores forge.WithUsername it has no use for. Bitbucket
// authenticates with two halves, so it gets both: the username through
// forge.WithUsername and the app password through forge.WithCredential, each
// walking its own env-reference and literal keys and the one keychain entry
// that holds both, read once.
func CredentialOptions(endpoint forge.Endpoint, cfg forge.Config, fallbackEnv string) []forge.Option {
	sub := endpoint.Section(cfg)

	if endpoint.Type == forge.SourceTypeBitbucket {
		username, password := bitbucketSources(sub, bitbucketUserFallbackEnv, bitbucketPassFallbackEnv)

		return []forge.Option{forge.WithUsername(username), forge.WithCredential(password)}
	}

	return []forge.Option{forge.WithCredential(ForgeCredential(sub, fallbackEnv))}
}

// The well-known variables for Bitbucket's two halves; the profile in
// pkg/setup/forge names the same two, and the config bundle points its env
// references at them by default.
const (
	bitbucketUserFallbackEnv = "BITBUCKET_USERNAME"
	bitbucketPassFallbackEnv = "BITBUCKET_APP_PASSWORD" //nolint:gosec // G101: the variable's NAME, not a credential
)

// ConfigKeyProvider overrides the release-source forge a tool was built with.
const ConfigKeyProvider = "vcs.provider"

// Relative to the forge subtree; the Sub supplies the "github." part.
const (
	authEnvKey      = "auth.env"
	authKeychainKey = "auth.keychain"
	authValueKey    = "auth.value"
)
