package forge

import (
	"context"
	"fmt"

	"gitlab.com/phpboyscout/go/credentials"

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
	if profile.Credential != SingleToken {
		return
	}

	check := checkForgeCredential(profile)

	setup.RegisterChecks(profile.Feature, []setup.CheckProvider{
		func(*props.Props) []setup.CheckFunc { return []setup.CheckFunc{check} },
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

		// The same bound deadline the setup wizard uses for its own resolve: a
		// keychain rung may block on an OS unlock prompt, and `doctor` must
		// report rather than hang.
		resolveCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
		defer cancel()

		origin, err := vcs.ResolveForgeCredentialOrigin(resolveCtx, sub, profile.FallbackEnv)

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
