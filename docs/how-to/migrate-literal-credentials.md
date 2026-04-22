---
title: Migrate literal credentials off config
description: Use config migrate-credentials to move plaintext credentials out of your tool's YAML config into environment variable references or the OS keychain. Covers both interactive and silent / CI flows.
tags: [how-to, credentials, migration, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Migrate literal credentials off config

If your tool's config still holds secrets in `*.api.key` / `*.auth.value` / `bitbucket.username` + `bitbucket.app_password` form, `config migrate-credentials` moves each of them to an environment-variable reference or to the OS keychain in a single pass.

The command is deliberately safe to run anywhere — dry-run produces a plan without mutation, and re-runs skip already-migrated entries. It is the same tool for interactive workstations and for automated pipelines; only the flags differ.

## TL;DR

```bash
# See what would happen
mytool config migrate-credentials --dry-run

# Migrate to env-var references, interactively (recommended for workstations)
mytool config migrate-credentials

# Migrate in CI / non-interactive, using defaults
mytool config migrate-credentials --yes

# Migrate to the OS keychain (requires keychain backend imported in the tool)
mytool config migrate-credentials --target=keychain --yes
```

## Step-by-step

### 1. Preview

Start with a dry-run to confirm the command found the credentials you expect:

```bash
mytool config migrate-credentials --dry-run
```

Sample output:

```
Migration plan (dry run — no changes written):

  anthropic.api.key → anthropic.api.env = ANTHROPIC_API_KEY (target: env)
  github.auth.value → github.auth.env = GITHUB_TOKEN (target: env)
  bitbucket.username + bitbucket.app_password → bitbucket.username.env = BITBUCKET_USERNAME (target: env)
```

If the plan looks wrong — missing entries, or a credential listed that isn't actually in your config — re-check the loaded config path with `mytool config list` before running for real.

### 2. Run interactively

```bash
mytool config migrate-credentials
```

For each credential the command:

1. Prompts for an env var name (default: the upstream-standard name).
2. Prints the `export <NAME>=...` instructions.
3. Waits for you to confirm you've exported the variable.
4. Verifies the variable is set in the current process.
5. Rewrites the config atomically — the literal is removed and the `.env` reference is added in a single file write.

If you bail at step 3 by answering "No, cancel migration", the config is left untouched for that credential and you can re-run later.

### 3. Skip the wait, override the names

If some env vars are already set via `/etc/profile`, a systemd drop-in, or a password manager's shell integration, skip the verification:

```bash
mytool config migrate-credentials --skip-verify
```

To pin specific env var names (useful when multiple tools share a host and their upstream names collide):

```bash
mytool config migrate-credentials \
  --env-var anthropic.api.key=MYTEAM_ANTHROPIC_KEY \
  --env-var openai.api.key=MYTEAM_OPENAI_KEY
```

### 4. Silent migration for CI/CD

Drop every prompt:

```bash
mytool config migrate-credentials --yes
```

`--yes` implies `--skip-verify` — the command trusts that the CI platform has injected the required env vars before the tool runs. Combine with `--env-var` to pin names explicitly, or with `--target` to pick a different destination mode.

### 5. Migrate to the OS keychain

The keychain target requires the tool's `main` to blank-import `github.com/phpboyscout/go-tool-base/pkg/credentials/keychain`. Without that, the command refuses:

```
Error: keychain target requested but no keychain-capable Backend is registered
Hint: Import github.com/phpboyscout/go-tool-base/pkg/credentials/keychain in your
      tool's main, or pass --target=env.
```

Once a backend is registered:

```bash
mytool config migrate-credentials --target=keychain --yes
```

Each literal credential becomes a keychain entry under `<toolname>/<account>`. Bitbucket's dual credentials are combined into a single JSON-blob entry under `<toolname>/bitbucket.auth`.

## Cascading the default from config

Set the default target per tool so you don't need `--target` on every invocation:

```yaml
# config.yaml
credentials:
  migrate:
    default_target: keychain
```

Explicit `--target` on the command line still wins over the cascade.

## What if it's interrupted?

The config file rewrite is atomic (temp-file + rename), so a SIGINT mid-migration leaves the original `config.yaml` in place. Re-run the command to resume — already-migrated credentials skip cleanly, and partial-progress (e.g. the keychain Store succeeded but the rewrite didn't) is visible in the second run's dry-run preview.

## FAQ

**Q: Can I migrate back to literal mode?**

No — that's by design. Literal storage is what the command moves *away* from. If you genuinely need a literal value (throwaway environment, reproducing a bug) set it manually with `config set <key> <value>`.

**Q: Does the command touch secrets that were already env-refs or keychain-refs?**

No. Only plaintext literals (`*.api.key`, `*.auth.value`, Bitbucket literals) are scanned. Existing `.env` / `.keychain` entries are preserved verbatim.

**Q: My Bitbucket username has no app_password or vice versa — what happens?**

The command still migrates the half you have (to env-var mode), but the keychain target requires both halves to be present. Partial-pair keychain migration errors with a message explaining which half is missing.

**Q: What permissions does the rewritten config file have?**

`0600` — owner read/write, nothing for group or world. Matches the initial setup-wizard invariant (R4 in the hardening spec).

## Related

- [Configure credentials](configure-credentials.md) — pick the storage mode when running `init`
- [Custom credential backend](custom-credential-backend.md) — implement a Vault / AWS SSM / 1Password backend
- [Migration guide for v1.12](../migration/v1.12-credential-storage.md) — full version upgrade notes
