---
title: Testing
description: Guides for testing GTB — automated suites (unit, race, integration, E2E) and hands-on walkthroughs for exercising features end-to-end against a real host.
tags: [testing, development]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Testing

This section collects the hands-on guides for testing GTB. Most day-to-day work is covered by the automated suites wired into `just`:

| Command | Scope |
|---------|-------|
| `just test` | Unit tests with coverage |
| `just test-race` | Race detector pass |
| `just test-integration` | Env-var-gated integration suites — see [Integration Testing](../integration-testing.md) |
| `just test-e2e` | Godog / BDD scenarios against the `cmd/e2e` binary |
| `just test-e2e-smoke` | Fast subset of the E2E suite |
| `just security` | `govulncheck`, `trivy`, `gitleaks`, `osv-scanner` |

Some features involve external platform state (OS keychains, OAuth flows, rate-limited APIs) that the automated suites either mock out or skip. The guides in this directory cover how to exercise those paths on a real workstation, primarily for spec verification and pre-release smoke testing.

## In this section

- [Manual credential testing](manual-credentials.md) — walk through the OS-keychain storage mode end-to-end using the `cmd/e2e` binary: wizard UX, runtime resolution, CI refusal, probe gating, Bitbucket JSON blob, and regulated-build stripping.
- [Testing the keychain on a headless host](headless-keychain-testing.md) — three ways to unblock yourself when the dev server, CI runner, or container has no registered Secret Service: install GNOME Keyring with `dbus-run-session`, run a containerised Secret Service, or swap in the in-memory backend from `credtest`.

## Related

- [Integration Testing](../integration-testing.md) — env-var-gated suites that hit real APIs (GitHub, GitLab, etc.).
- [`docs/components/credentials.md`](../../components/credentials.md) — architecture reference for the backend that the credential walkthrough exercises.
- [`docs/how-to/configure-credentials.md`](../../how-to/configure-credentials.md) — end-user view of the same storage modes.
