---
name: forge-publish-workflow
description: Publish to a git forge (GitLab/GitHub) cleanly — linear history via rebase and fast-forward, never squash-merge from the UI, no AI attribution, no @-mentions.
---

# Forge publishing workflow

Use this whenever you are committing, branching, merging, or opening a
merge/pull request on a git forge (GitLab, GitHub, and the like).

## Linear history, always

- **Favour rebase + fast-forward.** Keep history linear, with no merge commits.
  When a branch has diverged from its target, rebase it onto the latest target
  before merging.
- **Never squash-merge from the MR/PR UI.** Preserve a branch's logical commits.
- **If history genuinely needs squashing, do it locally and force-push** with
  `--force-with-lease`. Do not use the platform's squash toggle.
- Squashing is only worth it to consolidate **WIP-style commits that add no
  historical value** ("fix typo", "wip", "address review"). Meaningful,
  self-contained commits should survive.
- When re-applying a heavily diverged branch, do a **clean single-shot rebase or
  re-apply onto the latest target** rather than replaying many interleaved
  intermediate states.

## A clean publish sequence

1. Confirm the working tree is clean and the default branch is level with its
   remote.
2. Branch off the up-to-date default branch.
3. Commit logical, self-contained changes.
4. Push the branch and open the MR/PR.
5. Locally fast-forward the default branch onto the branch
   (`git merge --ff-only`) and push it; the MR/PR auto-merges.
6. Delete the merged branch, local and remote.

Only commit or push when the human has asked you to, and prefer the `gh` / `glab`
CLI for forge operations.

## Never on a forge

- **No `@`-mentions anywhere** — MR/PR descriptions, comments, commit messages,
  issue text. Bare tokens like `@cli`, `@smoke`, `@release` resolve to real
  usernames and notify those people on every reference. To name a tag, scope, or
  handle literally, drop the `@`, wrap it in backticks (`` `@cli` ``), or
  otherwise neutralise it so the forge does not turn it into a ping.
- **No AI attribution anywhere** — not in commit messages, and not in MR/PR
  descriptions or comments. No `Co-Authored-By:` trailer naming an AI, no
  "generated with AI" or "co-authored by AI" line. Responsibility for the change
  lies with the human who approves the commit or MR; they own it entirely.
