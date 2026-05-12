---
title: "GitLab Migration — Operational Follow-ups"
description: "Tasks that must be completed in the GitLab UI / dashboards before the release pipeline can run end-to-end. The code migration itself is done; these are operator-side prerequisites for cutting v0.1.0 on GitLab."
date: 2026-05-11
tags: [development, migration, gitlab, operations]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# GitLab Migration — Operational Follow-ups

The code migration is complete. CI is green for lint, tests, e2e, all security scans, and Pages publishes the docs site. The remaining tasks are dashboard-side configuration that must happen before the first GitLab release can be cut.

## 1. Project access token for semantic-release

The default `gitlab-ci-token` cannot push tags to a protected branch, which is why the `semantic-release` job currently fails with `EGITNOPERMISSION`.

**Action:** In `Settings → Access Tokens → Project access tokens`, create a token with:

- Name: `semantic-release-bot`
- Role: `Maintainer`
- Scopes: `api`, `write_repository`, `read_repository`
- Expiry: 1 year (calendar a renewal)

Then in `Settings → CI/CD → Variables`, add:

- `GITLAB_TOKEN` = the token above, masked + protected.

The semantic-release job picks it up via `process.env.GITLAB_TOKEN`. Once set, push a `feat` or `fix` commit to `main` and the pipeline should advance through to a successful tag + release.

## 2. Renovate runner schedule + token

Renovate runs as a scheduled CI job (`renovate` stage in `.gitlab-ci.yml`). It requires:

- A pipeline schedule in `CI/CD → Schedules`:
  - Pattern: `0 3 * * *` (daily 03:00 UTC)
  - Ref: `main`
  - Variables: `RENOVATE_TASK=scan`
- `RENOVATE_TOKEN` CI/CD variable — the same project access token created above works (api + write_repository), or generate a dedicated token if you want auditability.

## 3. GoReleaser secrets (when ready to ship signed builds)

The `goreleaser` job runs only on `v*.*.*` tags. It needs:

- `GTB_OTEL_AUTH` — base64-encoded `<otel-instance-id>:<otel-token>` for telemetry. Currently injected via `ldflags` from this CI variable.
- `APPLE_DEV_CERT`, `APPLE_DEV_CERT_PASSWORD`, `APPLE_NOTARY_ISSUER_ID`, `APPLE_NOTARY_KEY_ID`, `APPLE_NOTARY_KEY` — required for macOS notarization. Same set as the previous GitHub workflow.

Mark all as masked + protected CI/CD variables.

## 4. Homebrew tap

`.goreleaser.yaml`'s `homebrew_casks` block still points at `github.com/phpboyscout/homebrew`. Three options:

- **Drop Homebrew distribution entirely** — remove the block. Users install via the GitLab raw URL `install.sh` (or `go install`).
- **Move the tap to GitLab** — create `phpboyscout/homebrew` on GitLab, update the `repository:` block. Homebrew supports GitLab-hosted taps via `brew tap phpboyscout/homebrew https://gitlab.com/phpboyscout/homebrew.git`.
- **Keep the tap on GitHub** — accepts that one piece of the toolchain still touches GitHub. Lowest disruption to existing Homebrew users (the tap URL stays stable).

The current config still references the GitHub tap and `HOMEBREW_TAP_GITHUB_TOKEN`; pick one of the above before the first release.

## 5. Branch protection refinement

Main is currently protected at the `Maintainers` push level only. Suggested tightening:

- `Settings → Repository → Protected branches`:
  - `main`: require approvals = 0 (solo maintainer) or 1 (when team grows), enable "code owner approval if pushed", enable "require status checks to pass". Required checks: `lint`, `tests`, `e2e-bdd-tests`, `govulncheck`, `trivy`, `gitleaks`, `osv-scanner`, `analyze`, `pages`.
- `Settings → Repository → Push rules`:
  - Reject force push.
  - Commit message regex: `^(feat|fix|perf|refactor|chore|ci|docs|style|test)(\([\w,-]+\))?!?:\s` to enforce Conventional Commits.

## 6. GitLab Pages custom domain (optional)

Pages publishes to `phpboyscout.gitlab.io/go-tool-base` by default. The previous docs site lived at `gtb.phpboyscout.uk`. To keep the URL stable:

- `Settings → Pages → New Domain` → `gtb.phpboyscout.uk`.
- GitLab issues a TXT verification record and a Let's Encrypt cert automatically.
- Add the verification + ALIAS/CNAME records at your DNS host.

## 7. Archive the GitHub repository

Once the GitLab release pipeline produces v0.1.0 successfully and the docs site is live at the desired domain:

- `github.com/phpboyscout/go-tool-base` → `Settings → Archive this repository`.
- Update the README on the archived repo to point at GitLab. A pinned issue titled "Moved to GitLab" with the new URL helps stragglers.
- The repo stays read-only forever; existing `go get github.com/...` calls still resolve via the module proxy cache.
