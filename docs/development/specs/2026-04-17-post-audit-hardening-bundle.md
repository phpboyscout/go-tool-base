---
title: "Post-Audit Hardening Bundle"
description: "Bundled small, focused fixes for 13 findings from the 2026-04-17 audit whose individual scope is trivial — workflow configs, size limits, documentation, helper functions. Covers H-4, H-5, H-6, M-1, M-2, M-4, M-7, M-8, L-1, L-2, L-3, L-6, L-7."
date: 2026-04-17
status: APPROVED
tags:
  - specification
  - security
  - hardening
  - bundle
audit-findings:
  - security-audit-2026-04-17.md#h-4
  - security-audit-2026-04-17.md#h-5
  - security-audit-2026-04-17.md#h-6
  - security-audit-2026-04-17.md#m-1
  - security-audit-2026-04-17.md#m-2
  - security-audit-2026-04-17.md#m-4
  - security-audit-2026-04-17.md#m-7
  - security-audit-2026-04-17.md#m-8
  - security-audit-2026-04-17.md#l-1
  - security-audit-2026-04-17.md#l-2
  - security-audit-2026-04-17.md#l-3
  - security-audit-2026-04-17.md#l-6
  - security-audit-2026-04-17.md#l-7
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude
    role: AI drafting assistant
---

# Post-Audit Hardening Bundle

Authors
:   Matt Cockayne, Claude *(AI drafting assistant)*

Date
:   17 April 2026

Status
:   APPROVED

---

## Overview

The 2026-04-17 secondary audit surfaced 21 findings. Four findings (H-1, H-2/H-3, M-3, M-5/M-6) have their own dedicated specs because the design surface is non-trivial. The remaining 13 findings — workflow configuration, size limits, documentation, small helpers, and tidy-ups — are bundled here because:

- Each individual change is a small, mechanical PR (config edit, one-line API addition, or short middleware).
- They share a theme ("tighten the boundaries") and benefit from landing together.
- Bundling avoids the overhead of 13 mini-specs for uncontroversial work.

This spec enumerates each change with its location, rationale, and acceptance criteria. The implementation PR should reference each finding ID in its commit messages.

Each sub-change in this bundle can be implemented and merged independently. They are listed in suggested order (CI/supply-chain HIGH severity first, helper-utility LOW last), but a contributor picking up any one of them does not block on the others.

---

## Scope

| ID | Severity | Area | One-line summary |
|----|----------|------|-------------------|
| H-4 | HIGH | Renovate config | Add `minimumReleaseAge` to patch auto-merge rule |
| H-5 | HIGH | CI workflow | Pin goreleaser CLI to an exact version |
| H-6 | HIGH | CI workflow | Add `concurrency:` block to release workflow |
| M-1 | MEDIUM | HTTP server | Wrap management handlers with `MaxBytesReader` |
| M-2 | MEDIUM | gRPC server | Set `MaxRecvMsgSize` / `MaxSendMsgSize` explicitly |
| M-4 | MEDIUM | Telemetry backends | Drain response bodies via `io.LimitReader` |
| M-7 | MEDIUM | CI workflow | Split the goreleaser GitHub App token into per-repo tokens |
| M-8 | MEDIUM | Install scripts | Document `GITHUB_TOKEN` scope; make optional for public repos |
| L-1 | LOW | Agent tools | Truncate + redact subprocess output before wrapping into errors |
| L-2 | LOW | Snapshot encryption | Provide `chat.GenerateEncryptionKey()` helper |
| L-3 | LOW | Telemetry spill | Log spill-cleanup errors at WARN |
| L-6 | LOW | CI workflow | Set `persist-credentials: false` on all read-only workflows |
| L-7 | LOW | Python tooling | Pin transitive Python deps via hashes |

Findings already accepted as risk (L-4, L-5) are not in this bundle — they are documentation-only updates tracked separately.

---

## Per-finding details

