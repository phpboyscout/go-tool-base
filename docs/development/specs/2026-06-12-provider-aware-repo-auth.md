---
title: "Provider-aware repository auth — stop hard-coding GitHub for clone/push credentials"
description: "pkg/vcs/repo.NewRepo resolves clone/push auth from GitHub config only and hard-fails non-GitHub tools, even though the release-provider layer already has a clean multi-provider abstraction. Add a provider dimension to git auth via vcs.ResolveToken and make missing auth non-fatal for public repos."
status: IMPLEMENTED
date: 2026-06-12
tags:
  - specification
  - vcs
  - repo
  - auth
  - git
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-fable-5)
    role: AI drafting assistant
---

# Provider-aware repository auth

Authors
:   Matt Cockayne, Claude (claude-fable-5) *(AI drafting assistant)*

Date
:   2026-06-12

Status
:   IMPLEMENTED (open questions resolved in review 2026-06-12)

## Summary

`pkg/vcs/repo.NewRepo` configures git clone/push authentication from the `github.*` config subtree only (`configureSSHAuth` reads `github.ssh.key`; `configureTokenAuth` reads `GetGitHubToken(Config.Sub("github"))`). A tool whose repository lives on GitLab, Bitbucket, Gitea, or Codeberg either gets GitHub auth applied (wrong) or hard-fails. This spec generalises the repo layer's **forge-based** auth from GitHub-only to all supported forges, using the existing `<forge>.auth` / `<forge>.ssh` config-key convention.

**Scope boundary (clarified during review):** `vcs.RepoLike` git authentication is a **separate mechanism** from `ReleaseSource`/self-update authentication. They are independent concerns and independent code paths; the repo layer reads the forge config keys for git operations, while the release/update subsystem resolves its own release token from `ReleaseSource.Type`/`vcs.provider`. The two only share the low-level `vcs.ResolveToken` primitive (env-ref → keychain → literal), which the repo layer *already* uses via `GetGitHubToken`. This spec does **not** couple repo auth to the release layer — it only widens the repo layer's existing forge-key reading.

Finding addressed (from `docs/development/reports/codebase-audit-2026-06-12.md` §3.5):

- `repo-auth-hardcoded-to-github` — **Medium, missing-feature**

Also folds in the closely-related repo-layer low items so the auth rework lands coherently: `uninitialised-repo-methods-panic` (sentinel-guard all `RepoLike` methods), `getsshkey-fragile-passphrase-detection-and-tui-prompt` (typed `*ssh.PassphraseMissingError`; move the `huh` prompt out of library code), and `direct-token-skips-credential-precedence`.

## Motivation

GTB supports multiple forges, but the in-memory/local git repo layer (used by the init/train subsystems) only authenticates against GitHub. A GitLab-hosted tool that wants to clone/push via the repo layer cannot, despite the framework advertising multi-provider support. The forge-key convention is already established (`github.auth`, `github.ssh.key`); the fix is simply to read the right forge's subtree instead of always `github`.

## Design decisions

### D1 — Forge dimension on repo auth (per-forge config subtrees)

`NewRepo` selects the **forge config subtree** to read — `<forge>.auth` / `<forge>.ssh` — by the configured forge, instead of the hard-coded `github.*` (resolved O2: per-forge subtrees, *not* a provider-neutral `repo.*` subtree). The forge is determined from `ReleaseSource.Type`, overridable by the existing `vcs.provider` config key (resolved O1). Token resolution continues to use the shared `vcs.ResolveToken(cfg, "<FORGE>_TOKEN")` primitive (as `GetGitHubToken` already does) — this is shared plumbing, **not** a coupling to the release-source auth mechanism. Existing `github.*` configs keep working unchanged; the other forges' subtrees are read alongside, so there is **no migration** (resolved O3).

### D2 — Missing auth is non-fatal for public repos

A public clone needs no token. `NewRepo` must not hard-fail when no credential is configured; it falls back to unauthenticated access (matching the release layer's `Private` handling). Only `Private` repos require and enforce a token.

### D3 — SSH auth generalised and library-pure

`configureSSHAuth` reads from a provider-scoped `ssh` subtree (not `github.ssh`). Passphrase detection uses the typed `*ssh.PassphraseMissingError` rather than error-string matching. The interactive `huh` passphrase prompt moves **out of the library** into the CLI layer — `pkg/vcs/repo` must not block on a TUI. (The nil-subtree panic on a scalar `github.ssh` was already fixed in MR !63; this generalises the surrounding code.)

### D4 — Guard all `RepoLike` methods

Most `RepoLike` methods dereference `r.repo`/`r.tree` directly and panic if the repo was not opened, while `WithRepo`/`WithTree` have proper sentinel guards. Add the `ErrNoRepository`/`ErrNoWorktree` guards everywhere for a consistent, non-panicking contract.

## Open questions

*All resolved during review (2026-06-12).*

1. **O1 — Forge source for the repo layer.** *Resolved:* derive from `ReleaseSource.Type`, overridable by the existing `vcs.provider` config key. (D1)
2. **O2 — Config subtree shape.** *Resolved:* **per-forge subtrees** (`github.*`, `gitlab.*`, `bitbucket.*`, …), the established RepoLike convention — *not* a provider-neutral `repo.*` subtree, which would invent a third auth mechanism. (D1)
3. **O3 — Migration / back-compat.** *Resolved:* **no migration**. `github.*` already is the GitHub forge subtree; the other forges' subtrees are read alongside. Nothing to deprecate. (D1)
4. **O4 — `direct` provider.** *Resolved:* **git-hosting forges only** (github/gitlab/bitbucket/gitea/codeberg). The `direct` source is a download URL with no git remote, so `RepoLike` does not apply to it — clone/push has no defined host there. (Flagged for objection if a use case exists.)

## Verification plan

1. **Unit** — `NewRepo` for a GitLab-typed tool resolves auth from the GitLab subtree (not GitHub); a public repo with no token does not error.
2. **Back-compat** — an existing `github.*`-configured tool still authenticates (O3).
3. **No-panic** — every `RepoLike` method on an unopened repo returns the sentinel error, not a panic.
4. **SSH** — passphrase detection uses the typed error; the library does not invoke a TUI.
5. **Integration** (`INT_TEST_VCS=1`) — clone/push against a local bare remote for at least GitHub and GitLab token shapes.
6. **Docs** — update `docs/components/vcs/*` and the repo thread-safety/usage guides.

## Out of scope

- A full v2 role-interface split of the 22-method `RepoLike` (noted in the audit as a v2 direction; this spec only adds the guards, not the split).
- Release-provider changes (that layer already does this correctly and is the model).

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.5, §3.2
- [Plan 3 — improvements](../plans/2026-06-12-audit-plan-3-improvements.md) Phase 14
- `docs/components/vcs/`, `pkg/vcs/release` (the multi-provider model)
