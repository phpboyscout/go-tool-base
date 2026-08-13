package forge

import (
	"context"
	"fmt"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

// Credential RESOLUTION had no observable surface. `doctor` reported where
// credentials are stored — the literal-credential check — and the setup wizards
// resolve one internally to decide whether to prompt, but nothing let an
// operator ask "does my forge credential actually resolve, and from which key?"
// The answer was only ever discoverable by running a command that authenticates
// and reading the failure.
//
// This is deliberately NOT the stale-key report spec 0183 OQ6 struck. That one
// told operators that auth.env and auth.keychain are "no longer read", which is
// false for GTB — it reads both — so it warned users away from a supported,
// wizard-written configuration. This reports what resolution DID: which rung
// won, or that none did. It never claims a supported key is unsupported, and it
// never prints a value.

// registerCredentialCheck wires one forge's resolution check, gated on its
// feature so a tool that never enables the forge is never told about it.
//
// Single-token profiles only. The dual-credential shape resolves a username and
// an app password from different keys entirely, so reporting it through
// [vcs.ResolveForgeCredentialOrigin] — which walks auth.env / auth.keychain /
// auth.value — would report "no credential configured" for a Bitbucket that is
// perfectly well configured. A dual-shape check is worth having and is not this.
func registerCredentialCheck(profile Profile) {
	// Declaration first, and NOT gated on the credential shape. A dual-
	// credential forge's username and app password are secrets in config
	// whether or not the single-token resolution check can report on them — and
	// gating the declaration on the check is how bitbucket.app_password would
	// have quietly stopped being warned about when the hardcoded key list was
	// retired. Spec 0189 R4.
	declareCredentials(profile)

	if profile.Credential != SingleToken {
		return
	}

	check := checkForgeCredential(profile)

	setup.RegisterChecks(profile.Feature, []setup.CheckProvider{
		func(*props.Props) []setup.CheckFunc { return []setup.CheckFunc{check} },
	})
}

// declareCredentials registers a forge's credential keys so every reporting
// surface — the resolution report, the literal-credential warning, and the
// support-bundle redaction — sees them without keeping a list of its own.
//
// The dual shape declares both halves: they are separate secrets under separate
// keys, and reporting one without the other would be a half-answer.
func declareCredentials(profile Profile) {
	if profile.Credential == SingleToken {
		credentialposture.Register(credentialposture.Descriptor{
			Owner:       "forge:" + profile.Provider,
			Label:       profile.Label + " credential",
			EnvKey:      profile.ConfigPrefix + ".auth.env",
			KeychainKey: profile.ConfigPrefix + ".auth.keychain",
			LiteralKey:  profile.ConfigPrefix + ".auth.value",
			FallbackEnv: profile.FallbackEnv,
		})

		return
	}

	// Both halves share the one keychain entry, which holds a JSON blob rather
	// than a bare secret — so neither declares a KeychainKey it could resolve
	// alone.
	credentialposture.Register(credentialposture.Descriptor{
		Owner:       "forge:" + profile.Provider,
		Label:       profile.Label + " username",
		EnvKey:      profile.ConfigPrefix + ".username.env",
		LiteralKey:  profile.ConfigPrefix + ".username",
		FallbackEnv: profile.UserFallbackEnv,
	})
	credentialposture.Register(credentialposture.Descriptor{
		Owner:       "forge:" + profile.Provider,
		Label:       profile.Label + " app password",
		EnvKey:      profile.ConfigPrefix + ".app_password.env",
		LiteralKey:  profile.ConfigPrefix + ".app_password",
		FallbackEnv: profile.PassFallbackEnv,
	})
}

// checkForgeCredential builds the resolution check for one forge.
//
// It is per-profile rather than one check over all forges so the result names
// the forge, and so a tool that enables only GitHub is not told about GitLab.
func checkForgeCredential(profile Profile) setup.CheckFunc {
	name := profile.Label + " credential"

	return func(ctx context.Context, p *props.Props) setup.CheckResult {
		if p == nil || p.Config == nil {
			return setup.CheckResult{Name: name, Status: "skip", Message: "no configuration loaded"}
		}

		sub := vcs.ConfigFromReader(p.Config.View()).Sub(profile.Provider)

		// No deadline around the whole resolve: the keychain rung bounds its own
		// read. A wrapper here with the same duration meant a locked keychain
		// exhausted the budget and the walk aborted before judging the rungs
		// below it, which hid the plaintext-fallback refusal behind a timeout.
		origin, err := vcs.ResolveForgeCredentialOrigin(ctx, sub, profile.FallbackEnv)

		switch {
		case err != nil:
			return setup.CheckResult{
				Name:    name,
				Status:  "warn",
				Message: "credential configured but does not resolve",
				Details: fmt.Sprintf(
					"%v. Precedence is auth.env, then auth.keychain, then auth.value, then %s.",
					err, profile.FallbackEnv),
			}

		case origin == vcs.OriginNone:
			// Not a failure: a tool may legitimately never talk to this forge.
			return setup.CheckResult{
				Name:    name,
				Status:  "skip",
				Message: "no credential configured",
				Details: fmt.Sprintf(
					"Run `init %s` to configure one, or set %s.", profile.ConfigPrefix, profile.FallbackEnv),
			}

		default:
			return setup.CheckResult{
				Name:    name,
				Status:  "pass",
				Message: fmt.Sprintf("resolves from %s", origin),
			}
		}
	}
}