### H-4: Renovate `minimumReleaseAge` on patch auto-merge

**File:** `.github/renovate.json5`

**Change:** The Go dependency patch rule currently has `automerge: true, automergeType: "pr"` with no soak time. Add `minimumReleaseAge: "3d"` so a maliciously-published patch has a 3-day detection window before auto-merging into `main`.

```json5
{
  matchDatasources: ["gomod-datasource"],
  matchUpdateTypes: ["patch"],
  groupName: "go dependencies",
  groupSlug: "go-deps",
  automerge: true,
  automergeType: "pr",
  minimumReleaseAge: "3d", // NEW
},
```

Apply the same `minimumReleaseAge` to the `gomod-datasource` minor rule (which is manual-review already) for consistency. Major updates stay manual with a longer implicit soak.

**Acceptance:** Renovate dashboard confirms the new setting is active. CI continues to run on every Renovate PR.

---

### H-5: Pin goreleaser CLI to an exact version

**File:** `.github/workflows/goreleaser.yaml`

**Change:** Replace `version: "~> v2"` with an exact pin. Bump via Renovate PRs going forward.

```yaml
- name: Run GoReleaser
  uses: goreleaser/goreleaser-action@ec59f474b9834571250b370d4735c50f8e2d1e29 # v7.0.0
  with:
    distribution: goreleaser
    version: "2.10.2" # EXACT — bump via Renovate PR
```

Add to `.github/renovate.json5` a rule that treats `goreleaser-action`'s `version:` input as a dependency to track. Alternatively (simpler): add a comment noting the manual bump convention.

**Acceptance:** CI runs use a pinned binary; a `grep` check in CI (or in a pre-commit hook) fails if the version becomes a tilde/caret range.

---

### H-6: Add `concurrency:` block to release workflow

**File:** `.github/workflows/goreleaser.yaml`

**Change:** Add a top-level concurrency group so simultaneous tag pushes (or a quickly re-pushed tag) queue rather than race.

```yaml
name: goreleaser

on:
  push:
    tags: ["v*"]
  workflow_dispatch:

concurrency: # NEW
  group: release
  cancel-in-progress: false

permissions:
  contents: write
```

`cancel-in-progress: false` because cancelling an in-progress release would leave half-uploaded artefacts.

**Acceptance:** Pushing two tags in quick succession observably serialises the workflow runs.

---

### M-1: HTTP server request body size limit

**File:** `pkg/http/server.go`

**Change:** Wrap every handler mounted on the management HTTP server with `http.MaxBytesReader`. The simplest implementation: a middleware registered once when the server is constructed.

```go
const DefaultMaxRequestBodyBytes int64 = 1 << 20 // 1 MiB

// WithMaxRequestBodyBytes overrides DefaultMaxRequestBodyBytes.
func WithMaxRequestBodyBytes(n int64) ServerOption { /* ... */ }

func (s *Server) maxBytesMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBodyBytes)
        next.ServeHTTP(w, r)
    })
}
```

1 MiB is generous for management traffic (health, readiness, liveness, admin). Downstream tools with different needs can override.

**Acceptance:** A test POSTs a 2 MiB body to `/admin` and asserts the response is `413 Request Entity Too Large`.

---

### M-2: gRPC message size limits

**File:** `pkg/grpc/server.go`

**Change:** Add explicit `grpc.MaxRecvMsgSize` / `MaxSendMsgSize` options with sensible defaults (1 MiB) and expose override options.

```go
const DefaultMaxGRPCMessageBytes = 1 << 20 // 1 MiB

func NewServer(opts ...ServerOption) *grpc.Server {
    cfg := defaultServerConfig()
    for _, o := range opts {
        o(&cfg)
    }

    return grpc.NewServer(
        grpc.MaxRecvMsgSize(int(cfg.maxRecvBytes)),
        grpc.MaxSendMsgSize(int(cfg.maxSendBytes)),
        // ... existing options ...
    )
}
```

