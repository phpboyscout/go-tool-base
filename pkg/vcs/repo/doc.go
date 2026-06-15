// Package repo provides git repository operations (clone, checkout, commit,
// push, tree inspection) backed by go-git, behind the [RepoLike] abstraction
// with local-filesystem and in-memory storage backends.
//
// [RepoLike] is a composite built from focused role interfaces — [TreeReader],
// [Opener], [Authenticator], [WorktreeController], [Committer], [SourceState],
// [GitAccessor], and [Brancher]. Consumers should depend on the narrowest role
// that covers what they touch rather than the full composite. The named-remote
// operations CreateRemote/Remote remain concrete-only on [*Repo] and are not
// part of any role interface.
//
// The init-only primitives — [DiscoverRepository] (a read-only upward probe for
// an enclosing .git), [Repo.InitLocal] (init that refuses an existing repo with
// [ErrAlreadyRepository]), and [Repo.AddAll] (gitignore-aware staging) — form
// the [Initializer] role. It is deliberately NOT embedded in [RepoLike]:
// init/discovery and ignore-aware staging are first-commit concerns a
// clone/checkout consumer never needs, so a scaffold-time caller (the generator
// git step) depends on [Initializer] directly.
//
// Clone/push authentication is forge-aware: [NewRepo] reads credentials from
// the config subtree of the tool's forge (github, gitlab, bitbucket, gitea,
// codeberg) — `<forge>.ssh` for SSH auth, `<forge>.auth` plus the
// `<FORGE>_TOKEN` fallback environment variable for token auth. Missing
// credentials are non-fatal for public repositories; private repositories
// require a token.
package repo
