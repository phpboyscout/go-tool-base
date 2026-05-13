---
title: "GitLab Migration — Operational Follow-ups"
description: "Status of the post-migration tasks. The release pipeline now runs end-to-end (v0.1.3 published with binaries + checksums); the remaining items below are DNS records and secrets that only the project owner can set."
date: 2026-05-13
tags: [development, migration, gitlab, operations]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# GitLab Migration — Operational Follow-ups

The code migration **and the release pipeline** are both done. v0.1.3 is live on GitLab with goreleaser-produced binaries, SBOMs, and `checksums.txt`. The remaining items are out-of-band — DNS records, optional secrets, and the GitHub archive.

## 1. Project access token for semantic-release — ✅ DONE (using personal PAT as a fallback)

**Status:** `GITLAB_TOKEN` CI/CD variable is set, masked + protected, using the project owner's personal PAT (the GitLab.com Free tier doesn't expose project-access-token creation via API; project access tokens are a Premium feature).

**Recommended upgrade path:** when convenient (or if you move to Premium), create a dedicated `semantic-release-bot` project access token (Maintainer role, scopes `api + write_repository + read_repository`, 1 year expiry) and replace the `GITLAB_TOKEN` variable value. The personal PAT works today but mixes maintainer identity with the release bot.

## 2. Renovate runner schedule + token — ✅ DONE

**Status:** A daily pipeline schedule has been created (`schedule id 4245076`, runs at 03:00 UTC on `main` with `RENOVATE_TASK=scan`). `RENOVATE_TOKEN` is set to the same value as `GITLAB_TOKEN`.

Recommend separating the two tokens for auditability when you have a dedicated bot user.

## 3. GoReleaser secrets — partially done

**Status:**

- `GTB_OTEL_AUTH` — set to the literal placeholder string `placeholder` so the linker `-X` substitution succeeds without crashing. Telemetry is effectively disabled at runtime. Replace with the real base64-encoded `<otel-instance-id>:<otel-token>` whenever you want telemetry back on.
- `APPLE_DEV_CERT`, `APPLE_DEV_CERT_PASSWORD`, `APPLE_NOTARY_ISSUER_ID`, `APPLE_NOTARY_KEY_ID`, `APPLE_NOTARY_KEY` — **not set**. macOS binaries from v0.1.3 are not notarized; downloaders on macOS will hit Gatekeeper warnings. Add these masked + protected CI variables (same values as the previous GitHub workflow) when you want to restore notarization.

## 4. Homebrew tap — pending decision

The `homebrew_casks` block was removed from `.goreleaser.yaml` as part of unblocking the v0.1.3 release. Choose one of:

- **Drop Homebrew entirely** — no further action; users install via `https://gitlab.com/phpboyscout/go-tool-base/-/raw/main/install.sh` or `go install`.
- **Move the tap to GitLab** — create `phpboyscout/homebrew` on GitLab, re-add the block with a GitLab-hosted tap URL.
- **Keep the tap on GitHub** — re-add the block with a `HOMEBREW_TAP_GITHUB_TOKEN` CI variable holding a GitHub PAT scoped to the tap repo only.

## 5. Branch protection — ✅ DONE (Free-tier subset)

**Status:** `main` is protected (Maintainer-level push + merge, no force-push, no deletions). Project settings enforce FF-only merges, pipelines must pass, all discussions resolved.

**Pending (Premium-tier features):**

- Push rule for the Conventional Commits regex — requires GitLab Premium (`/projects/:id/push_rule` returned no-op on Free).
- Required status check enforcement at the protected-branch level — uses GitLab's newer rulesets API; works on Free for public projects, untested here.

For now the merge-must-pass-pipeline setting catches the same window.

## 6. GitLab Pages custom domain — ✅ DONE (DNS pending)

**Status:** `gtb.phpboyscout.uk` is registered in GitLab Pages with `auto_ssl_enabled: true`.

**Action required from you:** add these DNS records at your DNS provider:

```
TXT  _gitlab-pages-verification-code.gtb.phpboyscout.uk  →  1f57181f87c0476964107eee88e8b0ec
CNAME gtb.phpboyscout.uk  →  phpboyscout.gitlab.io
```

After propagation, GitLab will verify the domain and issue a Let's Encrypt cert automatically.

## 7. Archive the GitHub repository

Once the GitLab release pipeline produces v0.1.0 successfully and the docs site is live at the desired domain:

- `github.com/phpboyscout/go-tool-base` → `Settings → Archive this repository`.
- Update the README on the archived repo to point at GitLab. A pinned issue titled "Moved to GitLab" with the new URL helps stragglers.
- The repo stays read-only forever; existing `go get github.com/...` calls still resolve via the module proxy cache.
