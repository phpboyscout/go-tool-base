---
title: "Desktop-gated integration tests — keychain, WKD, live VCS, live chat"
description: "Specification for the credential- and environment-gated integration tests deferred from Phase 11/15 of the 2026-06-17 test-coverage closure plan. The OS-keychain round-trip (and the WKD check bundled with it) MUST be completed on a desktop environment with a working OS keychain — the headless homelab dev server has no Secret Service daemon. Live VCS and live chat coverage need credentials but are not desktop-gated."
date: 2026-06-20
status: APPROVED
tags:
  - specification
  - testing
  - integration
  - keychain
  - wkd
  - vcs
  - chat
  - deferred
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Desktop-gated Integration Tests Specification

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   20 June 2026

Status
:   APPROVED

---

!!! danger "Environment requirement — read first"
    **Work Items 2 (OS keychain) and 3 (WKD) MUST be done on a desktop
    environment with a working, unlocked OS keychain.** The headless homelab dev
    server has **no Secret Service daemon** (no gnome-keyring + dbus session), so
    `go-keyring` cannot store or retrieve and these tests cannot run there at all.
    Do **not** attempt them over SSH on the homelab box — schedule a session on a
    macOS / Windows / Linux-desktop machine with an unlocked login keyring.

    **Work Item 1 (live VCS) and Work Item 4 (live chat)** need credentials but
    are **not** desktop-gated — they run anywhere with the right tokens/keys.

## Summary

Phase 11 ("integration: GitLab + keychain + WKD + hot-reload") and Phase 15
("chat live-provider") of `docs/development/plans/2026-06-17-test-coverage-closure.md`
are **partially complete**:

- **DONE — config hot-reload.** `pkg/config/hotreload_test.go` already exercises
  the real fsnotify round-trip end-to-end (write a real file in `t.TempDir()`,
  assert observers fire) across eight scenarios. The plan's "unit-only" note was
  stale; nothing to add.
- **DEFERRED — everything else in Phases 11 and 15.** These need live
  credentials (VCS tokens, AI API keys) or a real desktop environment (an OS
  keychain), neither of which the headless homelab dev server provides. This spec
  pins them down so they can be picked up in one sitting when the prerequisites
  exist — and so they are **not forgotten**.

This spec is the single authoritative record of this deferred work; it absorbs
and replaces the earlier lightweight follow-up plan (now removed).

All work items are **gated** with `testutil.SkipIfNotIntegration(t, "<tag>")` so
they compile and skip in normal/CI runs and only execute when the matching
`INT_TEST_<TAG>=1` (or `INT_TEST=1`) env var **and** prerequisites are present.
They live in dedicated `*_integration_test.go` files per the project convention,
and each new `INT_TEST_*` tag is added to the inventory in
`docs/development/integration-testing.md` when its work item lands.

## Goals & Non-Goals

### Goals

- Real-dependency coverage for the four surfaces currently exercised only against
  mocks / `httptest`: live VCS (GitLab + GitHub), OS keychain, WKD, and live chat
  providers.
- A crisp, **environment-explicit** record so the desktop-only work is scheduled
  on the right machine and the homelab-runnable work is picked up opportunistically.
- Per-item prerequisites, gating tag, what-to-test, and acceptance criteria.

### Non-Goals

- No changes to production code are expected; these are test-only additions. If a
  minimal, additive test seam is genuinely required (mirroring an existing one),
  note it in the implementing MR.
- No re-doing the hermetic coverage already delivered (config hot-reload; the
  `httptest`/in-memory unit suites for the VCS providers, chat client, WKD
  derivation, and keychain via `credtest`).
- Not a CI-gating change. These stay opt-in; running them in CI is a separate
  decision once the secrets/runners exist.

---

## Work Item 1 — Live VCS integration (GitLab + GitHub) · NOT desktop-gated

**Effort:** low. **Where:** anywhere, including the homelab. **Blocked on:** a
token + a throwaway test project. Pick this up first — it is the cheapest and
needs no special environment.

