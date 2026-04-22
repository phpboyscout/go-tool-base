---
title: Manual credential testing
description: Exercise the OS-keychain credential storage mode end-to-end against a real workstation using the cmd/e2e test binary — wizard UX, runtime resolution, CI refusal, probe gating, Bitbucket JSON blob, and regulated-build stripping.
tags: [testing, development, credentials, keychain]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Manual credential testing

This guide walks through every observable behaviour of the OS-keychain storage mode using the `cmd/e2e` binary, which exposes all feature-flagged setup flows that the shipped `gtb` binary gates behind release-time decisions. Use it to verify keychain behaviour during spec work, pre-release smoke checks, or when investigating a report from a real deployment.

Each scenario maps to a requirement in [`2026-04-02-credential-storage-hardening.md`](../specs/2026-04-02-credential-storage-hardening.md). Automated coverage is in the Gherkin suite under `features/`; this guide is for the cases where a live OS keychain is easier than a mock.

## Prerequisites

Build the binary:

```bash
just build-e2e           # or: go build -o bin/e2e ./cmd/e2e
```

A reachable OS keychain is required for most scenarios. Support matrix:

| Platform | Backend | Ready out of the box? |
|----------|---------|-----------------------|
| macOS | Keychain | Yes — login keychain, unlocked after login. |
| Linux (desktop) | Secret Service via godbus (GNOME Keyring, KWallet) | If a desktop session is running. |
| Linux (headless/SSH/containers) | — | No. Probe detects; wizard hides keychain option. |
| Windows | Credential Manager | Yes on a logged-in user session. |

Check Linux reachability before starting:

```bash
dbus-send --session --print-reply \
  --dest=org.freedesktop.secrets /org/freedesktop/secrets \
  org.freedesktop.DBus.Peer.Ping
```

No error means the probe will succeed.

## Scenario 1 — Happy path: wizard writes to keychain

Exercises the end-to-end flow: storage-mode selector surfaces keychain, wizard calls `credentials.Store`, config records the reference, secret never touches disk.

```bash
rm -rf /tmp/e2etest
./bin/e2e init ai -d /tmp/e2etest
```

At each prompt:

1. **Select AI provider** — pick any (Claude, OpenAI, Gemini).
2. **Credential Storage** — confirm `OS keychain` appears as an option.
3. Pick `OS keychain`.
4. **API Key** — paste a recognisable fake, e.g. `sk-ant-test-xyzzy`.

Verify the written config:

```bash
cat /tmp/e2etest/config.yaml
```

Expected shape (provider-dependent):

```yaml
anthropic:
  api:
    keychain: e2e/anthropic.api
```

No `key:` field, no plaintext secret on disk.

Confirm the secret actually landed in the OS keychain:

=== "Linux"

    ```bash
    secret-tool lookup service e2e account anthropic.api
    ```

=== "macOS"

    ```bash
    security find-generic-password -a anthropic.api -s e2e -w
    ```

=== "Windows (PowerShell)"

    ```powershell
    # Credential Manager entries are surfaced under
    # Generic Credentials as "e2e:anthropic.api".
    cmdkey /list:e2e*
    ```

Each must print `sk-ant-test-xyzzy`.

## Scenario 2 — Resolver reads from keychain at runtime

With the config from Scenario 1 still in place:

```bash
./bin/e2e doctor --dir /tmp/e2etest
```

Expected: `credentials.no-literal ✓ no literal credentials in config`.

To observe the full resolver cascade, run at DEBUG:

```bash
./bin/e2e --log-level debug ai chat "hi" --dir /tmp/e2etest 2>&1 | head -20
```

