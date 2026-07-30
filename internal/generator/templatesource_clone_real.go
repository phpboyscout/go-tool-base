package generator

// The production git template-source clone, wired on top of pkg/vcs/repo so a
// git source reuses the exact provider-aware clone/auth path the rest of GTB
// uses (resolveForge, <forge>.auth/<forge>.ssh, vcs.ResolveToken, non-fatal
// public clone) — no second auth path. The fetch is inert: go-git's
// PlainClone runs no hooks and no filter/smudge, and submodules are not
// recursed (WithRecurseSubmodules is deliberately not passed).

import (
	"context"

	"github.com/cockroachdb/errors"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"gitlab.com/phpboyscout/go/repo"

	gtbrepo "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/repo"
)

// EnableRealTemplateClone wires the production provider-aware git clone for
// custom template sources. Call this on a Generator before resolving git
// sources in production paths; tests inject a fake via WithTemplateClone
// instead. Returns the Generator for chaining.
func (g *Generator) EnableRealTemplateClone() *Generator {
	g.cloneTemplate = g.realCloneTemplate

	return g
}

// realCloneTemplate clones a git template source into req.TargetDir at req.Ref
// using the provider-aware auth resolved from the tool's forge config, then
// resolves and checks out the concrete commit, returning the resolved SHA.
func (g *Generator) realCloneTemplate(req cloneRequest) (cloneResult, error) {
	r, err := gtbrepo.NewRepoFromProps(g.props)
	if err != nil {
		return cloneResult{}, errors.Wrap(err, "failed to initialise template repo auth")
	}

	// Clone without tags; do NOT recurse submodules (inert fetch). A single
	// branch is fetched when a branch ref is given.
	opts := []repo.CloneOption{repo.WithNoTags()}
	if req.Ref != "" {
		opts = append(opts, repo.WithSingleBranch(req.Ref))
	}

	// context.Background(): the template clone carried no context before the
	// go/repo API added a ctx parameter; Background preserves that behaviour.
	if _, _, err := r.Clone(context.Background(), req.URL, req.TargetDir, opts...); err != nil {
		return cloneResult{}, errors.Wrap(err, "failed to clone template source")
	}

	sha, err := resolveAndCheckout(r, req.Ref)
	if err != nil {
		return cloneResult{}, err
	}

	return cloneResult{ResolvedSHA: sha}, nil
}

// resolveAndCheckout resolves ref (branch/tag/commit, or "" for HEAD) to a
// concrete commit, checks it out, and returns the hex SHA.
func resolveAndCheckout(r *repo.Repo, ref string) (string, error) {
	var sha string

	err := r.WithRepo(func(gr *git.Repository) error {
		hash, resolveErr := resolveRevisionHash(gr, ref)
		if resolveErr != nil {
			return resolveErr
		}

		sha = hash.String()

		return nil
	})
	if err != nil {
		return "", err
	}

	if checkoutErr := r.CheckoutCommit(plumbing.NewHash(sha)); checkoutErr != nil {
		return "", errors.Wrapf(checkoutErr, "failed to check out resolved commit %s", sha)
	}

	return sha, nil
}

// resolveRevisionHash resolves ref to a commit hash. An empty ref resolves
// HEAD (the default branch the clone landed on).
func resolveRevisionHash(gr *git.Repository, ref string) (plumbing.Hash, error) {
	if ref == "" {
		head, err := gr.Head()
		if err != nil {
			return plumbing.ZeroHash, errors.Wrap(err, "failed to resolve template HEAD")
		}

		return head.Hash(), nil
	}

	hash, err := gr.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return plumbing.ZeroHash, errors.Wrapf(err, "failed to resolve template ref %q", ref)
	}

	return *hash, nil
}
