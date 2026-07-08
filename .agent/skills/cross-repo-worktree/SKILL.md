---
name: cross-repo-worktree
description: When the work touches a repo other than the one Claude was invoked in — or another session may be live on the same repo — do it in a dedicated git worktree of the target repo, so you never clobber another checkout's working tree or fight over its branch. Use before editing a repo that isn't your invocation cwd.
---

# Work in a worktree, not another repo's live checkout

## The hazard

Editing a repo's main checkout while it is the working directory of **another Claude session**
(or is simply a *different project* from the one you were invoked in) clobbers in-flight
changes and starts a fight over the branch. It happens for real: a session generating a reel
started editing keryx's checkout mid-build, on keryx's own feature branch, and had to be pulled
out and moved to a worktree before it wrecked the other session's work.

## The rule

If the change lands in a repo that is **not your invocation cwd**, or another session might be
working the same repo, **create a git worktree of the target repo and do the work there.** Never
edit the shared checkout, and never touch a branch another session owns.

## How

```bash
# fresh branch off the target's latest main, in a scratch location
git -C <target-repo> fetch origin
git -C <target-repo> worktree add /tmp/wt-<name> -b <branch> origin/main

# work, commit and push from inside the worktree
cd /tmp/wt-<name>
# …make the change, commit, push, raise the MR/PR…

# clean up when done (auto-removes if unchanged)
git -C <target-repo> worktree remove /tmp/wt-<name>
```

- Branch off the target's **latest main**, not whatever the shared checkout happens to be on.
- Put the worktree somewhere **scratch** — never inside a tree that's being served or indexed
  (a Hugo/studio server, a file watcher).
- A worktree is cheap; a clobbered branch or a trampled parallel session is not.

## Composes with

- **forge-publish-workflow** — land the worktree's branch with rebase + fast-forward, no
  squash-from-the-UI, no AI attribution, no `@`-mentions.
- For adopting/upgrading a *shared CI-CD component or pinned dependency* in a consumer repo,
  reach for `phpboyscout-infra`'s **adopt-shared-components**, which is this pattern specialised
  to that case.