The chat call itself will fail (the fake key isn't a real API key), but the log lines should show the credential being resolved from the keychain, not the config. A line with `source=keychain` and the service/account pair (not the value) is the signal.

## Scenario 3 — CI refuses literal mode (R5)

```bash
CI=true ./bin/e2e init ai -d /tmp/e2etest-ci
```

- The storage-mode prompt should **not** list `Literal value in config file (plaintext)`.
- If you bypass the form (via a test-only injection), the wizard exits non-zero with a hint pointing at CI secret injection.

## Scenario 4 — Probe gates the option when backend unreachable

Simulate a headless environment:

```bash
DBUS_SESSION_BUS_ADDRESS=disabled ./bin/e2e init ai -d /tmp/e2etest-headless
```

`OS keychain` must not appear in the storage-mode list. The probe performed a canary `Store`/`Retrieve`/`Delete`, got an error, and the wizard hid the option.

On a real headless host (SSH into a server without a desktop session), the same behaviour applies without `DBUS_SESSION_BUS_ADDRESS` manipulation.

## Scenario 5 — Bitbucket dual-credential JSON blob

Bitbucket requires a `{username, app_password}` pair. The wizard for Bitbucket storage mode is Phase 3, but the resolver can be exercised today by hand-populating the keychain entry.

Write the JSON blob:

=== "Linux"

    ```bash
    printf '{"username":"alice","app_password":"s3cret"}' | \
      secret-tool store --label="e2e bitbucket" \
        service e2e account bitbucket.auth
    ```

=== "macOS"

    ```bash
    security add-generic-password -a bitbucket.auth -s e2e \
      -w '{"username":"alice","app_password":"s3cret"}'
    ```

Point the config at it:

```bash
mkdir -p /tmp/e2etest-bb
cat > /tmp/e2etest-bb/config.yaml <<'EOF'
bitbucket:
  keychain: e2e/bitbucket.auth
EOF
```

Any Bitbucket resolver call now loads and unmarshals the blob. Debug it with:

```bash
./bin/e2e --log-level debug --dir /tmp/e2etest-bb doctor 2>&1 | head -20
```

### Corrupt-blob abort (R3)

Replace the entry with malformed JSON:

=== "Linux"

    ```bash
    printf '{"username":"alice' | \
      secret-tool store --label="e2e bitbucket" \
        service e2e account bitbucket.auth
    ```

=== "macOS"

    ```bash
    security delete-generic-password -a bitbucket.auth -s e2e
    security add-generic-password -a bitbucket.auth -s e2e \
      -w '{"username":"alice'
    ```

The next Bitbucket resolver call must surface `"not valid JSON"` rather than silently using a literal fallback. This is the R3 guarantee: a broken keychain entry is not masked by stale literals.

## Scenario 6 — Regulated-build strips keychain entirely

Confirm that deleting the opt-in import really removes every keychain code path:

```bash
rm cmd/e2e/keychain.go
go build -o bin/e2e-regulated ./cmd/e2e
go tool nm bin/e2e-regulated | grep -cE "zalando|godbus"
# → 0
```

Run a wizard against that binary:

```bash
./bin/e2e-regulated init ai -d /tmp/e2etest-reg
```

`OS keychain` must not appear. The backend is the stub, `credentials.Store` returns `ErrCredentialUnsupported`, and no session-bus or platform-keychain IPC ever happens.

Restore the file before committing:

```bash
git checkout -- cmd/e2e/keychain.go
```

## Cleanup

=== "Linux"

    ```bash
    secret-tool clear service e2e account anthropic.api
    secret-tool clear service e2e account bitbucket.auth
    ```

=== "macOS"

    ```bash
    security delete-generic-password -a anthropic.api -s e2e
    security delete-generic-password -a bitbucket.auth -s e2e
    ```

=== "Windows"

    ```powershell
    cmdkey /delete:e2e:anthropic.api
    cmdkey /delete:e2e:bitbucket.auth
    ```

```bash
rm -rf /tmp/e2etest /tmp/e2etest-ci /tmp/e2etest-headless /tmp/e2etest-bb /tmp/e2etest-reg
```

## Troubleshooting

**"OS keychain" option missing when I expect it to appear.**
Run the probe in isolation to see which stage fails:

```go
package main

import (
    "fmt"

    _ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
    "github.com/phpboyscout/go-tool-base/pkg/credentials"
)

func main() {
    fmt.Println("Available:", credentials.KeychainAvailable())
    fmt.Println("Probe:",    credentials.Probe())
}
```

- `Available=false` → the `pkg/credentials/keychain` subpackage is not linked (missing blank import, or you're on the stub build).
- `Available=true` / `Probe=false` → backend is compiled in but the live round-trip failed. On Linux this is almost always a missing or locked Secret Service provider; on macOS, a locked login keychain.

**`secret-tool: command not found` on Linux.** Install `libsecret-tools` (Debian/Ubuntu) or `libsecret` (Fedora/Arch). You can also verify entries via GNOME Seahorse (GUI) or `dbus-send` queries.

**Corrupt-JSON test isn't triggering.** Check you're pointing at the right keychain entry — service must be `e2e`, account must be `bitbucket.auth`. The resolver only inspects `bitbucket.keychain` config entries, not `bitbucket.<field>.env` or `bitbucket.username`.

## Related

- [`docs/components/credentials.md`](../../components/credentials.md) — architecture reference.
- [`docs/how-to/configure-credentials.md`](../../how-to/configure-credentials.md) — end-user configuration guide.
- [`2026-04-02-credential-storage-hardening.md`](../specs/2026-04-02-credential-storage-hardening.md) — spec driving this work; each scenario maps to a requirement (R1–R6).