**Acceptance:** A unit test sends a 2 MiB message to a test server and asserts the error includes `ResourceExhausted`.

---

### M-4: Telemetry backend response drain

**File:** `pkg/telemetry/datadog/datadog.go`, `pkg/telemetry/posthog/posthog.go`, `pkg/telemetry/backend.go` (generic HTTP backend)

**Change:** After `client.Do(req)`, drain the response body via `io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))`. Enshrines the "never read unbounded" invariant in the code so a future refactor cannot accidentally break it.

```go
resp, err := h.client.Do(req)
if err != nil {
    return nil
}
defer func() { _ = resp.Body.Close() }()
_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

if resp.StatusCode >= httpErrorThreshold {
    h.log.Debug("backend returned non-success status", "status", resp.StatusCode)
}
return nil
```

**Acceptance:** A test with an `httptest.Server` that streams an unbounded body (until 2 MiB) confirms the backend reads no more than 1 MiB and returns normally.

---

### M-7: Split the goreleaser GitHub App token

**File:** `.github/workflows/goreleaser.yaml`, `.goreleaser.yaml`

**Change:** Two separate `actions/create-github-app-token` invocations, producing:

- `GORELEASER_RELEASE_TOKEN` — scope: `go-tool-base` only, `contents: write` on that repo.
- `GORELEASER_HOMEBREW_TOKEN` — scope: `homebrew` only, `contents: write` on that repo.

`.goreleaser.yaml` references the two env vars independently (`GITHUB_TOKEN` for release, `HOMEBREW_TAP_GITHUB_TOKEN` for the Homebrew cask push).

**Acceptance:** Workflow diff shows two `create-github-app-token` steps, each with minimal repo scope. A dry-run release confirms both paths still work.

---

### M-8: Install scripts `GITHUB_TOKEN` scoping

**Files:** `install.sh`, `install.ps1`, `docs/installation.md`

**Change:** Three coordinated edits:

1. **Make `GITHUB_TOKEN` optional for public repositories.** The token is only needed to dodge the 60-req/hr anonymous API rate limit, which is unlikely to trip on a single install. Detect absence and continue without Authorization headers, at the cost of potential rate limiting.
2. **Warn on over-privileged tokens.** Before proceeding, call `GET /user` and inspect `X-OAuth-Scopes`. If any of `repo`, `admin:*`, `delete_repo` is present, print a loud warning recommending a fine-grained token with `contents:read` on the repo only.
3. **Document required scopes in the installation guide.** `docs/installation.md` gains a "Token permissions" subsection.

```bash
# install.sh — revised token handling
if [ -n "${GITHUB_TOKEN:-}" ]; then
  scopes=$(curl -sI -H "Authorization: token ${GITHUB_TOKEN}" "${api_base}/user" \
           | awk -F': ' '/^[Xx]-[Oo][Aa]uth-[Ss]copes:/{print $2}' | tr -d '\r\n')
  case "${scopes}" in
    *repo*|*admin*|*delete_repo*)
      echo "WARNING: GITHUB_TOKEN appears to have broad scopes (${scopes})." >&2
      echo "For installing releases, a fine-grained token with only" >&2
      echo "'contents: read' on this repo is sufficient." >&2
      ;;
  esac
  auth_header="-H Authorization: token ${GITHUB_TOKEN}"
else
  echo "INFO: No GITHUB_TOKEN set. Proceeding anonymously (subject to rate limits)."
  auth_header=""
fi
```

`install.ps1` gets an equivalent block.

**Acceptance:** Running the install script with no `GITHUB_TOKEN` set completes successfully for the public repo. Running with an over-scoped token produces the warning but still installs.

---

### L-1: Agent tool subprocess output truncation and redaction

**File:** `internal/agent/tools.go`

