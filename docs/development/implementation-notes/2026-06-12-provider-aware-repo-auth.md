---
title: "Implementation notes — provider-aware repository auth"
description: "What was implemented for the 2026-06-12 provider-aware-repo-auth spec, deviations, and open questions for review."
date: 2026-06-13
tags:
  - implementation-notes
  - vcs
  - repo
  - auth
---

# Implementation notes — provider-aware repository auth

Spec: [2026-06-12-provider-aware-repo-auth](../specs/2026-06-12-provider-aware-repo-auth.md)
Branch: `feat/provider-aware-repo-auth`

## What was implemented

### D1 — Forge dimension (per-forge subtrees)

- `resolveForge` (`pkg/vcs/repo/repo.go`) derives the forge from `Tool.ReleaseSource.Type`, lowercased/trimmed, overridable by the `vcs.provider` config key (mirroring `pkg/setup/update.go`). Empty or `direct` falls back to `github` (O4: `direct` has no git remote, and `github` preserves the pre-change default).
- `NewRepo` selects `<forge>.ssh` vs token auth on the resolved forge instead of hard-coded `github.*`.
- `configureTokenAuth` resolves via the shared `vcs.ResolveToken(p.Config.Sub(forge), "<FORGE>_TOKEN")` primitive — same chain as before (`auth.env` → `auth.keychain` → `auth.value` → fallback env). The `githubvcs.GetGitHubToken` dependency was dropped from the repo layer (it hard-errored on missing tokens, conflicting with D2; `GetGitHubToken` itself is unchanged for its other callers).
- Basic-auth usernames are forge-mapped: `x-access-token` (GitHub + unknown forges), `oauth2` (GitLab), `x-token-auth` (Bitbucket).

### D2 — Missing auth non-fatal for public repos

- No credential + `ReleaseSource.Private: false` → DEBUG log, `nil` auth, no error.
- No credential + `Private: true` → error with a hint naming `<FORGE>_TOKEN` and `<forge>.auth.env` (mirrors `requireReleaseToken` in the release layer).

### D3 — SSH auth generalised and library-pure

- `configureSSHAuth` reads `<forge>.ssh.key` (subtree-nil guard from MR !63 retained, log messages generalised with a `forge` keyval).
- `GetSSHKey` no longer imports or invokes `huh`. Passphrase detection uses `errors.As` against the typed `*gossh.PassphraseMissingError`; the error is wrapped with a hint and propagates out of `NewRepo`.
- New additive API `GetSSHKeyWithPassphrase(filePath, fs, passphrase)` for callers that obtained the passphrase. `GetSSHKey(path, fs)` is now a zero-passphrase shim over it — signature unchanged.

### D4 — Guards on all RepoLike methods

- `ErrNoWorktree` guards: `Checkout`, `CheckoutCommit`, `Commit`, `CreateBranch` (tree leg).
- `ErrNoRepository` guards: `Push`, `CreateBranch`, `CreateRemote`, `Remote`, and (via a new shared `headTree` helper) `WalkTree`, `FileExists`, `DirectoryExists`, `GetFile`.
- `AddToFS` needs no guard (does not touch `r.repo`/`r.tree`). `ThreadSafeRepo` inherits all guards by delegation.
- `RepoLike` itself is **unchanged** — no mock regeneration was needed.

### Tests

- `pkg/vcs/repo/provider_auth_unit_test.go`: table-driven forge/token matrix (GitLab subtree selection, GitHub back-compat, `vcs.provider` override, public-no-token, private-no-token errors, gitea fallback env, bitbucket username, empty/`direct` defaults), per-forge SSH subtree selection, typed-passphrase tests (encrypted ed25519 fixture generated via `gossh.MarshalPrivateKeyWithPassphrase`), and a no-panic sentinel sweep over every `RepoLike` method on both `Repo` and `ThreadSafeRepo`.
- `TestForgeAuthClonePush` (integration, `INT_TEST_VCS=1`): clone/commit/push against a local bare remote with GitHub- and GitLab-shaped token configs.
- Updated `TestRepo_Unit_NewRepo_TokenAuthFails` to set `Private: true` (the old expectation contradicted D2).

### Docs

- `docs/components/vcs/repo.md` — forge-aware auth table, non-fatal-public behaviour, `GetSSHKeyWithPassphrase`, CLI-prompt responsibility, unopened-repo contract.
- `docs/concepts/vcs-repositories.md` — forge-aware auth table + scope-boundary note (repo auth ≠ release auth).
- `docs/migration/v0.16-repo-provider-auth.md` (+ index row) — the two behaviour changes.
- `pkg/vcs/repo/doc.go` — package comment rewritten (it previously described URL parsing, which this package never did).

## Deviations from the spec

- **None substantive.** One judgement call: the spec's "private repos require/enforce a token" is enforced at `NewRepo` time on the token path only. An SSH-configured private repo is not pre-checked (SSH agent presence can't be cheaply validated), matching the previous behaviour.
- `CreateBranch` was lightly refactored (`branchExists` helper) to stay under the cyclop threshold after adding the guards.

## Breaking-change assessment

No public **signature** changed. Two **behaviour** changes, called out with a `BREAKING CHANGE:` footer in the commit and a migration note:

1. `GetSSHKey` no longer interactively prompts for passphrases (returns the typed error instead).
2. `NewRepo` no longer errors when no token is configured for a public repository.

## Open questions for review

1. **No CLI call site for the passphrase prompt was found.** Nothing in this repository (commands, setup, init, train) calls `GetSSHKey` or hits the encrypted-key path — the `huh` prompt was effectively dead interactive code inside the library. I therefore did *not* relocate the prompt anywhere; the typed error + `GetSSHKeyWithPassphrase` retry pattern is documented in `docs/components/vcs/repo.md` and the migration note as the CLI layer's responsibility. If a downstream tool needs a ready-made prompt helper, should one be added to `pkg/forms`?
2. **Basic-auth usernames per forge**: GitLab `oauth2`, Bitbucket `x-token-auth`, everything else `x-access-token`. Gitea/Codeberg accept a token as the basic-auth password with an arbitrary username, so they use the default — worth confirming against a real Gitea instance (no integration test covers gitea/codeberg HTTP auth shapes).
3. **Bitbucket app-password mode**: the release layer special-cases Bitbucket (username + app password); the repo layer only supports the single-token (`x-token-auth`) shape. Is repo-layer app-password support needed, or is token auth sufficient for git-over-HTTPS?
4. **Fallback env for the forge override**: when `vcs.provider` overrides the type, the fallback env var follows the override (e.g. `GITLAB_TOKEN`), consistent with `requireReleaseToken`. Confirm that is the intended interaction.
5. **Integration coverage**: `TestForgeAuthClonePush` exercises GitHub/GitLab token shapes against a local bare remote (file transport, so credentials are constructed but not validated by a server). Real HTTP-auth round-trips would need a local smart-HTTP server — worth a follow-up?
