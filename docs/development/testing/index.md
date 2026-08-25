---
title: Testing
description: Guides for testing GTB: automated suites (unit, race, integration, E2E) and hands-on walkthroughs for exercising features end-to-end against a real host.
tags: [testing, development]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Testing

This section collects the hands-on guides for testing GTB. Most day-to-day work is covered by the automated suites wired into `just`:

| Command | Scope |
|---------|-------|
| `just test` | Unit tests with coverage |
| `just test-race` | Race detector pass |
| `just test-integration` | Env-var-gated integration suites: see [Integration Testing](../integration-testing.md) |
| `just test-e2e` | Godog / BDD scenarios against the `cmd/e2e` binary |
| `just test-e2e-smoke` | Fast subset of the E2E suite |
| `just security` | `govulncheck`, `trivy`, `gitleaks`, `osv-scanner` |

Some features involve external platform state (OS keychains, OAuth flows, rate-limited APIs) that the automated suites either mock out or skip. The guides in this directory cover how to exercise those paths on a real workstation, primarily for spec verification and pre-release smoke testing.

## E2E test binaries: `cmd/e2e` vs `cmd/gtb`

E2E scenarios drive a *compiled* binary, and **which binary matters**. There are two, with deliberately different feature flags: pick the one that matches what you are testing.

### `cmd/e2e`: the framework test binary (default)

`cmd/e2e` exists so we can write **contrived scenarios for framework features without baking test fixtures into the shipped binary**. It enables *every* feature-flagged command (`InitCmd`, `UpdateCmd`, `DoctorCmd`, `ConfigCmd`, `AiCmd`, the VCS login commands, …), wires a stub release source so update/init flows don't reach the network, and embeds a minimal config. It is **not shipped**, GoReleaser only builds `cmd/gtb`.

Use it (via `support.BinaryPath()`) for scenarios exercising the **framework** a downstream tool inherits: init, doctor, config, update, chat, controls lifecycle, signal handling, bootstrap traversal, and so on.

> **The gotcha that keeps biting us:** because `cmd/e2e` *enables* `InitCmd`, the framework bootstrap **requires a config file** (init is how a tool's first config gets created). Scenarios that drive `cmd/e2e` must therefore provide one. The CLI step world writes a default `config.yaml`, and `aTemporaryDirectoryWithConfigFile` overrides it per scenario. Run `cmd/e2e` with no config and it exits non-zero with *"no configuration files found, please run init, or provide a config file"*.

### `cmd/gtb`: the real shipped binary

`cmd/gtb` (`internal/cmd/root`) **disables** `InitCmd`, so its bootstrap treats a missing config as *allow-empty* and gtb's own command-line tooling (`generate`, `regenerate`, `remove`, `keys`) runs **without any config file**, exactly as it does in production.

Scenarios that test **gtb's own tooling** must build and run `cmd/gtb` (via `support.GeneratorBinaryPath()`), **not** `cmd/e2e`. Driving `generate`/`add-flag` through the `InitCmd`-enabled `cmd/e2e` would wrongly demand a config file (green on a developer's machine that has an ambient `~/.config/gtb` config, red in CI's clean environment) and misrepresents how the generator actually runs.

### Rule of thumb

| Testing… | Binary | Helper |
|----------|--------|--------|
| A **framework** feature a downstream tool inherits (init, doctor, config, update, chat, controls, signals) | `cmd/e2e` | `support.BinaryPath()` |
| **gtb's own** tooling *behaviour* (generate/regenerate/remove output, add-flag metadata, key files) | `cmd/gtb` | `support.GeneratorBinaryPath()` |

When a generator/tooling scenario fails in CI with a config error but passes locally, this binary mismatch is almost always why.

> **One deliberate exception.** `cmd/e2e` *does* register the generator commands (`cmd/e2e/main.go`), but only so that **input-validation rejection** scenarios (`features/generator/validation.feature`, e.g. `generate project --name BadName` is rejected) can run through the shared CLI step-world, which supplies a config. Those scenarios assert *rejection* and never reach real generation, so the InitCmd/config requirement is met by the CLI world's config. Anything exercising **successful generation behaviour** must still use `cmd/gtb`.

## In this section

- [Testing huh / charm interactive forms](huh-form-testing.md): unit-test code that drives `charm.land/huh` forms without a TTY: huh's accessible mode (`TERM=dumb`) with scripted stdin, the injectable `WithForm` pattern, or driving the form as a Bubble Tea model. Includes copy-paste helpers and a decision guide.
- [Manual credential testing](manual-credentials.md): walk through the OS-keychain storage mode end-to-end using the `cmd/e2e` binary: wizard UX, runtime resolution, CI refusal, probe gating, Bitbucket JSON blob, and regulated-build stripping.
- [Testing the keychain on a headless host](headless-keychain-testing.md): three ways to unblock yourself when the dev server, CI runner, or container has no registered Secret Service: install GNOME Keyring with `dbus-run-session`, run a containerised Secret Service, or swap in the in-memory backend from `credtest`.

## Related

- [Integration Testing](../integration-testing.md): env-var-gated suites that hit real APIs (GitHub, GitLab, etc.).
- [`docs/components/credentials.md`](../../explanation/components/credentials.md): architecture reference for the backend that the credential walkthrough exercises.
- [`docs/how-to/configure-credentials.md`](../../how-to/configure-credentials.md): end-user view of the same storage modes.