**Change:** Every path that folds subprocess output into an error message truncates to head+tail (e.g. first 50 + last 50 lines) and routes the result through `pkg/redact.String` (added in `2026-04-17-telemetry-redaction.md`). Prevents both log-volume floods and credential leaks.

```go
// headTailTruncate returns head + "\n… [omitted X lines] …\n" + tail.
func headTailTruncate(s string, headLines, tailLines int) string { /* ... */ }

// wrapToolError wraps err with the truncated, redacted output.
func wrapToolError(err error, output []byte) error {
    sanitised := redact.String(headTailTruncate(string(output), 50, 50))
    return errors.Wrapf(err, "\n%s", sanitised)
}
```

Every `errors.Wrapf(err, "\n%s", string(output))` site is replaced by `wrapToolError(err, output)`.

**Acceptance:** A test runs a tool that emits a long stderr containing `sk-xxxxxx...` and confirms the wrapped error contains `sk-***` and is ≤ 110 lines.

---

### L-2: `chat.GenerateEncryptionKey()` helper

**File:** `pkg/chat/filestore.go`

**Change:** Expose a helper that returns 32 random bytes from `crypto/rand`:

```go
// GenerateEncryptionKey returns a fresh 32-byte AES-256 key from
// crypto/rand, suitable for use with WithEncryption. Each snapshot
// store should use a distinct key obtained either from this helper
// or from an operator-controlled source (e.g. KMS).
func GenerateEncryptionKey() ([]byte, error) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return nil, errors.Wrap(err, "reading random bytes")
    }
    return key, nil
}
```

Update `WithEncryption`'s doc comment to recommend this helper explicitly; reject keys of length != 32 with a clearer error than "AES-GCM: invalid key length".

**Acceptance:** Unit test calls `GenerateEncryptionKey()` twice and asserts the results differ and are 32 bytes each.

---

### L-3: Log spill-cleanup errors at WARN

**File:** `pkg/telemetry/spill.go`

**Change:** Replace `_ = os.Remove(f)` with explicit error checks and a WARN log:

```go
if err := os.Remove(f); err != nil {
    c.log.Warn("failed to remove spill file after successful send",
        "file", filepath.Base(f),
        "error", err)
}
```

`filepath.Base(f)` instead of the full path to avoid leaking the spill directory location in logs.

**Acceptance:** A test runs the flush path with a read-only spill directory and confirms the WARN log is emitted exactly once per stuck file.

---

### L-6: `persist-credentials: false` on read-only workflows

**Files:** `.github/workflows/codeql.yaml`, `.github/workflows/security.yaml`, `.github/workflows/tests.yaml` — and any other workflow that does not need git-push access.

**Change:** Every `actions/checkout@...` in a read-only workflow gains:

```yaml
- uses: actions/checkout@<sha>
  with:
    persist-credentials: false
```

Scorecard already has this. Write-enabled workflows (goreleaser, release-please, etc.) keep the default.

**Acceptance:** Manual review of each workflow confirms the right ones got the flag and the write-enabled ones did not.

---

### L-7: Python dependency pinning

**Files:** `.github/workflows/goreleaser.yaml`, `.github/workflows/docs.yml`, new `requirements-lock.txt`

**Change:** Generate a pinned lock file with hashes:

```bash
# One-time, locally:
uv pip compile --generate-hashes requirements.in -o requirements-lock.txt
```

Update workflows to `pip install --require-hashes -r requirements-lock.txt`.

**Acceptance:** CI runs use the lock file; a Renovate rule is added to keep the lock file bumped.

---

## Project Structure

The bundle touches many small places. A consolidated list:

