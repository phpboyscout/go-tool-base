---
title: "Generator git initialisation & initial commit (opt-out), optional remote push"
description: "gtb generate project scaffolds files but leaves the destination un-versioned — the operator must hand-run git init, add, commit before the tree is tracked. Add an opt-out post-generation git step (init + add + initial commit, only when the destination is not already a repo) and an optional push to the remote derived from props.Tool.ReleaseSource, reusing pkg/vcs/repo for auth."
status: DRAFT
date: 2026-06-15
tags:
  - specification
  - generator
  - git
  - vcs
  - repo
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Generator git initialisation & initial commit (opt-out), optional remote push

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-15

Status
:   DRAFT

## Summary

`gtb generate project` writes a complete skeleton to the destination path —
Go files, templated CI/config files, a `.gitignore`, and `.gtb/manifest.yaml` —
then runs `go mod tidy` and `golangci-lint run --fix` as post-processing
(`runSkeletonPostProcessing`). It stops there: the destination is left as an
**un-versioned directory**. The operator must remember to `git init`, stage,
and make the initial commit before the new tool is tracked at all, and before
the freshly-rendered CI pipeline can run against a real ref.

This spec proposes adding a **post-generation git step** to `generate project`:
when the destination is **not already a git repository**, initialise it, stage
the generated tree, and make the **initial commit** — as an **opt-out** step
(default: do it; skippable via `--no-git`, a manifest preference, or a wizard
prompt). It further proposes an **optional push** of that initial commit to the
remote derived from the supplied `props.Tool.ReleaseSource` (type/host/owner/repo
→ forge URL), reusing the provider-aware auth in `pkg/vcs/repo` delivered by the
[provider-aware-repo-auth](2026-06-12-provider-aware-repo-auth.md) work.

