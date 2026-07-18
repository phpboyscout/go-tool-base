---
title: Implement a custom credential backend
description: Plug a remote or bespoke secret store (Hashicorp Vault, AWS SSM, 1Password Connect) into a GTB-based tool by implementing the credentials.Backend interface — now documented on the go/credentials microsite.
tags: [how-to, credentials, backend, vault, integration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Implement a custom credential backend

The credential `Backend` interface — and the worked example of implementing one for
a remote store (Hashicorp Vault KV v2, adaptable to AWS SSM / 1Password Connect) —
now lives with the standalone `go/credentials` module:

> **[Implement a custom backend →](https://credentials.go.phpboyscout.uk/how-to/custom-backend/)**

That guide covers the `Backend` contract and its required semantics, the full Vault
example, blank-import vs explicit registration, testing with `credtest`, composing
backends, and current limitations.

## GTB specifics

A tool built on GTB registers a custom backend exactly as any Go program does —
call `credentials.RegisterBackend` (or blank-import a package whose `init()` does)
before the first credential call. It composes with GTB's built-in keychain opt-in:

```go
import (
	_ "gitlab.com/phpboyscout/go/credentials/keychain" // GTB's default keychain backend
	_ "yourtool/internal/vaultinit"                    // your custom backend; later registration wins
)
```

The scaffolded `cmd/<tool>/keychain.go` (from `gtb generate`) is the canonical spot
to add or replace this wiring.

## Related

- [credentials.go.phpboyscout.uk — custom backend](https://credentials.go.phpboyscout.uk/how-to/custom-backend/)
- [Configure credentials](configure-credentials.md)
- [Credentials component](../explanation/components/credentials.md)
