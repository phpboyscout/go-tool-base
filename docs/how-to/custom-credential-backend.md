---
title: Implement a custom credential backend
description: How to plug a remote or bespoke secret store (Hashicorp Vault, AWS SSM, 1Password Connect) into a GTB-based tool by implementing the credentials.Backend interface and registering it at startup. Worked example uses Vault's KV v2 engine.
tags: [how-to, credentials, backend, vault, integration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Implement a custom credential backend

`pkg/credentials` ships with two backends out of the box: a stub that returns `ErrCredentialUnsupported` (regulated-default) and a go-keyring wrapper in `pkg/credentials/keychain` (desktop/laptop). Tool authors who need a different secret store — Hashicorp Vault, AWS Secrets Manager / SSM Parameter Store, GCP Secret Manager, 1Password Connect, a custom corporate store — implement the `credentials.Backend` interface and register their implementation at process startup.

This guide walks through the full shape of a custom backend using Hashicorp Vault's KV v2 secrets engine as the worked example. The same pattern applies to any other store.

## When to build one

Consider a custom backend when:

- Your organisation mandates a specific secret store (Vault, cloud-native KMS, internal HSM-backed service) for AI credentials or VCS tokens.
- You want centralised rotation, audit logs, or policy-based access that the OS keychain cannot provide.
- You're running many instances of a GTB-built tool across a fleet and need a single source of truth.

Stick with the built-in backends when:

- You're a single-user desktop tool (OS keychain is fine).
- You only need env-var references (already the default storage mode).

## The `Backend` contract

```go
type Backend interface {
    Store(ctx context.Context, service, account, secret string) error
    Retrieve(ctx context.Context, service, account string) (string, error)
    Delete(ctx context.Context, service, account string) error
    Available() bool
}
```

Required semantics for each method:

| Method | Must do | Must not do |
|--------|---------|-------------|
| `Store` | Write a secret under `service/account`. Overwrite any existing entry. Return `ctx.Err()` on cancellation before commit. | Log `secret`. Embed `secret` in error messages. |
| `Retrieve` | Return the stored value when present. Return `ErrCredentialNotFound` when the backend is reachable but the entry does not exist. Wrap other failures. | Distinguish "missing" by returning an empty string with nil err — resolvers rely on the sentinel to fall through cleanly. |
| `Delete` | Remove the entry if present. Return `nil` when the entry is missing (idempotent). | Surface a "not found" error — it confuses callers that re-run setup. |
| `Available` | Report whether the backend *could* satisfy calls right now, based on a cheap static check (initialised, token present, connection pooled). | Perform I/O — that is what `credentials.Probe(ctx)` is for. |

Return-value conventions:

- `ErrCredentialNotFound` — backend healthy, entry absent. Callers fall through to the next resolution step.
- `ErrCredentialUnsupported` — backend is the stub. Callers fall through.
- Any other error — real failure (auth, network, permission). Callers still fall through for VCS/chat resolvers but the error may be surfaced by direct `credentials.Store/Retrieve/Delete` callers such as the setup wizard.

## Worked example: Hashicorp Vault (KV v2)

This implementation uses the official `github.com/hashicorp/vault/api` client, KV v2 at mount path `secret/`, token-based auth. The same structure adapts to approle, Kubernetes, or cloud IAM auth with only the constructor changing.

### 1. Skeleton

Create `yourtool/credentials/vault/backend.go`:

```go
// Package vault provides a credentials.Backend backed by Hashicorp
// Vault's KV v2 secrets engine.
package vault

import (
	"context"
	stderrors "errors"
	"fmt"

	"github.com/cockroachdb/errors"
	vaultapi "github.com/hashicorp/vault/api"

	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

// Backend writes secrets under <mount>/data/<prefix>/<service>/<account>.
// The KV v2 engine places each secret's payload under a "data" key; we
// stash the single-value secret as {"value": "<secret>"} to match the
// simple Store/Retrieve/Delete contract.
type Backend struct {
	client *vaultapi.Client
	mount  string // e.g. "secret" (no leading/trailing slash)
	prefix string // e.g. "mytool" — namespaces the tool's entries
}

// Config configures the Vault backend.
type Config struct {
	Address string // VAULT_ADDR; e.g. "https://vault.corp:8200"
	Token   string // VAULT_TOKEN; the app's token
	Mount   string // KV v2 mount path; defaults to "secret"
	Prefix  string // namespace inside the mount; defaults to "gtb"
}

// New builds a Backend from Config. Returns an error if the Vault
// client cannot be constructed; Vault round-trips happen lazily on
// first call.
func New(cfg Config) (*Backend, error) {
	vc := vaultapi.DefaultConfig()
	if err := vc.Error; err != nil {
		return nil, errors.Wrap(err, "default vault config")
	}

	if cfg.Address != "" {
		vc.Address = cfg.Address
	}

	client, err := vaultapi.NewClient(vc)
	if err != nil {
		return nil, errors.Wrap(err, "vault.NewClient")
	}

	if cfg.Token != "" {
		client.SetToken(cfg.Token)
	}

	mount := cfg.Mount
	if mount == "" {
		mount = "secret"
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "gtb"
	}

	return &Backend{client: client, mount: mount, prefix: prefix}, nil
}

// path returns the KV v2 logical path for a secret. KV v2 maps
// <mount>/<key> to <mount>/data/<key> for reads/writes; using the
// high-level KVv2 API abstracts this, but we build paths manually to
// keep the dependency minimal.
func (b *Backend) path(service, account string) string {
	return fmt.Sprintf("%s/data/%s/%s/%s", b.mount, b.prefix, service, account)
}
```

### 2. Implement the methods

```go
// Store writes a KV v2 entry at <mount>/data/<prefix>/<service>/<account>
// with payload {"value": secret}.
func (b *Backend) Store(ctx context.Context, service, account, secret string) error {
	_, err := b.client.Logical().WriteWithContext(ctx, b.path(service, account), map[string]any{
		"data": map[string]any{"value": secret},
	})
	if err != nil {
		// Never include `secret` in the wrapped error — Vault sometimes
		// echoes the payload in diagnostics.
		return errors.Wrapf(err, "vault.Write %s/%s", service, account)
	}

	return nil
}

// Retrieve reads a KV v2 entry. Returns ErrCredentialNotFound when
// Vault is reachable but the path has no secret (either never written
// or deleted with purge).
func (b *Backend) Retrieve(ctx context.Context, service, account string) (string, error) {
	sec, err := b.client.Logical().ReadWithContext(ctx, b.path(service, account))
	if err != nil {
		return "", errors.Wrapf(err, "vault.Read %s/%s", service, account)
	}

	// KV v2: nil response or nil .Data means the entry doesn't exist;
	// a deleted-but-not-destroyed entry returns non-nil Data with a
	// "metadata" key but empty "data" — treat both as not-found.
	if sec == nil || sec.Data == nil {
		return "", credentials.ErrCredentialNotFound
	}

	data, ok := sec.Data["data"].(map[string]any)
	if !ok || data == nil {
		return "", credentials.ErrCredentialNotFound
	}

	v, ok := data["value"].(string)
	if !ok {
		return "", credentials.ErrCredentialNotFound
	}

	return v, nil
}

// Delete removes the current version of the KV v2 entry. Idempotent:
// Vault returns 204 for both "was present, now gone" and "was already
// absent" — the client reports no error in either case.
func (b *Backend) Delete(ctx context.Context, service, account string) error {
	_, err := b.client.Logical().DeleteWithContext(ctx, b.path(service, account))
	if err != nil {
		if stderrors.Is(err, vaultapi.ErrSecretNotFound) {
			return nil
		}

		return errors.Wrapf(err, "vault.Delete %s/%s", service, account)
	}

	return nil
}

// Available reports whether the backend is ready to serve requests.
// The check is cheap: we verify the Vault client was constructed with
// a token. For a true liveness signal — "can I round-trip right now?"
// — callers should use credentials.Probe, which performs a
// Set/Get/Delete canary under its own context.
func (b *Backend) Available() bool {
	return b.client != nil && b.client.Token() != ""
}
```

### 3. Register at startup

In your tool's `main` package, register the backend before the first credential call. Two patterns:

=== "Blank-import (side-effect registration)"

    Create `yourtool/cmd/yourtool/vault.go`:

    ```go
    package main

    // Registers the Vault backend at process start. Requires
    // VAULT_ADDR and VAULT_TOKEN in the environment. Delete this
    // file to ship a build that uses the default (stub) backend.
    import _ "yourtool/internal/vaultinit"
    ```

    And `yourtool/internal/vaultinit/vaultinit.go`:

    ```go
    package vaultinit

    import (
        "log"
        "os"

        "github.com/phpboyscout/go-tool-base/pkg/credentials"
        "yourtool/credentials/vault"
    )

    //nolint:gochecknoinits // side-effect registration is the whole point
    func init() {
        b, err := vault.New(vault.Config{
            Address: os.Getenv("VAULT_ADDR"),
            Token:   os.Getenv("VAULT_TOKEN"),
            Prefix:  "yourtool",
        })
        if err != nil {
            log.Fatalf("vault backend: %v", err)
        }

        credentials.RegisterBackend(b)
    }
    ```

=== "Explicit from `main`"

    ```go
    func main() {
        cfg := loadConfigFromCobraFlagsOrEnv() // your own plumbing
        b, err := vault.New(vault.Config{
            Address: cfg.VaultAddr,
            Token:   cfg.VaultToken,
            Prefix:  cfg.VaultPrefix,
        })
        if err != nil {
            log.Fatal(err)
        }

        credentials.RegisterBackend(b)

        if err := cmd.Execute(); err != nil {
            os.Exit(1)
        }
    }
    ```

The blank-import form composes with the `cmd/gtb/keychain.go` pattern — both register their respective backend via `init()`; later registrations win. Use the explicit form if you need the backend's construction to be config-driven (e.g. a flag selects Vault vs OS keychain).

### 4. Test it

Register your Vault backend in an integration test, run the resolver, assert the Store/Retrieve round-trip succeeds. For unit tests of code that *depends on* a credentials backend (resolvers, setup wizards), do not stand up a real Vault — use `pkg/credentials/credtest.MemoryBackend`, which satisfies the same contract and runs entirely in-process:

```go
import (
    "testing"

    "github.com/phpboyscout/go-tool-base/pkg/credentials"
    "github.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
)

func TestResolverReadsFromBackend(t *testing.T) {
    credtest.Install(t) // registers a fresh MemoryBackend, restores the stub on cleanup

    require.NoError(t, credentials.Store(t.Context(), "mytool", "github.auth", "ghp_test"))

    // ... drive pkg/vcs.ResolveToken, pkg/chat.New, etc. and assert they
    // pick up the stored value.
}
```

Round-trip integration tests for your actual Vault implementation belong behind an env-var gate (see [Integration Testing](../development/integration-testing.md)): `INT_TEST_VAULT=1 go test ./yourtool/credentials/vault/...`.

## Composing backends

A common ask is "try Vault first; fall back to OS keychain if Vault is unreachable; fall back to stub if neither responds". `credentials.RegisterBackend` takes a single `Backend` at a time, so composition is implemented by writing a wrapping `Backend`:

```go
type ChainedBackend struct {
    backends []credentials.Backend
}

func (c *ChainedBackend) Retrieve(ctx context.Context, service, account string) (string, error) {
    var lastErr error
    for _, b := range c.backends {
        v, err := b.Retrieve(ctx, service, account)
        if err == nil {
            return v, nil
        }
        if errors.Is(err, credentials.ErrCredentialNotFound) {
            // try next backend
            lastErr = err
            continue
        }
        // unavailable / auth failure — also try next
        lastErr = err
    }
    return "", lastErr
}

// Similar for Store/Delete; Store typically writes to the first
// Available() backend only.
```

Register the chain once at startup:

```go
credentials.RegisterBackend(&ChainedBackend{
    backends: []credentials.Backend{
        mustBuildVaultBackend(),
        keychain.Backend{}, // from pkg/credentials/keychain
    },
})
```

## Current limitations

Worth knowing when designing a custom backend:

- **No built-in retry.** Transient network errors surface as-is; wrap your Vault client with retry logic before registering.
- **No built-in caching.** Each `Retrieve` hits the backend. Caching is your implementation's concern — but be careful not to cache across token rotation.
- **Single active backend.** Composition is via a wrapping `Backend` (see above), not a registration chain.
- **Error taxonomy is thin.** `ErrCredentialNotFound` and `ErrCredentialUnsupported` are the only sentinels. Auth failures and permission denials surface as wrapped errors; callers that want to distinguish them must inspect via `errors.As` on your own error types.

## Related

- [`docs/components/credentials.md`](../components/credentials.md) — architecture reference for `Backend`, `RegisterBackend`, `Probe`.
- [`docs/development/testing/manual-credentials.md`](../development/testing/manual-credentials.md) — scenarios that exercise the active backend end-to-end.
- [`pkg/credentials/credtest`](../../pkg/credentials/credtest/memory.go) — in-process backend for unit testing.
- [`pkg/credentials/keychain`](../../pkg/credentials/keychain/keychain.go) — the canonical `go-keyring` implementation of `Backend`, useful as a reference when writing your own.