**Why it matters:** `pkg/vcs/gitlab` and `pkg/vcs/github` are unit-tested with
`httptest` only. GitLab **nested-group / Enterprise** path handling and the live
PR / release-asset round-trips have no real-API coverage — the headline VCS
capability is exercised only against mocks. (A prior `pkg/vcs/github`
`client_integration_test.go` was removed because it hardcoded a fake
`GITHUB_TOKEN`; do not reintroduce that pattern.)

**What to test (gated, `INT_TEST_VCS=1`):**

- GitLab `GetLatestRelease` / `GetReleaseByTag` / `ListReleases` /
  `DownloadReleaseAsset` against a real project — including a **nested-group**
  path (`group/subgroup/repo`) and, if available, a **self-hosted/Enterprise**
  host via `ReleaseSource.Host`.
- GitLab MR create / update / label / get-by-branch on a scratch branch, with
  cleanup in `t.Cleanup`.
- GitHub equivalents (release read + a PR lifecycle), including a
  `WithEnterpriseURLs` host if a GHE instance is available.
- Token resolution precedence (env vs config).

**Prerequisites / credentials:**

| Need | Detail |
|---|---|
| `GITLAB_TOKEN` | `api` scope on a **throwaway** project the test can create branches/MRs in. |
| GitLab test project | `owner/repo`; ideally also a **nested-group** project; optionally a self-hosted host for the Enterprise path. |
| `GITHUB_TOKEN` | `repo` scope on a **throwaway** GitHub repo (NOT the archived `phpboyscout/gtb` mirror — it rejects writes). |

**Acceptance:** `pkg/vcs/gitlab/*_integration_test.go` (+ a GitHub counterpart)
gated by `INT_TEST_VCS=1`; each test self-cleans created branches/MRs; inventory
updated.

---

## Work Item 2 — OS keychain real round-trip · ⚠️ DESKTOP REQUIRED

**Effort:** medium. **Where:** **a desktop with a working OS keychain — NOT the
homelab.** **Blocked on:** an unlocked OS keychain session.

**Why it matters:** `pkg/credentials/keychain` is exercised today only via
`keyring.MockInit` / the in-memory `credtest` backend. The **real OS backend**
round-trip and the runtime resolution precedence against a real keychain are
unverified.

**What to test (gated, `INT_TEST_KEYCHAIN=1`):**

- Real store → retrieve → delete via the OS backend (blank-import
  `pkg/credentials/keychain`), with a **unique per-run** service/account so
  repeat/parallel runs don't collide; clean up in `t.Cleanup`.
- Runtime resolution precedence end-to-end with a real keychain entry present:
  `env → keychain → literal → well-known fallback` (the matrix in CLAUDE.md
  § Credential Storage).
- The `doctor` `credentials.no-literal` check against a real-keychain-backed
  config.

**Prerequisites / environment:**

| Need | Detail |
|---|---|
| Desktop OS keychain | macOS Keychain / Windows Credential Manager built-in, **or** a Linux **desktop** with gnome-keyring + an active dbus session. |
| Unlocked login keyring | A locked keyring prompts or errors; unlock it for the run. |
| **NOT the homelab** | The headless server has no Secret Service daemon — these will not run there. |

**Acceptance:** `pkg/credentials/keychain/*_integration_test.go` gated by
`INT_TEST_KEYCHAIN=1`; documented as **desktop-only** in
`docs/development/integration-testing.md`.

---

## Work Item 3 — WKD against a live openpgpkey host · bundled with Item 2

**Effort:** low. **Where:** bundled into the same desktop session as Item 2 (no
hard desktop dependency itself — only network egress — but grouped so both real-
crypto items are done in one sitting). **Blocked on:** a live WKD host.

**Why it matters:** the WKD resolver (`pkg/openpgpkey` + the update signing trust
path) is tested against `httptest` only. Resolving a real key from a real Web Key
Directory over HTTPS — advanced vs direct method URLs — is unverified end-to-end.

