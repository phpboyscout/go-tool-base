---
name: drive-ci-from-the-cli
description: Drive a merge request through CI from the CLI — open it, watch the pipeline to green, merge on success, and work around a forge SSH incident by pushing over HTTPS. Use when landing an MR/PR whose pipeline you need to watch and merge.
---

# Drive CI from the CLI

Use this when landing a change through a forge MR/PR whose pipeline you need to
watch and merge — rather than assuming a push is fine and walking away.

## Watch, then merge on green

- After pushing and opening the MR, **watch the pipeline to completion** instead
  of guessing. Poll `glab ci status --branch <branch>` (or `gh`) until it leaves
  the running state; do it in a background loop so you're not blocking, and check
  job-level status if the top-line is ambiguous.
- **Merge on green via the API** (`glab mr merge <n>` / `gh pr merge`) —
  fast-forward, preserving commits (see `forge-publish-workflow`). Confirm `main`
  hasn't advanced past your branch's base first; if it has, rebase before
  merging.
- **A green MR pipeline ≠ a re-run main pipeline.** After merge, a tag/release or
  bot (e.g. a Release MR) may run on `main` — wait for *that* before assuming the
  tag/artifact exists.

## Flaky jobs: retry, don't re-rebase

If a job fails on a timing flake (not a real failure), **retry that job**, don't
rebase the whole branch — a rebase re-runs everything and, on a busy repo whose
`main` keeps advancing, can put you on a treadmill where the branch is never
green-and-current at the same moment. Rebase only when `main` actually moved.

## When the forge has an SSH incident

If `git push`/`fetch` over SSH suddenly fails for **every** repo with an empty
`remote: ERROR:` banner — while `ssh -T git@<host>` still greets you and the
HTTPS API still works — suspect a **forge-side SSH incident**, not your key or
your sandbox. Confirm it's broad (a known-good repo fails too), then work around
it over **HTTPS**:

```
git -c credential.helper='!f() { echo username=oauth2; echo "password=$GITLAB_TOKEN"; }; f' \
  push https://<host>/<path>.git <branch>
```

- Use the token you already have in the environment (`$GITLAB_TOKEN` / a PAT) via
  a one-shot credential helper, so it never lands in the remote URL or process
  args. The default `glab auth git-credential` helper may be scoped read-only and
  get an HTTP-Basic *Access denied* — the explicit token helper is what works.
- `glab` / `gh` MR operations (create, merge) go over the **HTTPS API** and are
  unaffected by an SSH incident, so the rest of the flow still works.
- Don't hammer a downed endpoint: a gentle retry loop (or just wait) is enough —
  SSH resumes on its own once the forge resolves the incident.
