package setup

import (
	"context"
	"time"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

// explainRefusal turns a forge's refusal into something a user can act on.
//
// Every release call used to be wrapped as "failed to get latest release", so a
// rate-limited host, a lapsed token, a private repository the token cannot see
// and a tool whose first release has not been cut were one sentence. Each of
// those has a different next step, and the flat message named none of them.
//
// The sentinels come from forge's refusal set (spec 0193 D5). An error carrying
// none of them is returned untouched: inventing a hint for an error we have not
// classified would be worse than the wrapper it replaces.
func (s *SelfUpdater) explainRefusal(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	_, owner, repo := s.Tool.GetReleaseSource()

	switch {
	// Checked before ErrNotFound: "the repository is not there" and "the
	// repository is there and has no release yet" are the two most easily
	// confused outcomes, and a new tool hits the second one constantly.
	case errors.Is(err, forge.ErrReleaseNotFound):
		return errors.WithHintf(err,
			"%s/%s has no release to update to yet. This is the expected answer for a tool "+
				"whose first release has not been cut; it is not a configuration problem.",
			owner, repo)

	case errors.Is(err, forge.ErrNotFound):
		return errors.WithHintf(err,
			"%s/%s was not found on %s. Check the tool's release source: the owner or "+
				"repository is wrong, or the repository is private and the credential cannot "+
				"see it.", owner, repo, s.endpointLabel())

	case errors.Is(err, forge.ErrUnauthorized):
		return errors.WithHintf(err,
			"The credential %s is not valid. It is absent, expired, or revoked. "+
				"Run `%s doctor` to see every rung of the chain and which one answers.",
			s.credentialOrigin(ctx), s.Tool.Name)

	// Distinct from Unauthorized on purpose: the token works, so re-issuing it
	// changes nothing. The scope is what is missing.
	case errors.Is(err, forge.ErrForbidden):
		return errors.WithHintf(err,
			"The credential is valid but lacks permission for %s/%s. Re-issuing it will not "+
				"help; grant it read access to that repository's releases, or check whether "+
				"the repository is private and the token's scope covers private repositories.",
			owner, repo)

	case errors.Is(err, forge.ErrRateLimited):
		if retry, ok := forge.RetryAfter(err); ok {
			return errors.WithHintf(err,
				"%s is rate limiting this client. It asked for a wait of %s. An "+
					"authenticated request usually has a far higher limit than an anonymous "+
					"one, so configuring a credential may be the real fix.",
				s.endpointLabel(), retry.Round(time.Second))
		}

		return errors.WithHintf(err,
			"%s is rate limiting this client, and did not say for how long. An authenticated "+
				"request usually has a far higher limit than an anonymous one, so configuring "+
				"a credential may be the real fix.", s.endpointLabel())
	}

	return err
}

// endpointLabel names the forge in a message: its host where one is configured,
// otherwise the source type. Enterprise installs are the case that matters —
// "github.com is rate limiting you" is actively misleading on a GHE host.
func (s *SelfUpdater) endpointLabel() string {
	if s.endpoint.Host != "" {
		return s.endpoint.Host
	}

	if s.endpoint.Type != "" {
		return s.endpoint.Type
	}

	return "the release host"
}

// credentialOrigin names the rung that supplied the rejected credential, in the
// same vocabulary `doctor` uses so the two agree rather than describing one
// chain two ways.
//
// It resolves the chain a second time, which is deliberate but not free: rung 2
// dereferences a keychain entry, so on a keychain-backed configuration this can
// raise an unlock prompt. That is acceptable HERE and nowhere else — an
// unauthorized refusal is terminal, the user is about to be told their
// credential is wrong, and naming the wrong rung sends them to edit a file that
// was never consulted. Everywhere else the chain stays lazy (spec 0189).
//
// A resolution failure is not propagated: the caller already has a refusal to
// report, and losing it behind a second error would be a poor trade.
func (s *SelfUpdater) credentialOrigin(ctx context.Context) string {
	if s.authConfig == nil {
		return "for this release source"
	}

	origin, err := vcs.ResolveForgeCredentialOrigin(ctx, s.authConfig, s.fallbackEnv)
	if err != nil || origin == vcs.OriginNone {
		return "for this release source"
	}

	return "resolved from " + string(origin)
}
