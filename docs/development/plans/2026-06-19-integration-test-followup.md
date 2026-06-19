---
title: "Deferred integration tests — GitLab/GitHub VCS, OS keychain, WKD"
description: "Follow-up work for the credential- and environment-gated integration tests deferred from Phase 11 of the 2026-06-17 test-coverage closure plan. The hermetic parts (config hot-reload) are already covered; these three items need live credentials or a desktop keychain session and are tracked here so they are not lost."
status: TODO
date: 2026-06-19
---

# Deferred integration tests (Phase 11 follow-up)

Phase 11 of `docs/development/plans/2026-06-17-test-coverage-closure.md` ("integration:
GitLab + keychain + WKD + hot-reload") is **partially complete**:

- **Config hot-reload — DONE (already covered).** `pkg/config/hotreload_test.go`
  already exercises the real fsnotify round-trip end-to-end (write a real file
  in `t.TempDir()`, assert observers fire) across eight scenarios
  (merge-preserved, validation-rollback, fail-closed, no-deadlock,
  primary-file-removed, env-prefix, close-idempotent). The watch/reload/notify
  functions are 73–100% covered. Phase 11's "config hot-reload is unit-only" note
  was stale; nothing to add.
- **GitLab/GitHub VCS, OS keychain, WKD — DEFERRED.** These need live
  credentials or a real desktop environment that the headless homelab dev server
  does not provide. They are specified below so they can be picked up in one
  sitting when the prerequisites are available.

All three are **gated** with `testutil.SkipIfNotIntegration(t, "<tag>")` so they
compile and skip in normal/CI runs, and only execute when the matching
`INT_TEST_<TAG>=1` (or `INT_TEST=1`) env var and prerequisites are present. They
live in dedicated `*_integration_test.go` files per the project convention.

---

## Item 1 — GitLab + GitHub VCS integration (LOW effort, ready when creds exist)

**Status:** ready to write — blocked only on a token + a throwaway test project.
The user has confirmed this is "easy"; it is the first to pick up.

**Why it matters:** `pkg/vcs/gitlab` and `pkg/vcs/github` are unit-tested with
`httptest` only. GitLab nested-group / Enterprise path handling and the live
PR / release-asset round-trips have no real-API coverage — the headline VCS
capability is currently exercised only against mocks.

**What to test (gated, `INT_TEST_VCS=1`):**

- GitLab `GetLatestRelease` / `GetReleaseByTag` / `ListReleases` /
  `DownloadReleaseAsset` against a real project — including a **nested-group**
  path (`group/subgroup/repo`) and, if available, a **self-hosted/Enterprise
  host** via `ReleaseSource.Host`.
- GitLab PR (MR) create / update / label / get-by-branch against the test
  project (create on a scratch branch, clean up after).
- GitHub equivalents against a real GitHub repo (release read + a PR lifecycle),
  including a `WithEnterpriseURLs` host if a GHE instance is available.
- Token resolution precedence from env vs config.

**Prerequisites / credentials:**

| Need | Detail |
|---|---|
| `GITLAB_TOKEN` | `api` scope, on a throwaway project the test can create MRs/branches in. |
| GitLab test project | `owner/repo`; ideally also a **nested-group** project to exercise path encoding; optionally a self-hosted host for the Enterprise path. |
| `GITHUB_TOKEN` | `repo` scope on a throwaway GitHub repo (NOT the archived `phpboyscout/gtb` mirror, which rejects writes). |

**Acceptance:** new `pkg/vcs/gitlab/*_integration_test.go` (and a GitHub
counterpart) gated by `INT_TEST_VCS=1`; each test self-cleans created branches /
MRs; documented in `docs/development/integration-testing.md`.

---

## Item 2 — OS keychain real round-trip (BLOCKED on a desktop keychain session)

**Status:** blocked. **Cannot run on the homelab dev server** — a headless Linux
box has no Secret Service daemon (gnome-keyring + a dbus session), so `go-keyring`
fails. Pick this up in a session on a **desktop with a working OS keychain**
(macOS Keychain, Windows Credential Manager, or a Linux desktop with an unlocked
gnome-keyring).

**Why it matters:** `pkg/credentials/keychain` is exercised today only via
`keyring.MockInit` / the in-memory `credtest` backend. The real OS backend
round-trip (store → retrieve → delete) and the runtime resolution precedence
(`env → keychain → literal → fallback`) against a real keychain are unverified.

**What to test (gated, `INT_TEST_KEYCHAIN=1`):**

- Real store / retrieve / delete via the OS backend (blank-import
  `pkg/credentials/keychain`), with a unique per-run service/account so parallel
  or repeat runs don't collide; clean up in `t.Cleanup`.
- Runtime resolution precedence end-to-end with a real keychain entry present:
  env var wins over keychain, keychain over literal, literal over well-known
  fallback (mirrors the matrix in CLAUDE.md § Credential Storage).
- The `doctor` `credentials.no-literal` check against a real-keychain-backed
  config.

**Prerequisites / environment:**

| Need | Detail |
|---|---|
| Desktop OS keychain | macOS/Windows built-in, or Linux desktop with gnome-keyring + an unlocked login keyring and an active dbus session. **Not present on the homelab server.** |
| Interactive/unlocked session | The keyring must be unlocked (a locked keyring prompts or errors). |

**Acceptance:** new `pkg/credentials/keychain/*_integration_test.go` gated by
`INT_TEST_KEYCHAIN=1`; runnable on a developer desktop; documented as
desktop-only in `docs/development/integration-testing.md`.

---

## Item 3 — WKD against a real openpgpkey host (deferred with keychain)

**Status:** deferred to the same desktop session as Item 2 (per the user's
grouping). Network egress to a live WKD host is the only hard requirement; no
secret is needed.

**Why it matters:** the WKD resolver (`pkg/openpgpkey` + the update signing trust
path) is tested against `httptest` only. Resolving a real key from a real Web Key
Directory over HTTPS — including the advanced vs direct method URLs — is
unverified end-to-end.

**What to test (gated, `INT_TEST_WKD=1`):**

- Resolve a known email to its published key from a **live openpgpkey host** and
  assert the fingerprint matches an expected value. The project's own
  `openpgpkey.phpboyscout.uk` (which publishes the release-signing key) is the
  natural target if its WKD tree is live; otherwise a known third-party WKD email.
- Both the advanced-method (`openpgpkey.<domain>/.well-known/openpgpkey/<domain>/...`)
  and direct-method URLs.
- A not-found / unreachable host path returning the expected error.

**Prerequisites:**

| Need | Detail |
|---|---|
| Live WKD host | e.g. `openpgpkey.phpboyscout.uk` serving a key for a known email; confirm the tree is published. |
| Known email + fingerprint | The expected identity to assert against. |
| Network egress | HTTPS to the host (works from the homelab too, but bundled with Item 2 for a single sitting). |

**Acceptance:** new `pkg/openpgpkey/*_integration_test.go` gated by
`INT_TEST_WKD=1`; documented in `docs/development/integration-testing.md`.

---

## Pick-up order

1. **Item 1 (GitLab/GitHub)** — as soon as a token + throwaway project are to
   hand; low effort, runnable from the homelab.
2. **Items 2 + 3 (keychain + WKD)** — together, in a desktop session with an
   unlocked OS keychain. WKD has no hard desktop dependency but is bundled here
   for one sitting.

When each lands, update Phase 11 of the coverage-closure plan and add the new
`INT_TEST_*` tags to the inventory in `docs/development/integration-testing.md`.