| File | Change summary | Finding |
|------|----------------|---------|
| `.github/renovate.json5` | `minimumReleaseAge: "3d"` on patch rule | H-4 |
| `.github/workflows/goreleaser.yaml` | Exact `version:` pin, `concurrency:` block, two `create-github-app-token` steps, `persist-credentials: false` on non-release jobs | H-5, H-6, M-7, L-6 |
| `.github/workflows/codeql.yaml`, `security.yaml`, `tests.yaml`, `docs.yml` | `persist-credentials: false` | L-6 |
| `.github/workflows/docs.yml`, `goreleaser.yaml` | `pip install --require-hashes -r requirements-lock.txt` | L-7 |
| `requirements-lock.txt` | New | L-7 |
| `requirements.in` | New (source for pip-compile) | L-7 |
| `install.sh`, `install.ps1` | Optional GITHUB_TOKEN, scope warning, docs link | M-8 |
| `docs/installation.md` | "Token permissions" subsection | M-8 |
| `.goreleaser.yaml` | Reference two separate env vars for release vs homebrew tokens | M-7 |
| `pkg/http/server.go` | `MaxBytesReader` middleware + option | M-1 |
| `pkg/http/server_test.go` | 413 test | M-1 |
| `pkg/grpc/server.go` | `MaxRecvMsgSize`, `MaxSendMsgSize` options | M-2 |
| `pkg/grpc/server_test.go` | ResourceExhausted test | M-2 |
| `pkg/telemetry/datadog/datadog.go`, `pkg/telemetry/posthog/posthog.go`, `pkg/telemetry/backend.go` | `io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))` | M-4 |
| Matching `*_test.go` | Unbounded-body test | M-4 |
| `pkg/telemetry/spill.go` | WARN on cleanup error | L-3 |
| `pkg/telemetry/spill_test.go` | Assert WARN emitted | L-3 |
| `internal/agent/tools.go` | `headTailTruncate` + `wrapToolError` helpers | L-1 |
| `internal/agent/tools_test.go` | Truncation + redaction test | L-1 |
| `pkg/chat/filestore.go` | `GenerateEncryptionKey` helper + better-error on bad key length | L-2 |
| `pkg/chat/filestore_test.go` | Round-trip test | L-2 |

---

## Error Handling

No new sentinel errors. Each sub-change follows existing package conventions (`cockroachdb/errors`, `WithHint` where user-facing).

---

## Non-Functional Requirements

### Testing & Quality Gates

| Requirement | Target |
|-------------|--------|
| Line coverage | Maintain existing coverage targets for each package (≥ 90 % for `pkg/`). Small helpers get dedicated tests; workflow/config changes verified by CI smoke runs |
| Race detector | All packages retain `go test -race` green |
| Workflow dry-run | A push to a scratch branch triggers goreleaser with the new config; verify concurrency, split tokens, and exact version pin work end-to-end |
| Install-script smoke | CI job invokes `install.sh` without `GITHUB_TOKEN` set (anonymous); install succeeds |
| Install-script warning | CI job invokes `install.sh` with a deliberately-overscoped PAT; warning is emitted |
| Lock-file hash | `pip install --require-hashes -r requirements-lock.txt` succeeds; hash mismatch test confirms the `--require-hashes` flag is actually active |

### Documentation Deliverables

| Artefact | Scope |
|----------|-------|
| `docs/installation.md` | New "Token permissions" subsection for M-8 |
| `docs/how-to/releasing.md` (if present) | Reference the concurrency invariant and the two-token model for M-7/H-6 |
| `docs/components/http.md` | Document `WithMaxRequestBodyBytes` option (M-1) |
| `docs/components/grpc.md` | Document size-limit options (M-2) |
| `docs/components/telemetry.md` | Short note on spill-cleanup logging (L-3) and response-drain invariant (M-4) |
| `docs/components/chat.md` | Document `GenerateEncryptionKey` usage (L-2) |
| `CLAUDE.md` | One-line pointer under Architecture referencing the telemetry response-drain invariant and the chat encryption-key helper |
| Commit messages | Each commit prefixed with the finding ID (`fix(security): [H-4] …`) so the audit is traceable in git log |

### Observability