**What to test (gated, `INT_TEST_WKD=1`):**

- Resolve a known email to its published key from a **live openpgpkey host** and
  assert the fingerprint matches an expected value. The project's own
  `openpgpkey.phpboyscout.uk` (which publishes the release-signing key) is the
  natural target if its WKD tree is live; otherwise a known third-party WKD email.
- Both the advanced-method and direct-method URLs.
- A not-found / unreachable-host path returning the expected error.

**Prerequisites:**

| Need | Detail |
|---|---|
| Live WKD host | e.g. `openpgpkey.phpboyscout.uk` serving a key for a known email; confirm the tree is published. |
| Known email + fingerprint | The expected identity to assert against. |
| Network egress | HTTPS to the host. |

**Acceptance:** `pkg/openpgpkey/*_integration_test.go` gated by `INT_TEST_WKD=1`;
inventory updated.

---

## Work Item 4 — Live chat-provider coverage · NOT desktop-gated

**Effort:** medium. **Where:** anywhere with API keys (costs money — run
sparingly). **Blocked on:** AI provider API keys. This is Phase 15 of the
coverage plan.

**Why it matters:** `pkg/chat` is unit-tested with in-process fakes / `httptest`
only. Real Anthropic / OpenAI / Gemini SSE streaming and auth-mode behaviour have
zero live coverage.

**What to test (gated, `INT_TEST_CHAT_LIVE=1` — distinct from the existing
in-process `chat` tag):**

- A minimal real request/response and an SSE **streaming** turn per provider
  (Anthropic, OpenAI, Gemini), asserting event ordering and final assembly.
- Auth-mode resolution (env-var key) and `ValidateBaseURL` against the real
  endpoints.
- Keep token usage tiny (short prompts, low max-tokens) — these cost money.

**Prerequisites:**

| Need | Detail |
|---|---|
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GEMINI_API_KEY` | One per provider under test; tests skip per-provider when its key is absent. |
| Network egress | HTTPS to each provider. |

**Acceptance:** `pkg/chat/*_live_integration_test.go` gated by a new
`INT_TEST_CHAT_LIVE` tag (so it never runs with the existing in-process `chat`
suite); per-provider skip when the key is missing; inventory updated. Pair with
the doc note already added in `integration-testing.md` clarifying the in-process
`chat` tests need **no** keys.

---

## Testing Strategy & gating

- Each item is a dedicated `*_integration_test.go` gated by its tag via
  `testutil.SkipIfNotIntegration`. New tags: `INT_TEST_KEYCHAIN`, `INT_TEST_WKD`,
  `INT_TEST_CHAT_LIVE` (VCS reuses the existing `INT_TEST_VCS`).
- Tests must **self-clean** any remote/keychain state they create (`t.Cleanup`,
  unique per-run identifiers).
- They stay **opt-in** — never enabled by the default suite or CI without a
  separate, deliberate secrets/runners decision.
- When each item lands: update Phase 11/15 of the coverage-closure plan and add
  the tag + env vars to the inventory in `docs/development/integration-testing.md`.

## Implementation phases (pick-up order)

1. **Item 1 — live VCS (GitLab/GitHub).** As soon as a token + throwaway project
   are to hand. Homelab-runnable. Low effort.
2. **Items 2 + 3 — keychain + WKD, together, on a desktop** with an unlocked OS
   keychain. This is the desktop-gated sitting.
3. **Item 4 — live chat.** Whenever AI keys are available; independent of the
   others; keep it cheap.

## Open Questions

1. Which throwaway GitLab/GitHub projects to target for Item 1 — a dedicated
   `*-it-sandbox` repo per forge is recommended so cleanup can be aggressive.
2. Is `openpgpkey.phpboyscout.uk`'s WKD tree currently published for a known
   email (Item 3)? If not, choose a stable third-party WKD identity.
3. Should Item 4 run on a schedule (cron) once keys exist, given its cost, rather
   than on every opt-in run? Default: manual/opt-in only.
