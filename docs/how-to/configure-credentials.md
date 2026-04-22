---
title: Configure credentials
description: How to pick a storage mode for AI and VCS credentials in a GTB-based tool, and how to migrate existing literal-mode configs to the recommended env-var reference mode.
date: 2026-04-20
tags: [how-to, credentials, security, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure credentials

This guide covers the three storage modes GTB supports for user-supplied secrets (AI API keys, VCS tokens, Bitbucket app passwords) and how to pick the right one for your environment.

If you want the background on why we built this, see the [Credential Storage Hardening spec](../development/specs/2026-04-02-credential-storage-hardening.md).

## The three modes in one sentence

| Mode | Config sees | Secret lives |
|------|-------------|--------------|
| **Env-var reference** (default, recommended) | The **name** of an env var | Your shell profile, `direnv` file, or CI secret injection |
| **OS keychain** (opt-in via blank import) | A `<service>/<account>` reference | OS keychain (macOS Keychain / Linux Secret Service / Windows Credential Manager) |
| **Literal** (legacy) | The secret itself | The config file — `~/.<toolname>/config.yaml` |

Keychain mode is only offered if the tool's `main` package imports `github.com/phpboyscout/go-tool-base/pkg/credentials/keychain`. Regulated builds omit the import — the tool then runs with a stub backend that never reaches a session bus or platform keychain API, and Go's linker dead-code elimination keeps `go-keyring`, `godbus`, and `wincred` out of the shipped binary.

## When to pick which mode

### Local development

**Pick env-var reference.** It's the default for a reason:

- The config file stays committable to your dotfiles repo — it contains only an env var name, no secret.
- Rotating a token only requires changing one env var, not re-running `init`.
- If you use `direnv`, per-project env vars are supported out of the box.

```bash
# Your shell profile or .envrc
export ANTHROPIC_API_KEY=sk-ant-api03-...

# Run the wizard; accept the defaults.
mytool init ai
# -> pick provider → pick "Environment variable reference (recommended)"
# -> accept "ANTHROPIC_API_KEY" as the env var name
```

Your `~/.mytool/config.yaml` will then contain:

```yaml
ai:
  provider: claude
anthropic:
  api:
    env: ANTHROPIC_API_KEY
```

No secret on disk.

### CI / CD

**Env-var reference is the only mode accepted under `CI=true`.** The wizard refuses literal mode to stop credentials from landing in build artefacts or job logs.

For GitHub Actions:

```yaml
- run: mytool ...
  env:
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
    GITHUB_TOKEN:      ${{ secrets.GITHUB_TOKEN }}
```

For GitLab CI: add the secrets as protected CI/CD variables and they become env vars automatically.

If your tool uses a custom env-var name via the wizard (e.g. `MYAPP_ANTHROPIC_KEY`), set that name in the CI secret — GTB reads whatever name you chose.

### Containers / Kubernetes

**External secret injection.** GTB is not in the loop:

- Kubernetes Secrets mounted as env vars via `envFrom` or `env[].valueFrom.secretKeyRef`.
- CSI secret store drivers (Vault, AWS Secrets Manager, Azure Key Vault).

Bake the tool's config into the image as env-var-reference mode only — never literal. The config file is immutable; the env vars are supplied at runtime.

### Throwaway or air-gapped environments

**Literal is acceptable** if you accept the plaintext-on-disk risk. The wizard still writes the config file with `0600` permissions and the `config list` command masks the value — but if the file is backed up, sync'd to a dotfile repo, or read by malware with user privileges, the secret is exposed.

The doctor command will warn:

```
$ mytool doctor
[WARN] Credential storage
       1 literal credential(s) in config
       Key(s): anthropic.api.key. Migrate to env-var references
       (e.g. anthropic.api.env: ANTHROPIC_API_KEY) …
```

## Multi-tool workstation strategy

Running several GTB-based tools on the same workstation? You have two options:

**Option A — share the provider-standard env var.** Every tool reads `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc. Simple, but all tools use the same key.

**Option B — per-tool env var names.** During each tool's `init ai` wizard, override the default name with a tool-specific one:

```
Environment Variable Name: MYTOOL_ANTHROPIC_API_KEY
```

Then in your shell profile:

```bash
export MYTOOL_ANTHROPIC_API_KEY=sk-ant-...
export OTHERAPP_ANTHROPIC_API_KEY=sk-ant-...-different
```

This lets you give different tools different keys (useful for per-project billing, rate limits, or compliance boundaries).

## Prefix-aware env binding (advanced)

A tool built on GTB can declare an `EnvPrefix` in its `Tool` props (e.g. `MYTOOL`). Viper's `AutomaticEnv` then automatically binds every config key to its `MYTOOL_<UPPER_SNAKE>` env var:

```yaml
# config.yaml
anthropic:
  api:
    key: sk-ant-placeholder
```

Set at runtime:

```bash
export MYTOOL_ANTHROPIC_API_KEY=sk-ant-real-key
mytool ai chat "hi"    # uses the env value, not the YAML placeholder
```

This is orthogonal to the env-var-reference mode — it's automatic for every config key. It's convenient for CI-injected overrides but shouldn't be the primary storage strategy because it still couples the env var name to the tool's prefix, making per-project rotation harder.

## Migrating from literal mode

No automated migration command exists yet (it's coming in Phase 3 as `config migrate-credentials`). Manual steps for now:

1. Identify literal credentials with the doctor:
   ```bash
   mytool doctor
   ```

2. For each flagged key (e.g. `anthropic.api.key`), note the current value:
   ```bash
   mytool config get anthropic.api.key --unmask
   ```

3. Export the value under a provider-standard env var name in your shell profile:
   ```bash
   export ANTHROPIC_API_KEY=<the-value>
   ```

4. Replace the literal with an env-var reference in `~/.mytool/config.yaml`:
   ```yaml
   # before
   anthropic:
     api:
       key: sk-ant-...

   # after
   anthropic:
     api:
       env: ANTHROPIC_API_KEY
   ```

5. Verify:
   ```bash
   mytool doctor
   # Should now show: Credential storage ✓ no literal credentials in config
   ```

## Handling headless servers

The `gtb init github` wizard runs an OAuth device flow by default. On dev servers, containers, or SSH-only hosts where no browser is available, the flow falls back automatically:

1. A warning is logged with the root cause.
2. The wizard prints a personal-access-token creation URL with the required scopes (`repo,read:org,gist`) pre-populated.
3. You visit the URL on any device (your laptop, phone), generate the token, and paste it into the hidden input back on the server.

Your pasted token is stored under `github.auth.value` — exactly where the OAuth-issued token would have landed.

## FAQ

**Q: Can I use both `{provider}.api.env` and `{provider}.api.key` at the same time?**

Yes. The env-var reference always wins — if the referenced env var is set to a non-empty value, it's used. If the env var is unset or empty, the resolver falls through to the literal. This makes rotation safe: change the env var without touching the config file; once the rollout is complete, clear the literal.

**Q: Does the tool ever write my token to the terminal or logs?**

No. Setup wizards collect tokens via hidden password inputs. Every log site that might handle credentials either routes through [`pkg/redact`](../components/redact.md) or explicitly declines to log the value. The `doctor credentials.no-literal` check names offending **keys**, not values.

**Q: What if my env var is named differently on different machines?**

Record the name that's right for each machine when you run `init ai` on that machine. The config file is per-user (`~/.mytool/config.yaml`) — different workstations can point at different env vars.

**Q: Will Phase 2 / Phase 3 break my env-var-reference setup?**

No. The storage modes are additive. Phase 2 adds keychain as a third option, Phase 3 adds a migration command — neither removes or changes the env-var reference path.

**Q: How do I enable OS keychain support in a tool built on GTB?**

Add a blank import of the optional keychain subpackage to your tool's `main`:

```go
// cmd/mytool/main.go
import (
    _ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
)
```

The blank import registers a `go-keyring`-backed backend during package init. From that point on, `credentials.KeychainAvailable()` reports true and the setup wizard offers keychain mode when the OS backend is reachable. To strip keychain support from a regulated build, remove the import (or put it in a `//go:build !nokeychain`-tagged file and build with `-tags nokeychain`).

## Configuring GitHub and Bitbucket credentials

The same three-mode UX appears in `init github` and `init bitbucket`, with storage-specific wrinkles worth knowing about.

### `init github`

The wizard runs OAuth (via the GitHub CLI flow) or falls back to manual PAT entry on headless hosts, then routes the captured token per the selected mode:

- **Env-var mode** displays the token once inside a note field. You copy it into `export GITHUB_TOKEN=<token>` in your shell profile and confirm. Only the env-var reference is persisted. This is the recommended default — your config file stays committable and the token lives where CI platforms already expect it.
- **Keychain mode** stores the token under `<toolname>/github.auth` in the OS keychain and records `github.auth.keychain: <toolname>/github.auth` in the config. No plaintext on disk.
- **Literal mode** writes `github.auth.value: ghp_...`. Refused under CI.

Re-running `init` after a successful configuration short-circuits instead of overwriting — your env-var or keychain mode survives a re-run.

### `init bitbucket`

Bitbucket's dual-credential model is handled natively:

- **Env-var mode** asks for two env var names, defaulting to `BITBUCKET_USERNAME` and `BITBUCKET_APP_PASSWORD`. Writes `bitbucket.username.env` and `bitbucket.app_password.env` to the config. You `export` both in your shell profile.
- **Keychain mode** collects the username and app password in one form (app password is hidden), serialises both into a single JSON blob `{"username": "...", "app_password": "..."}`, and stores it under a single keychain entry. The config records only `bitbucket.keychain: <toolname>/bitbucket.auth`. Corrupt or incomplete blobs abort resolution at use time rather than silently falling back — a repair path through re-running setup is the intended recovery.
- **Literal mode** writes both `bitbucket.username` and `bitbucket.app_password` to the config as plaintext. Refused under CI.

The `--skip-bitbucket` flag (defaults to `true` under CI) hides the wizard entirely when the tool author doesn't want Bitbucket configuration to run during `init`.
