---
title: Testing the keychain on a headless host
description: Three ways to exercise the OS-keychain credential storage mode on a server, container, or CI runner — where no desktop session or registered Secret Service provider is available.
tags: [testing, development, credentials, keychain, headless]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Testing the keychain on a headless host

The keychain backend relies on a reachable OS credential store — macOS Keychain, Windows Credential Manager, or a freedesktop Secret Service provider on Linux. On a headless Linux server (dev box, SSH-only host, CI runner), the session bus is often present but no Secret Service is registered, so `credentials.Probe()` correctly returns `false` and the setup wizard hides the keychain option. Scenarios 1, 2, and 5 in [Manual credential testing](manual-credentials.md) all depend on the probe passing.

This guide gives engineers three ways to unblock themselves, in order of how close each one is to the real thing.

| Option | Real go-keyring round-trip? | Install required | Best for |
|--------|-----------------------------|------------------|----------|
| [1. GNOME Keyring + `dbus-run-session`](#option-1--gnome-keyring-with-dbus-run-session) | Yes | `gnome-keyring`, `libsecret-tools` | Pre-release verification on a dev server |
| [2. Containerised Secret Service](#option-2--containerised-secret-service) | Yes | Docker or Podman | Cross-distro testing; hermetic isolation |
| [3. In-memory backend swap](#option-3--in-memory-backend-swap) | No (exercises GTB code paths) | None | Fast iteration; CI runners without D-Bus |

## Option 1 — GNOME Keyring with `dbus-run-session`

Spawns a transient session bus, runs an unlocked `gnome-keyring-daemon` inside it, and runs your test command against the live Secret Service. When the outer shell exits, the bus and keyring are discarded — no persistent state.

### Prerequisites

```bash
sudo apt-get install -y gnome-keyring libsecret-tools dbus-user-session
# Fedora: sudo dnf install -y gnome-keyring libsecret dbus-daemon
# Arch:   sudo pacman -S gnome-keyring libsecret dbus
```

### One-off test

Use this for a single scripted command — the daemon starts, your command runs, everything is torn down:

```bash
dbus-run-session -- bash -c '
  # Unlock (or create) the keyring with a test password, start the
  # Secret Service component only.
  printf "test-pass\n" | gnome-keyring-daemon --unlock --start --components=secrets >/dev/null

  # Sanity check: write/read a canary entry.
  printf "canary-value" | secret-tool store --label="canary" service test account canary
  secret-tool lookup service test account canary
  # → canary-value

  # Now run the e2e binary against the live keyring.
  ./bin/e2e init ai --dir /tmp/e2etest
'
```

### Interactive session

When you want to work through the wizard prompts manually, keep the subshell alive:

```bash
dbus-run-session -- bash -l
# --- inside the subshell ---
printf "test-pass\n" | gnome-keyring-daemon --unlock --start --components=secrets >/dev/null

# Verify probe passes
go run ./cmd/e2e ai ...   # or any scenario from manual-credentials.md

# When done:
exit
```

### Caveats

- `dbus-run-session` must run from a real login session (SSH, tmux, screen). Running under `sudo -u someoneelse` or from a daemon context often fails with "Failed to open connection to bus".
- The daemon runs with the password you gave on stdin. Don't reuse a production password; a throwaway string like `test-pass` is the convention.
- Entries written under `dbus-run-session` are NOT visible from the same user's normal login session — they live in a separate transient keyring. That's the point: nothing leaks between test runs.

## Option 2 — Containerised Secret Service

A throwaway container with `gnome-keyring` installed, running a dedicated Secret Service instance. Useful when:

- You want to test against a specific distro or library version.
- Your host's systemd security policy blocks `dbus-run-session`.
- You want guaranteed isolation between runs.

```bash
docker run --rm -it \
  -v "$PWD":/src \
  -w /src \
  ubuntu:24.04 \
  bash -c '
    apt-get update -qq && apt-get install -y --no-install-recommends \
      golang-go gnome-keyring libsecret-tools dbus ca-certificates git >/dev/null

    dbus-run-session -- bash -c "
      printf \"test-pass\n\" | gnome-keyring-daemon --unlock --start --components=secrets >/dev/null
      go run ./cmd/e2e init ai --dir /tmp/e2etest
    "
  '
```

For interactive use, replace the final `bash -c \"…\"` with `bash` and drive the scenarios from the container shell.

## Option 3 — In-memory backend swap

The `pkg/credentials/credtest.MemoryBackend` satisfies `credentials.Backend` and reports `Available() == true`, so the wizard thinks a real keychain is present. Everything GTB does downstream — storage-mode selector, `Probe()` round-trip, config writes, resolver cascade — runs unchanged.

**What this covers:**

- Wizard UI: the "OS keychain" option appears, wizard walks through the keychain branch.
- `credentials.Probe()`: canary `Store`/`Retrieve`/`Delete` all succeed against the map.
- Config writes: `{provider}.api.keychain: <tool>/<account>` lands in config, no literal value on disk.
- Resolver: `pkg/chat.New`, `pkg/vcs.ResolveToken`, and `bitbucket.NewReleaseProvider` all resolve through the backend.
- Bitbucket JSON-blob corrupt/incomplete abort.
- Regulated-build stripping via `rm cmd/e2e/keychain.go`.

**What this does NOT cover:**

- The actual `go-keyring` library's behaviour against a platform keychain. That's already covered by the unit tests in `pkg/credentials/keychain/` (via `keyring.MockInit()`) and should be verified with Option 1 on a desktop before a release.

### Swap in the memory backend

Replace `cmd/e2e/keychain.go` with:

```go
package main

// Developer-only: activates the in-memory backend so the e2e binary
// can be tested on a host that lacks a real Secret Service provider.
// See docs/development/testing/headless-keychain-testing.md.
//
// Do NOT commit this form — cmd/e2e ships with the real backend so
// CI exercises the full go-keyring path.

import (
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
)

//nolint:gochecknoinits // side-effect registration for headless test runs
func init() {
	credentials.RegisterBackend(&credtest.MemoryBackend{})
}
```

Rebuild and run:

```bash
go build -o bin/e2e ./cmd/e2e

./bin/e2e init ai --dir /tmp/e2etest
# → "OS keychain" appears
# → wizard writes anthropic.api.keychain to config
# → secret lives in the in-process map, discarded on exit

# Run Scenario 2's resolver snippet with the same swap in place to
# verify the whole cascade.
```

### Restore before committing

```bash
git checkout -- cmd/e2e/keychain.go
git diff cmd/e2e/keychain.go   # should be empty
```

Never commit the swapped version — `cmd/e2e` must ship with the real backend so the Gherkin suite in CI exercises the full go-keyring path.

## Related

- [Manual credential testing](manual-credentials.md) — the scenarios this guide unblocks.
- [`docs/components/credentials.md`](../../components/credentials.md) — architecture reference for `Backend`, `RegisterBackend`, and the stub/memory/go-keyring implementations.
- [`pkg/credentials/credtest`](../../../pkg/credentials/credtest/memory.go) — source for the in-memory backend and its test helper.
