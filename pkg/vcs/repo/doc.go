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
// Clone/push authentication is forge-aware: [NewRepo] takes package-owned
// [Settings], including release-source metadata, typed SSH settings, and a
// narrow token config. GTB config integration is isolated to adapter helpers
// such as [SettingsFromProps] and [SettingsFromContainable], which preserve the
// existing `<forge>.ssh`, `<forge>.auth`, and fallback environment-variable
// layout. Missing credentials are non-fatal for public repositories; private
// repositories require a token.
package repo