| Event | Level | Fields |
|-------|-------|--------|
| Request body exceeds `MaxBytesReader` limit (M-1) | WARN | `path`, `content_length` (header value if present) |
| gRPC message exceeds `MaxRecvMsgSize` (M-2) | WARN | `method`; underlying gRPC `ResourceExhausted` returned to client |
| Spill cleanup failure (L-3) | WARN | `file` (basename only), `error` |
| Over-scoped install token (M-8) | STDERR warning, not logged | — (install script runs before any logger is configured) |

### Performance Bounds

| Metric | Bound | Notes |
|--------|-------|-------|
| `MaxBytesReader` wrapping | O(1) per request | Negligible overhead |
| gRPC size-limit enforcement | O(1) at handler entry | gRPC already bounds via its own options |
| Telemetry response drain | ≤ 1 MiB read per telemetry send | Bounded; does not affect upload latency |
| `GenerateEncryptionKey` | ≤ 1 ms per call | `crypto/rand` is fast |

### Security Invariants

1. The management HTTP server never reads an unbounded request body.
2. The gRPC server never accepts an unbounded message.
3. Telemetry backends never read unbounded response bodies.
4. Subprocess output routed into agent-tool errors is truncated (head+tail) and credential-redacted before leaving `internal/agent/`.
5. Spill-file cleanup failures are visible to operators.
6. Release workflows do not race against themselves; release artefacts match their `checksums.txt` and (Phase 2 of the GPG spec) their signature.
7. Release workflow's GitHub tokens have minimum required scope and are split per repo.
8. Renovate cannot auto-merge a patch dependency until it has existed for at least 3 days.
9. Read-only CI workflows never persist git credentials after checkout.
10. Python tooling runs from a hash-pinned lock file.

---

## Migration & Compatibility

**No public API signature changes.** All options (`WithMaxRequestBodyBytes`, `WithMaxRecvMsgSize`, etc.) are additive. Defaults are chosen to be generous — existing callers should not observe any behaviour difference unless they were sending traffic beyond the thresholds (in which case rejecting was the right thing).

**Workflow migrations happen as PR changes and take effect on merge**; no special coordination needed.

**`install.sh` users who rely on `GITHUB_TOKEN` being required** see no difference — the token is still accepted and used. Users who were unaware they could omit it now have a smoother path.

**Python pinning migration**: the `requirements-lock.txt` is committed; contributors who run `pip-compile` locally to bump deps produce PRs against the lock file.

---

## Implementation Phases

The bundle is organised so contributors can pick up any subset. Suggested order (by severity, then by blast radius):

| Phase | Changes | Effort |
|-------|---------|--------|
| 1 — CI/Supply-chain HIGH | H-4, H-5, H-6 | 2 hours |
| 2 — Server size limits | M-1, M-2 | 3 hours |
| 3 — Telemetry hardening | M-4, L-3 | 2 hours |
| 4 — Release credentials | M-7, M-8 | 3 hours |
| 5 — Agent + helpers | L-1, L-2 | 3 hours |
| 6 — CI hygiene | L-6, L-7 | 2 hours |

Each phase is one PR or several related PRs; contributors can land them independently. Recommended spacing: 1 PR per day across a week.

---

## Resolved Decisions

1. **Bundle rather than 13 mini-specs.** Each change is small and uncontroversial; spec-per-change overhead would dwarf the implementation.
2. **Each sub-change is independently mergeable.** A reviewer can sign off on any single phase without waiting for the others.
3. **Workflow changes before code changes.** Supply-chain HIGH findings (H-4, H-5, H-6) land first because they close the biggest blast-radius gaps with the least code.
4. **Commit-message convention: `fix(security): [FINDING-ID] …`.** Makes the audit-to-commit mapping searchable.
5. **No test rewrites required** — existing tests continue to pass unchanged for every sub-change.
6. **`M-3` is NOT in this bundle** despite looking "small" — it has a dedicated spec (`2026-04-17-chat-baseurl-validation.md`) because the validation rules need explicit design review.
