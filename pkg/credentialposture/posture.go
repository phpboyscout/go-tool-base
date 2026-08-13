// Package credentialposture reports where a credential comes from, without
// ever reporting what it is.
//
// A credential's posture is three separate facts, and confusing them is what
// made the previous checks hard to act on:
//
//   - where it is STORED — an environment reference, a keychain entry, or a
//     literal in a config file;
//   - where it RESOLVES FROM — which of those actually supplied the value,
//     given a fixed precedence;
//   - what is SHADOWED — the lower-precedence copies that are still present
//     and would win if the one above them went away.
//
// `doctor` previously reported only the first, and only for a hardcoded list of
// keys. "A literal credential is in use" and "a literal credential is dead
// configuration underneath a working environment reference" therefore read
// identically, while being very different situations: one is an active
// exposure, the other is untidy. See spec 0189.
//
// Nothing here returns or renders a credential value. Every type is designed so
// that reporting posture cannot leak the secret whose posture it describes.
package credentialposture

import (
	"context"
	"fmt"
	"os"
	"strings"

	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"
)

// Origin names the rung that supplied a credential. It is a key name or a
// variable role — never a value — so it is safe to print.
type Origin string

const (
	// OriginNone means no rung produced a credential.
	OriginNone Origin = "none"
	// OriginEnvRef is the config-named environment variable, dereferenced.
	OriginEnvRef Origin = "auth.env"
	// OriginKeychain is the config-named keychain entry, dereferenced.
	OriginKeychain Origin = "auth.keychain"
	// OriginLiteral is the literal value in config.
	OriginLiteral Origin = "auth.value"
	// OriginFallbackEnv is the well-known fallback variable (e.g. GITHUB_TOKEN).
	OriginFallbackEnv Origin = "fallback environment variable"
)

// Reader is the narrow config surface a posture walk needs. Both
// `config.View` and `forge.Config` satisfy it, which is what lets this package
// serve forges and AI providers without depending on either.
type Reader interface {
	GetString(key string) string
}

// Descriptor declares one credential: which keys hold it, in which storage
// mode, and the well-known variable to fall back to.
//
// A bundle declares its own credential and the assembling code supplies the
// descriptor, so a new credential is covered by declaring it rather than by
// editing a list somewhere else. Three such lists existed before this — doctor's
// LiteralCredentialKeys, migrate's knownCredentials (whose own comment asks the
// reader to keep them in sync by hand), and the forge profiles.
type Descriptor struct {
	// Owner names the declaring bundle, e.g. "forge:github". It appears in
	// reports so a finding can be traced back to what declared it.
	Owner string
	// Label is the human name used in a report, e.g. "GitHub".
	Label string
	// EnvKey holds the NAME of an environment variable.
	EnvKey string
	// KeychainKey holds a "service/account" reference.
	KeychainKey string
	// LiteralKey holds the secret itself, in plaintext. Deprecated storage.
	LiteralKey string
	// FallbackEnv is the well-known variable tried when config says nothing.
	FallbackEnv string
}

// Rung is one step of the precedence chain: what it is, and how to read it.
type Rung struct {
	Origin Origin
	// Key is the config key this rung reads, empty for the fallback variable
	// (which is not configured anywhere).
	Key string
	// read returns the credential this rung supplies, or "" if it supplies
	// none.
	read func(ctx context.Context, cfg Reader, d Descriptor) (string, error)
}

// Read returns the credential this rung supplies, or "" if it supplies none.
//
// It exists so a caller that must actually *obtain* the credential — rather
// than report on it — can compose the same rungs in the same order instead of
// declaring the precedence a second time. pkg/vcs builds its forge credential
// chain this way, which is what stops the supplying path and the reporting path
// disagreeing about precedence.
func (r Rung) Read(ctx context.Context, cfg Reader, d Descriptor) (string, error) {
	return r.read(ctx, cfg, d)
}

// Rungs returns the precedence chain, declared once.
//
// Stating it here and having both the resolver and the shadow report walk this
// slice is what stops the two disagreeing — the property pkg/vcs's forgeRungs
// already relies on, kept when the logic was generalised.
func (d Descriptor) Rungs() []Rung {
	return []Rung{
		{Origin: OriginEnvRef, Key: d.EnvKey, read: readEnvRef},
		{Origin: OriginKeychain, Key: d.KeychainKey, read: readKeychain},
		{Origin: OriginLiteral, Key: d.LiteralKey, read: readLiteral},
		{Origin: OriginFallbackEnv, read: readFallbackEnv},
	}
}

// Shadow is a lower-precedence copy of a credential that is present but not in
// effect.
type Shadow struct {
	// Key is the config key still holding a copy.
	Key string
	// Origin is the storage mode that copy uses.
	Origin Origin
}

