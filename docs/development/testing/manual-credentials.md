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
./bin/e2e init ai --dir /tmp/e2etest
```

At each prompt:

1. **Select AI provider** — pick any (Claude, OpenAI, Gemini).
2. **Credential Storage** — confirm `OS keychain` appears as an option.
   If it doesn't, your host fails the probe — jump to [Troubleshooting](#troubleshooting) before going further.
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

## Scenario 2 — Doctor and config observe the keychain reference

With the config from Scenario 1 still in place, point the tool at it via the root-level `--config` flag (`doctor` does not have its own `--dir`; it reads whichever config file is loaded):

```bash
./bin/e2e --config /tmp/e2etest/config.yaml doctor
```

Expected: the `credentials.no-literal` check passes — no literal credentials were written.

Inspect the resolved config:

```bash
./bin/e2e --config /tmp/e2etest/config.yaml config get anthropic.api.keychain
# → e2e/anthropic.api
```

The resolver itself is not directly exercised by any `gtb` subcommand — the e2e binary doesn't expose an `ai chat`-style invocation. To see a real resolution trace, run a tiny ad-hoc program that imports the library:

```bash
cat > /tmp/resolve_check.go <<'EOF'
package main

import (
	"context"
	"fmt"

	"github.com/spf13/afero"

	_ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func main() {
	cfg, err := config.LoadFilesContainer(afero.NewOsFs(),
		config.WithConfigFiles("/tmp/e2etest/config.yaml"))
	if err != nil {
		panic(err)
	}
	p := &props.Props{Logger: logger.NewNoop(), Config: cfg}
	client, err := chat.New(context.Background(), p,
		chat.Config{Provider: chat.ProviderClaude})
	fmt.Printf("client=%v err=%v\n", client != nil, err)
}
EOF
go run /tmp/resolve_check.go
```

`client=true err=<nil>` confirms the resolver walked env → keychain → literal and found the keychain-stored value.

## Scenario 3 — CI refuses literal mode (R5)

```bash
CI=true ./bin/e2e init ai --dir /tmp/e2etest-ci
```

- The storage-mode prompt must **not** list `Literal value in config file (plaintext)`.
- If you bypass the form (via a test-only injection), the wizard exits non-zero with a hint pointing at CI secret injection.

## Scenario 4 — Probe gates the option when backend unreachable

If `OS keychain` is missing from the Scenario 1 prompt, you've already landed in this scenario. The probe returned `false` and the wizard hid the option — the designed behaviour on any host without a registered Secret Service provider.

Common triggers for probe failure:

| Host | Why probe fails |
|------|-----------------|
| Headless Linux server / SSH dev box | `DBUS_SESSION_BUS_ADDRESS` may be set, but no Secret Service bus name is registered. |
| CI runner / container | Usually no session bus at all. |
| Linux desktop with keychain locked | Session bus reachable, Secret Service registered, but writes rejected until unlocked. |

To force the failure mode on a host that would otherwise pass (e.g. for verification before a release on your laptop):

```bash
DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null \
  ./bin/e2e init ai --dir /tmp/e2etest-headless
```

`OS keychain` must not appear in the storage-mode list.

To distinguish "D-Bus missing" from "Secret Service missing" on your own host:

```bash
echo "DBUS_SESSION_BUS_ADDRESS=$DBUS_SESSION_BUS_ADDRESS"
dbus-send --session --print-reply \
  --dest=org.freedesktop.secrets /org/freedesktop/secrets \
  org.freedesktop.DBus.Peer.Ping
```

- Empty `DBUS_SESSION_BUS_ADDRESS` → no session bus.
- `ServiceUnknown` error → bus is there, no Secret Service registered.
- No error → bus and Secret Service both live; probe should succeed.

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

No e2e subcommand exercises the Bitbucket resolver directly, so drive it with a tiny ad-hoc program that calls `bitbucket.NewReleaseProvider` — construction performs the resolution and surfaces any error:

```bash
cat > /tmp/bb_check.go <<'EOF'
package main

import (
	"fmt"

	"github.com/spf13/afero"

	_ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/bitbucket"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func main() {
	cfg, err := config.LoadFilesContainer(afero.NewOsFs(),
		config.WithConfigFiles("/tmp/e2etest-bb/config.yaml"))
	if err != nil {
		panic(err)
	}
	_, err = bitbucket.NewReleaseProvider(release.ReleaseSourceConfig{Private: true}, cfg)
	fmt.Printf("err=%v\n", err)
}
EOF
go run /tmp/bb_check.go
```

With the valid blob from above, `err=<nil>` (construction succeeded; both fields resolved from the keychain).

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

Re-run the ad-hoc program above. It must print an error containing `"not valid JSON"` rather than silently using a literal fallback. This is the R3 guarantee: a broken keychain entry is not masked by stale literals.

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
./bin/e2e-regulated init ai --dir /tmp/e2etest-reg
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

```bash
cat > /tmp/probe_check.go <<'EOF'
package main

import (
	"fmt"

	_ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

func main() {
	fmt.Println("Available:", credentials.KeychainAvailable())
	fmt.Println("Probe:   ", credentials.Probe())
}
EOF
go run /tmp/probe_check.go
```

- `Available=false` → the `pkg/credentials/keychain` subpackage is not linked (missing blank import, or you're on the stub build).
- `Available=true` / `Probe=false` → backend is compiled in but the live round-trip failed. On Linux this is almost always a missing or locked Secret Service provider; on macOS, a locked login keychain; on Windows, a disabled Credential Manager.

**`secret-tool: command not found` on Linux.** Install `libsecret-tools` (Debian/Ubuntu) or `libsecret` (Fedora/Arch). You can also verify entries via GNOME Seahorse (GUI) or `dbus-send` queries.

**I'm on a server and want to verify behaviour that needs a reachable keychain.** Short of installing GNOME Keyring or similar, you can run the Gherkin suite against the mock backend (`just test-e2e`) — it covers the same paths without a real OS keychain. Scenarios that truly require the live round-trip (1, 2, 5) are only meaningful on a desktop or a macOS/Windows workstation.

**Corrupt-JSON test isn't triggering.** Check you're pointing at the right keychain entry — service must be `e2e`, account must be `bitbucket.auth`. The resolver only inspects `bitbucket.keychain` config entries, not `bitbucket.<field>.env` or `bitbucket.username`.

## Related

- [`docs/components/credentials.md`](../../components/credentials.md) — architecture reference.
- [`docs/how-to/configure-credentials.md`](../../how-to/configure-credentials.md) — end-user configuration guide.
- [`2026-04-02-credential-storage-hardening.md`](../specs/2026-04-02-credential-storage-hardening.md) — spec driving this work; each scenario maps to a requirement (R1–R6).
