// Package repo provides git repository operations (clone, checkout, commit,
// push, tree inspection) backed by go-git, behind the [RepoLike] abstraction
// with local-filesystem and in-memory storage backends.
//
// Clone/push authentication is forge-aware: [NewRepo] reads credentials from
// the config subtree of the tool's forge (github, gitlab, bitbucket, gitea,
// codeberg) — `<forge>.ssh` for SSH auth, `<forge>.auth` plus the
// `<FORGE>_TOKEN` fallback environment variable for token auth. Missing
// credentials are non-fatal for public repositories; private repositories
// require a token.
package repo