// Posture is one credential's resolved state, reported without its value.
type Posture struct {
	// Owner and Label come from the descriptor that declared the credential.
	Owner string
	Label string
	// Origin is the rung that supplied the credential, or OriginNone.
	Origin Origin
	// Key is the config key the winning rung read. Empty when the fallback
	// variable won or nothing resolved.
	Key string
	// Shadowed lists the lower-precedence copies still present, highest
	// precedence first.
	Shadowed []Shadow
}

// Deprecated reports whether the credential in effect is stored in a deprecated
// mode — a literal in a config file.
func (p Posture) Deprecated() bool { return p.Origin == OriginLiteral }

// String renders the posture for an operator.
//
// It is deliberately the only rendering path, so the never-log-values rule has
// one place to hold rather than one per call site.
func (p Posture) String() string {
	if p.Origin == OriginNone {
		return fmt.Sprintf("%s: no credential configured", p.Label)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "%s: resolves from %s", p.Label, p.Origin)

	if len(p.Shadowed) > 0 {
		keys := make([]string, 0, len(p.Shadowed))
		for _, s := range p.Shadowed {
			keys = append(keys, s.Key)
		}

		fmt.Fprintf(&b, "; shadowed copies still present in %s", strings.Join(keys, ", "))
	}

	return b.String()
}

// Resolve walks the precedence chain and reports which rung supplies the
// credential and which lower rungs still hold one.
//
// Error handling mirrors the forge resolver deliberately: a rung that fails
// does not stop the walk, because a later rung may still supply a working
// credential — and in that case the configuration genuinely does work. The
// retained error is returned only when nothing resolved, which is exactly when
// a bare "no credential" would otherwise hide the reason. That is what keeps a
// configured-but-broken credential diagnosed rather than reported as absent.
func Resolve(ctx context.Context, cfg Reader, d Descriptor) (Posture, error) {
	p := Posture{Owner: d.Owner, Label: d.Label, Origin: OriginNone}

	var (
		retained error
		rungs    = d.Rungs()
	)

	for i, rung := range rungs {
		value, err := rung.read(ctx, cfg, d)
		if err != nil {
			// A cancelled or expired context is about the caller, not the
			// configuration, and every later rung would fail the same way.
			if ctx.Err() != nil {
				return p, err
			}

			retained = errors.Join(retained, err)

			continue
		}

		if value == "" {
			continue
		}

		p.Origin = rung.Origin
		p.Key = rung.Key
		p.Shadowed = shadowsBelow(ctx, cfg, d, rungs[i+1:])

		return p, nil
	}

	return p, retained
}

// shadowsBelow reports which lower-precedence rungs still hold a credential.
//
// The fallback variable is excluded: it is not a copy anybody configured, and
// naming it as a shadow would tell an operator to go and remove something they
// did not put there. Only a rung backed by a config key is a copy in the sense
// that matters — one somebody has to delete.
func shadowsBelow(ctx context.Context, cfg Reader, d Descriptor, lower []Rung) []Shadow {
	var shadows []Shadow

	for _, rung := range lower {
		if rung.Key == "" {
			continue
		}

		value, err := rung.read(ctx, cfg, d)
		if err != nil || value == "" {
			continue
		}

		shadows = append(shadows, Shadow{Key: rung.Key, Origin: rung.Origin})
	}

	return shadows
}

// readEnvRef dereferences a config-named environment variable: the config holds
// the variable's NAME, and this reads its value.
func readEnvRef(ctx context.Context, cfg Reader, d Descriptor) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	name := configString(cfg, d.EnvKey)
	if name == "" {
		return "", nil
	}

	// A named variable that is unset is not an error: it falls through to the
	// next rung, which is how a developer machine and a CI runner share one
	// config file.
	return strings.TrimSpace(os.Getenv(name)), nil
}

// readKeychain resolves a "service/account" keychain reference.
//
// The caller is expected to bound the context — a keychain read can block on an
// OS unlock prompt, and a diagnostic must report rather than hang.
func readKeychain(ctx context.Context, cfg Reader, d Descriptor) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	ref := configString(cfg, d.KeychainKey)
	if ref == "" {
		return "", nil
	}

	service, account, ok := strings.Cut(ref, "/")
	if !ok || service == "" || account == "" {
		return "", errors.Newf("malformed keychain reference %q: want \"service/account\"", ref)
	}

	secret, err := credentials.Retrieve(ctx, service, account)
	if err != nil {
		return "", errors.Wrapf(err, "reading keychain entry %q", ref)
	}

	return strings.TrimSpace(secret), nil
}

// readLiteral reads the plaintext value from config.
func readLiteral(ctx context.Context, cfg Reader, d Descriptor) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return configString(cfg, d.LiteralKey), nil
}

// readFallbackEnv reads the well-known variable for this credential.
func readFallbackEnv(ctx context.Context, _ Reader, d Descriptor) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if d.FallbackEnv == "" {
		return "", nil
	}

	return strings.TrimSpace(os.Getenv(d.FallbackEnv)), nil
}

func configString(cfg Reader, key string) string {
	if cfg == nil || key == "" {
		return ""
	}

	return strings.TrimSpace(cfg.GetString(key))
}
