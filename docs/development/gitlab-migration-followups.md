---
title: "GitLab Migration — Operational Follow-ups"
description: "Post-migration status. The release pipeline is fully end-to-end (notarized macOS binaries, signed checksums, homebrew cask publish, GitLab Pages site live). Only the GitHub archive remains."
date: 2026-05-14
tags: [development, migration, gitlab, operations]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# GitLab Migration — Operational Follow-ups

The migration is essentially complete. Latest release **v0.2.2** built end-to-end with notarized macOS binaries, checksums, SBOMs, and a homebrew cask pushed to the GitLab-hosted tap. The docs site is live at https://gtb.phpboyscout.uk.

## 1. Release automation token — ✅ MOSTLY DONE (semantic-release → releaser-pleaser)

**Status:** semantic-release has been replaced by [releaser-pleaser](https://releaser-pleaser.dev/) (Release-MR pattern, GitLab CI/CD component). The `RELEASER_PLEASER_TOKEN` CI/CD variable already exists.

**Remaining checks before the new pipeline can cut a release:**

1. Confirm `RELEASER_PLEASER_TOKEN` is a project access token with **Maintainer** role and scopes **`api`, `read_repository`, `write_repository`** (the CI job token is not sufficient).
2. Set the project's **Merge method** to *Fast-forward merge* and **Squash commits** to *Require* — releaser-pleaser needs these to reliably map commits to MRs.

**Cleanup:** the old `GITLAB_TOKEN` variable was only used by semantic-release (GoReleaser authenticates with the CI job token via `use_job_token: true`). Confirm nothing else references it, then remove it.

## 2. Renovate runner schedule + token — ✅ DONE

**Status:** Daily pipeline schedule `id 4245076` runs at 03:00 UTC on `main` with `RENOVATE_TASK=scan`. `RENOVATE_TOKEN` mirrors `GITLAB_TOKEN`.

**Caveat — Python toolchain bumps:** `renovate.json` disables the `pip_requirements` manager. Renovate bumps the version line in `requirements-lock.txt` but cannot regenerate the SHA-256 hashes that `pip install --require-hashes` enforces, breaking the `pages` job. Bumps to that lockfile stay manual via the recipe in the file's own header. Re-enable when self-hosted `postUpgradeTasks` are wired in.

## 3. GoReleaser secrets — ✅ DONE

**Status:** All required secrets are set as masked + protected CI/CD variables. v0.2.x releases ship with notarized + signed macOS binaries.

- `GTB_OTEL_AUTH` — base64 of `1576673:<token>`, real OTel write token.
- `APPLE_NOTARY_KEY` — base64-encoded `.p8` (App Store Connect API key).
- `APPLE_NOTARY_KEY_ID` — `V89487UH6J`.
- `APPLE_NOTARY_ISSUER_ID` — `cff1b5c3-a4bf-4adc-b3cd-db492f195207`.
- `APPLE_DEV_CERT` — base64 of the re-encrypted `.p12` (PKCS#12 modern cipher).
- `APPLE_DEV_CERT_PASSWORD` — set; recommend rotating since it was shared in chat during setup.

## 4. Homebrew tap — ✅ DONE (migrated to GitLab)

**Status:** Mirrored `github.com/phpboyscout/homebrew` → `gitlab.com/phpboyscout/homebrew`. The `homebrew_casks` block in `.goreleaser.yaml` pushes to the GitLab tap over SSH using a dedicated Ed25519 deploy key (registered on the tap with `can_push=true`). Private key lives in the `HOMEBREW_TAP_SSH_KEY` CI variable.

Users install with:

```bash
brew tap phpboyscout/homebrew https://gitlab.com/phpboyscout/homebrew.git
brew install --cask gtb
```

## 5. Branch protection — ✅ DONE (Free-tier subset)

**Status:** `main` is protected (Maintainer-level push + merge, no force-push, no deletions). Project settings enforce FF-only merges, pipelines must pass, discussions resolved before merge.

**Pending (Premium):** Conventional-Commits push-rule regex and required-status-check enforcement at the protected-branch level. Merge-must-pass-pipeline currently covers the same window.

## 6. GitLab Pages custom domain — ✅ DONE

**Status:** `gtb.phpboyscout.uk` verified, Let's Encrypt cert issued (CN=gtb.phpboyscout.uk, valid through Aug 2026), site responds HTTP 200. `pages_access_level=enabled` (previously `private`, which made every request hit GitLab's auth redirect).

DNS resolves through Cloudflare proxy in front of GitLab Pages — TLS terminates at Cloudflare. No further action.

## 7. Archive the GitHub repository — ⚠️ STILL OUTSTANDING

This is the last step. Once you're confident the GitLab migration is stable:

- `Settings → Archive this repository` on `github.com/phpboyscout/go-tool-base`.
- Edit the GitHub README on the archived repo to point at `https://gitlab.com/phpboyscout/go-tool-base` and pin an issue titled "Moved to GitLab" with the new URL.
- The repo stays read-only forever; existing `go get github.com/phpboyscout/go-tool-base` calls still resolve via the module proxy cache for previously-resolved versions, but new resolution will fail (intentional — module path is now `gitlab.com/phpboyscout/go-tool-base`).

I can run the archive via `gh api -X PATCH repos/phpboyscout/go-tool-base -F archived=true` on your authorisation.