This is purely additive to the generator pipeline; it touches only the
post-write hook point in `internal/generator/skeleton.go` and the
`generate project` command in `internal/cmd/generate/project.go`. It does **not**
alter `regenerate` or `remove`, which must never init or commit (see
[Interaction with `regenerate`/`remove`](#interaction-with-regenerateremove)).

## Motivation

The scaffolded tree is the start of a real repository, but the generator hands
back a directory in an in-between state:

- It already emits a `.gitignore` (in the common skeleton assets) — it clearly
  *intends* the output to be a git repo — yet never initialises one.
- The rendered CI files (`.gitlab-ci.yml` / `.github/workflows/*`) and the
  releaser-pleaser / GoReleaser wiring all assume a versioned repo with a remote.
  Until the operator hand-runs git, none of that is exercisable.
- `go mod tidy` and `golangci-lint --fix` already mutate the tree as part of
  post-processing; the natural "first commit" boundary is *after* that settles,
  which is awkward to reproduce by hand (the manifest hash-refresh step exists
  precisely because post-processing changes files).

A one-line "we did the initial commit for you" is a meaningful DX win for a
scaffolding command, and the framework already owns a provider-aware git layer
(`pkg/vcs/repo`) that can init, commit, add a remote, and push with forge-correct
auth — so the optional push is cheap to offer on top.

The step must be **opt-out, not opt-in**: a brand-new directory almost always
wants to be a repo, and an operator who does not want it can pass `--no-git`.
The push, by contrast, has real side effects on a remote and is treated more
conservatively (see [D6 — Push is opt-in](#d6--push-is-opt-in-not-opt-out)).

## Design decisions

### D1 — A post-generation git step at the existing hook point

The git step runs inside `generateSkeleton` **after** `generateSkeletonFiles`
and **after** `runSkeletonPostProcessing` + `refreshProjectFileHashes`, so the
initial commit captures the fully-settled tree (post-`go mod tidy`,
post-lint-fix, with the final manifest hashes written). It is gated exactly like
post-processing is today: only when the generator filesystem is a real
`*afero.OsFs` (`if _, ok := g.props.FS.(*afero.OsFs); ok`), never under the
in-memory FS used in tests, and never under `--dry-run` (see [D8](#d8--dry-run-and-ci-behaviour)).

The step is a new unexported method (illustrative name `runSkeletonGitInit`)
on `*Generator`, invoked from `generateSkeleton`. Like the existing
post-processing commands, a git failure is **non-fatal**: the skeleton has
already been written successfully, so a failed init/commit/push is logged as a
warning and `generate project` still returns success. The new code does not
change the existing return contract of `GenerateSkeleton`.

### D2 — Already-a-repo detection (the hard gate)

The init+commit **only** happens when the destination is **not already a git
repository**. Detection walks from the destination path upward (matching git's
own discovery semantics) to find an enclosing `.git`: if `generate project -p
some/path` targets a subdirectory of an existing repository, that is treated as
"already a repo" and the step is skipped entirely (we must not make a nested
commit inside someone's existing tree, nor `git init` a subdirectory of a repo).

Implementation note: `pkg/vcs/repo.OpenLocal` *already* does init-if-absent
(`git.PlainOpen` falling back to `git.PlainInitWithOptions`), which is the wrong
primitive here because it cannot distinguish "opened existing" from "initialised
new", and it does not walk upward. The git step therefore needs an explicit
**discovery probe** before deciding to act — either a dedicated helper added to
`pkg/vcs/repo` (preferred, keeps go-git usage in one place — see
[D5](#d5--reuse-pkgvcsrepo-not-go-git-directly)) or
`git.PlainOpenWithOptions(path, &git.PlainOpenOptions{DetectDotGit: true})`
checked for a non-error result. When the probe finds a repo, the whole step
(init, commit, **and** push) is skipped with an INFO log; the generated files
remain in place.

### D3 — Init + add + initial commit

When the destination is not a repo:

1. **Init** the repository at the destination with an explicit default branch
   name (see [D4](#d4--branch-name-and-author-identity)).
2. **Stage** the generated tree. Staging respects the just-written `.gitignore`
   (the skeleton emits one), so build artefacts and the like are not committed.
   `go-git`'s `Worktree.AddWithOptions{All: true}` / `AddGlob` honours
   `.gitignore`; the implementation must confirm the ignore rules are applied
   (a plain `Add(".")` that ignores `.gitignore` would be a bug to guard
   against in tests).
3. **Commit** with a conventional initial-commit message (see
   [D7](#d7--commit-message-convention)).

The `.gtb/manifest.yaml` (and its post-processing hash refresh) is committed as
part of the tree, so the very first commit already records the generator state.

### D4 — Branch name and author identity

**Branch name.** Init with an explicit default branch rather than relying on the
ambient git default. Proposed default: `main`. This should be overridable (flag
or manifest) but `main` is the framework convention and matches the rendered CI
(`releaser-pleaser` operates on `main`). `pkg/vcs/repo.OpenLocal` already shows
the `git.PlainInitWithOptions` + `DefaultBranch` shape.

**Author identity.** The initial commit needs an author. `go-git` does **not**
read `user.name`/`user.email` from the host git config automatically the way the
`git` CLI does, so the identity must be resolved explicitly. Proposed resolution
order (an [open question](#open-questions) to confirm):

1. Local/global git config `user.name` / `user.email`, if resolvable.
2. A GTB-specific config key (e.g. `generator.git.author.{name,email}`).
3. A safe framework fallback (e.g. name `gtb`, email derived from the tool /
   a non-routable placeholder) — used so the commit never fails outright for
   lack of identity.

This is deliberately conservative: a real human identity is preferred, but the
non-fatal contract ([D1](#d1--a-post-generation-git-step-at-the-existing-hook-point))
means a missing identity must degrade to a fallback, not abort scaffolding.

### D5 — Reuse `pkg/vcs/repo`, not go-git directly

The git work routes through `pkg/vcs/repo` (the `RepoLike` / role-interface
layer) rather than calling `go-git` directly from the generator, for three
reasons:

- It is the single place go-git is used and already carries the
  `Committer` (`Commit`, `Push`), `Authenticator`, `CreateRemote`/`Remote`,
  and worktree roles this step needs.
- It already resolves **provider-aware clone/push auth** from the tool's forge
  config subtree (`resolveForge` → `<forge>.auth`/`<forge>.ssh` →
  `vcs.ResolveToken`), exactly what the optional push needs — no second auth
  path. See [provider-aware-repo-auth](2026-06-12-provider-aware-repo-auth.md).
- Its method set (init via `OpenLocal`, `CreateRemote`, `Push`, `Commit`) is
  unit-testable against a local bare remote, matching the existing
  `repo_integration_test.go` patterns.

Gaps to fill in `pkg/vcs/repo` (each a small, backward-compatible addition):

- An **init-only** primitive (or a discovery probe + explicit init) so the
  generator can distinguish "already a repo" from "newly initialised" — today
  `OpenLocal` conflates them ([D2](#d2--already-a-repo-detection-the-hard-gate)).
- Confirm `.gitignore`-respecting staging is reachable through the role
  interface (add a worktree `Add`/`AddAll` method if the public surface does
  not already expose it — `RepoLike` currently exposes `Commit`/`Push` but
  staging goes through the raw worktree via `WithTree`).

The generator depends on the **narrowest roles** it needs (`Committer`, the
worktree accessor, `CreateRemote`) per the role-interface guidance in the
provider-aware-repo-auth spec, not the full `RepoLike` composite.

### D6 — Optional push: deriving the remote from `Tool.ReleaseSource`

The push target is derived from the values already collected by `generate
project` and persisted in `.gtb/manifest.yaml` → `ReleaseSource`
(`Type`/`Host`/`Owner`/`Repo`). The forge URL is built as
`https://{host}/{owner}/{repo}.git` (with `host` defaulting per backend —
`github.com`/`gitlab.com` — exactly as the generator already defaults it). For
GitLab nested groups the owner segment is the full group path
(`group/subgroup`), which the URL construction must preserve.

Mechanics:

1. `CreateRemote("origin", []string{forgeURL})` on the freshly-initialised repo.
2. `Push` the default branch with auth resolved by `NewRepo`'s provider-aware
   flow (`resolveForge` keys off `ReleaseSource.Type`, overridable by
   `vcs.provider`). Token vs SSH selection is identical to clone/push elsewhere.

Auth is **not** re-implemented: it is whatever `pkg/vcs/repo.NewRepo` already
configures for this tool's forge. A public repo with no token still gets an
**unauthenticated** push attempt (which will simply fail at the remote if the
remote requires auth) — the non-fatal contract means that failure is a warning,
not an error.

### D6a — When the remote does not exist yet

The common case for a brand-new tool is that **the remote repository has not
been created on the forge**. Pushing to a non-existent remote fails. This spec
proposes that **creating the remote repository via the forge API
(`pkg/vcs`) is OUT OF SCOPE** for the first iteration (see
[Out of scope](#out-of-scope) and the [open question](#open-questions) on it):
the push targets an **assumed-existing** remote, and a failure (remote absent,
auth missing, network down) is logged as an actionable warning with the manual
`git push` command the operator can run once they have created the repo.
Forge-API repo creation is a plausible follow-up but is a distinct capability
(auth scopes, visibility, default-branch protection) that should not block this
DX win.

### D6 — Push is opt-in, not opt-out

Unlike init+commit (opt-out,
[D1](#d1--a-post-generation-git-step-at-the-existing-hook-point)), **push
defaults to OFF**. Pushing has irreversible-ish side effects on a remote the
operator may not have created, may not intend to populate yet, or may want to
inspect locally first. Push is enabled explicitly via `--push` (and/or a
manifest/wizard preference). `--push` implies the git step (it makes no sense to
push without a commit); `--no-git` with `--push` is a conflicting-flags error.
This opt-in-for-push / opt-out-for-init split is the safe default and is called
out as an [open question](#open-questions) for the maintainer to confirm.

### D7 — Commit message convention

The repo mandates Conventional Commits. The initial commit message should follow
suit. Proposed default:

```
chore: scaffold <tool-name> with gtb

Generated by gtb <version>.
```

`chore:` is non-releasing, which is correct for an initial scaffold (the first
real release is cut later). The tool name and gtb version are already available
to the generator (`config.Name`, `g.currentVersion()`). The exact wording is an
[open question](#open-questions); a `feat:` initial commit is an alternative if
the maintainer wants the scaffold itself to seed the first changelog entry.

### D8 — Dry-run and CI behaviour

- **`--dry-run`**: the git step is **not** executed. The existing dry-run path
  (`GenerateSkeletonDryRun` → `withDryRunOverlay`) previews file writes and the
  post-processing commands without touching disk; the git step is previewed as a
  described action (e.g. "would `git init` + commit on branch `main`"; "would
  push to `<url>`" when `--push` is set) in the `DryRunResult`, consistent with
  how post-process commands are surfaced today.
- **Non-interactive / `--ci`**: the init+commit still runs (it is opt-out and
  has no remote side effect), using flag/manifest values and the author-identity
  fallback — no prompt. The **push** does not auto-enable under `--ci`; it
  remains governed by `--push`. Whether `--ci` should additionally *suppress*
  the git step entirely (treating CI runs as ephemeral) is an
  [open question](#open-questions).

### D9 — Manifest records the preference

`generate project` already records its inputs in `.gtb/manifest.yaml`. A
git/no-git (and push) preference is recorded under a new optional manifest block
(e.g. `properties.git: { init: true, push: false, branch: main }`), mirroring how
signing/help/telemetry preferences are persisted. This keeps a manifest-driven
regeneration self-describing. The block is **advisory for `generate project`
only** — `regenerate` ignores it for git actions
([Interaction with `regenerate`/`remove`](#interaction-with-regenerateremove)).
Whether the preference belongs in the manifest at all (vs being a pure
invocation-time flag) is an [open question](#open-questions).

## Interaction with `regenerate`/`remove`

The git step is **exclusive to `generate project`** (initial scaffolding).

- **`regenerate`** (`internal/generator/regenerate.go`) re-renders into an
  **existing** project that is, by definition, already a git repository
  (it has a committed `.gtb/manifest.yaml`). The already-a-repo gate
  ([D2](#d2--already-a-repo-detection-the-hard-gate)) means regenerate would
  skip init/commit even if the code path were shared — but to be unambiguous,
  the git step is **not wired into the regenerate path at all**. Regenerate must
  never create commits or push on the operator's behalf; it only rewrites files
  and refreshes hashes, leaving staging/committing to the operator (so they can
  review the regenerated diff).
- **`remove`** (`internal/generator/removal.go`) deletes generated files. It
  must never init, stage, commit, or push. No change.

A test asserting that regenerate/remove on a fresh temp dir produce **no new
commits** is part of the [verification plan](#verification-plan).

## Open questions

1. **O1 — Push default.** Confirm push is **opt-in (`--push`, default off)**
   while init+commit is opt-out ([D6](#d6--push-is-opt-in-not-opt-out)). Is
   opt-in the right safety posture, or should push be opt-out-with-confirmation
   in interactive mode?
2. **O2 — Remote creation scope.** Confirm forge-API creation of the remote
   repository is **out of scope** for iteration 1
   ([D6a](#d6a--when-the-remote-does-not-exist-yet)), with push targeting an
   assumed-existing remote and failing to a warning. Is a follow-up to
   create-then-push wanted, and on which forges?
3. **O3 — Author identity source.** Confirm the resolution order in
   [D4](#d4--branch-name-and-author-identity) (host git config → GTB config key →
   framework fallback). go-git does not read host `user.*` automatically — is
   reading the host git config acceptable, or should GTB require its own config
   key / prompt?
4. **O4 — Commit message convention.** Confirm `chore: scaffold <tool> with gtb`
   ([D7](#d7--commit-message-convention)) vs a `feat:` initial commit that seeds
   the first changelog entry.
5. **O5 — Default branch name.** Confirm `main`
   ([D4](#d4--branch-name-and-author-identity)) as the hard default, overridable
   by flag/manifest.
6. **O6 — `--ci` behaviour.** Should `--ci`/non-interactive **suppress** the git
   step entirely (CI runs as ephemeral), or run init+commit as in
   [D8](#d8--dry-run-and-ci-behaviour) (push still opt-in)?
7. **O7 — Manifest preference.** Should the git/push preference be persisted in
   `.gtb/manifest.yaml` ([D9](#d9--manifest-records-the-preference)) or stay a
   pure invocation-time flag with no manifest footprint?
8. **O8 — `pkg/vcs/repo` surface.** Confirm the small additions to
   `pkg/vcs/repo` ([D5](#d5--reuse-pkgvcsrepo-not-go-git-directly)): an
   init-only/discovery primitive distinct from `OpenLocal`'s init-if-absent, and
   a `.gitignore`-respecting staging method on the role surface. Are these
   acceptable additive changes to a Beta-tier package, or should they live in an
   `internal/` helper to avoid widening the public API?
9. **O9 — Staging correctness.** Confirm that `.gitignore` is honoured at stage
   time (go-git `AddWithOptions`), so generated build artefacts are not
   committed — and that the `.gitignore` itself *is* committed.

## Verification plan

1. **Unit — init+commit happens.** `generate project` into a fresh temp dir
   (real OS FS) leaves a git repo with exactly one commit on `main`, whose tree
   includes `.gtb/manifest.yaml`, `.gitignore`, and the Go skeleton.
2. **Unit — already-a-repo gate.** Targeting a path inside an existing repo (and
   a path that *is* a repo) skips init/commit entirely; no new commit is made,
   existing history untouched ([D2](#d2--already-a-repo-detection-the-hard-gate)).
3. **Unit — opt-out.** `--no-git` writes the skeleton with no `.git` directory.
4. **Unit — `.gitignore` honoured.** A path matched by the generated `.gitignore`
   is absent from the initial commit; `.gitignore` itself is present
   ([O9](#open-questions)).
5. **Unit — author identity fallback.** With no resolvable identity, the commit
   still succeeds using the framework fallback
   ([D4](#d4--branch-name-and-author-identity)).
6. **Unit — non-fatal.** A forced git failure (e.g. unwritable `.git`) yields a
   warning and a successful `generate project`, with files still written.
7. **Unit — regenerate/remove make no commits**
   ([Interaction](#interaction-with-regenerateremove)).
8. **Unit — remote URL derivation** from `ReleaseSource` for GitHub (`org/repo`)
   and GitLab nested groups (`group/subgroup/repo`) builds the correct
   `https://{host}/{owner}/{repo}.git`.
9. **Integration (`INT_TEST_VCS=1`)** — `--push` against a **local bare remote**
   pushes the initial commit; auth selection follows the provider-aware flow
   (token vs ssh) — extends `repo_integration_test.go` patterns.
10. **Dry-run** — `--dry-run` performs no git actions and the `DryRunResult`
    describes the would-be init/commit (and push under `--push`).
11. **Docs** — update `docs/components/` (generator) and the relevant
    how-to/concepts pages; cross-reference `pkg/vcs/repo`.

## Out of scope

- **Forge-API creation of the remote repository** (create-then-push). Push
  targets an assumed-existing remote; creating it via `pkg/vcs` provider APIs
  (visibility, branch protection, scopes) is a separate, larger capability
  ([D6a](#d6a--when-the-remote-does-not-exist-yet), [O2](#open-questions)).
- **Multi-commit / curated history** (e.g. separate commits per skeleton layer).
  A single initial commit is the contract.
- **Signing the initial commit** (GPG/SSH-signed commits). The signing
  generator feature concerns *release* signature verification, not commit
  signing; this step makes an unsigned commit.
- **Changing `regenerate`/`remove`** to commit or push anything
  ([Interaction](#interaction-with-regenerateremove)).
- **Re-implementing git auth.** Auth is whatever `pkg/vcs/repo` already resolves.

## Related

- [Provider-aware repository auth](2026-06-12-provider-aware-repo-auth.md) — the
  forge-aware clone/push auth (`resolveForge`, `<forge>.auth`/`<forge>.ssh`,
  `vcs.ResolveToken`) the optional push reuses.
- `internal/generator/skeleton.go` — `GenerateSkeleton` / `generateSkeleton` /
  `runSkeletonPostProcessing` (the hook point) and `writeSkeletonManifest`.
- `internal/cmd/generate/project.go` — the `generate project` command and its
  flag/wizard surface where `--no-git`/`--push` are added.
- `pkg/vcs/repo/repo.go` — `RepoLike` roles (`Committer`, `Authenticator`,
  `CreateRemote`, `OpenLocal`), `NewRepo`'s provider-aware auth.
- `pkg/props/tool.go` — `Tool.ReleaseSource` (Type/Host/Owner/Repo) the remote
  URL is derived from.
- `internal/generator/manifest.go` — `.gtb/manifest.yaml` where a git/push
  preference would be recorded ([D9](#d9--manifest-records-the-preference)).
