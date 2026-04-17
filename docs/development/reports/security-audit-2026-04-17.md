# Security Audit Report — go-tool-base (Secondary)

**Date:** 2026-04-17
**Auditor Role:** Senior Security Analyst (Go specialist)
**Scope:** Full codebase review, focused on changes since the 2026-04-02 audit
**Branch:** `docs/spec-security-review` (based on `main`)
**Status:** Findings drafted; awaiting triage and spec/fix assignment

---

## Executive Summary

The three-month growth in the codebase since the 2026-04-02 audit — opt-in telemetry with vendor backends, Gemini chat provider, MCP command wiring, snapshot persistence with encryption, race remediation, config env prefix — has surfaced **new areas of risk not covered by the prior audit or by any of the five existing DRAFT specs** (credential storage, checksums + GPG signing, template escaping, URL schemes, env prefix).

This secondary audit enumerates those new issues. The framework's overall posture remains strong — HTTPS-enforced HTTP client, SHA-256 checksums, no `InsecureSkipVerify`, structured errors, pre-commit secret scanning, multiple CI security scanners — but the larger surface area brings new concerns at the boundaries.

**Findings by severity:**

| Severity | Count | New (not in prior audit, not in draft specs) |
|----------|-------|----------------------------------------------|
| HIGH | 6 | 6 |
| MEDIUM | 8 | 7 (1 covered by existing `url-scheme-validation` spec) |
| LOW | 7 | 6 (1 already accepted risk per `security-decisions.md`) |
| False-positives found and excluded | 3 | — |

---

## Part 1: Findings

### HIGH Severity

#### H-1: Path Traversal in Snapshot `FileStore` via Untrusted ID

**Location:** `pkg/chat/filestore.go:78, 84, 131, 165`

**Description:** `FileStore.Save/Load/Delete` construct paths via `filepath.Join(s.dir, id+".json")` where `id` is the `Snapshot.ID` (currently a UUID from `google/uuid.New`) but arrives as a caller-supplied string in `Load` and `Delete`, and as a JSON-unmarshalled field in `Save`. A caller — or a malicious JSON snapshot — that supplies an ID like `../../../etc/secret` causes reads and writes outside `s.dir`.

**Risk:** Arbitrary file read (via `Load`) or delete (via `Delete`) on the local filesystem; write is the most dangerous (could overwrite config files inside the user's home under the right conditions).

**Reproduction:** Pass `"../../evil"` to `FileStore.Load` — `filepath.Join` resolves the traversal and reads outside `s.dir`. Snapshot unmarshalling from a user-provided file would also set `snapshot.ID` to whatever the JSON says.

**Resolution:** SPEC NEEDED.

**Mitigation:**
- Validate IDs in Save/Load/Delete as strict UUID (or an explicit ID character class matching the generator).
- Additionally: after `filepath.Join`, verify the resolved path is a descendant of `s.dir` (compare absolute-clean path prefixes). Belt-and-braces.

---

#### H-2: ReDoS in Bitbucket `filename_pattern` Config

**Location:** `pkg/vcs/bitbucket/release.go:87, 114`

**Description:** `filenamePattern *regexp.Regexp` is compiled from `src.Params["filename_pattern"]` without length or complexity validation. A tool's config can supply a pathological pattern (e.g. `(a+)+b`) that causes catastrophic backtracking during release asset matching, hanging `update`.

**Risk:** Local DoS of the self-update path. Config-source is typically trusted (local YAML) but downstream tools that accept config from less-trusted sources would be exposed.

**Resolution:** SPEC NEEDED or trivial code fix.

**Mitigation:**
- Reject patterns longer than a reasonable bound (e.g. 256 chars).
- Compile in a bounded goroutine with a 100 ms deadline; reject on timeout.
- Document that `filename_pattern` must be trusted input.

---

#### H-3: ReDoS in Docs TUI Search (Regex Mode)

**Location:** `pkg/docs/tui.go:309, 451`

**Description:** When the user toggles regex mode in the docs browser, the search query is compiled as `regexp.Compile("(?i)" + query)` with no length or complexity bounds. A user who types `(a+)+$` (accidentally or deliberately) hangs the TUI.

**Risk:** Self-inflicted local DoS in the interactive docs browser. Lower than H-2 because the attacker is the user, but still a robustness issue — and a vector if a tool ever accepts external search queries.

**Resolution:** Code fix.

**Mitigation:**
- Length cap on query (e.g. 512 chars).
- Pre-run the compile in a bounded goroutine with a timeout; cancel and return "invalid regex" on timeout.
- Consider using RE2 defaults already in play (Go's `regexp` is RE2, but pathological patterns with Go extensions can still be slow).

---

#### H-4: Renovate Auto-Merges Patch Dependency PRs Without Soak Time

**Location:** `.github/renovate.json5` — `gomod-datasource` + `patch` group has `automerge: true, automergeType: "pr"`

**Description:** Patch Go-module updates are auto-merged on PR-open. There is no `minimumReleaseAge`, no required-CI gate beyond the default, and no manual review. A malicious patch release of any indirect dependency (hijacked maintainer, typosquat) becomes a merged main-branch commit within minutes.

**Risk:** Supply-chain injection with minimal detection window. This is a standard class of attack — `event-stream`/`node-ipc`/`ctx` incidents in npm/pypi all followed this pattern.

**Resolution:** Config change.

**Mitigation:**
- Add `minimumReleaseAge: "3d"` to the patch rule so freshly-published versions cannot auto-merge.
- Or change `automergeType: "pr"` → `"branch"` and require human click-through.
- Require green CI including `govulncheck`, `osv-scanner`, and `trivy` before auto-merge (confirm these are blocking, not advisory).
- Narrow the scope: auto-merge only direct dependencies, not transitive.

---

#### H-5: Goreleaser Action Version Constraint Is a Range, Not a Pin

**Location:** `.github/workflows/goreleaser.yaml` — `with: version: "~> v2"`

**Description:** The goreleaser CLI is pinned with a tilde constraint, resolving to any v2.x.y at run time. The GitHub Action itself is SHA-pinned (`@ec59f474…` — good), but the binary that action downloads is not. A malicious v2 release of goreleaser would execute inside the signing workflow with access to `APPLE_DEV_CERT`, `APPLE_DEV_CERT_PASSWORD`, `APPLE_NOTARY_KEY`, and the GitHub App token.

**Risk:** Signing-key compromise, artefact tamper at build time — worse than VCS compromise because it's _before_ signing.

**Resolution:** Config change.

**Mitigation:**
- Pin `version:` to an exact release (e.g. `"2.10.2"`).
- Bump via Renovate with the `minimumReleaseAge` discipline from H-4.
- Consider verifying the goreleaser binary's checksum out-of-band before invocation.

---

#### H-6: Release Workflow Has No Concurrency Controls

**Location:** `.github/workflows/goreleaser.yaml` — no `concurrency:` block

**Description:** The release workflow can run concurrently if a tag is pushed twice, pushed-then-retried, or if two tags are pushed rapidly. Combined with `use_existing_draft: true` and `replace_existing_artifacts: true` (`.goreleaser.yaml`), this opens a race where two builds can interleave artefacts in the same release.

**Risk:** Artefact mismatch — a release's binary comes from build A but its SBOM or (once Phase 2 ships) signature comes from build B. Integrity verifiers would fail, or worse, verify a tampered binary against a correct sig.

**Resolution:** Config change.

**Mitigation:**
```yaml
concurrency:
  group: release
  cancel-in-progress: false
```

At group `release` scope, simultaneous tag pushes queue rather than race.

---

### MEDIUM Severity

#### M-1: No Request Body Size Limit on Management HTTP Server

**Location:** `pkg/http/server.go` — `Server.Handler` wiring; management endpoints (`/health`, `/ready`, `/live`, `/admin`) lack `http.MaxBytesReader`.

**Description:** `MaxHeaderBytes` is set (1 MB) but no `Server.MaxBytesReader` or equivalent wrapper is applied to request bodies. A large POST to a management endpoint reads unbounded into memory.

**Risk:** Memory DoS of the management plane; blocks health-check traffic.

**Resolution:** Code fix.

**Mitigation:**
- Global middleware wrapping every handler with `r.Body = http.MaxBytesReader(w, r.Body, limit)`.
- Configurable limit via `pkg/controls` config.

---

#### M-2: No gRPC Message Size Limits Configured

**Location:** `pkg/grpc/server.go` — `grpc.NewServer()` called without `grpc.MaxRecvMsgSize` / `MaxSendMsgSize`.

**Description:** Defaults (4 MiB receive, unbounded send) are applied. For a management gRPC endpoint this is acceptable, but the absence of explicit limits means a misconfigured handler could return a large message that stresses clients, or accept an unusually large request.

**Risk:** Memory pressure; predictable-ish defaults may not match operator expectations.

**Resolution:** Code fix.

**Mitigation:**
- Explicitly set `grpc.MaxRecvMsgSize(1 << 20)` and `grpc.MaxSendMsgSize(1 << 20)` as GTB defaults.
- Expose via `pkg/grpc` options so downstream tools can override for high-throughput use cases.

---

#### M-3: OpenAI/Gemini `BaseURL` Is Unvalidated

**Location:** `pkg/chat/openai.go` (`ProviderOpenAICompatible` path), `pkg/chat/gemini.go` (`cfg.BaseURL`)

**Description:** `Config.BaseURL` accepts any string. If set to `http://evil.example.com`, the hardened HTTP client will refuse plaintext (per existing TLS rules), but URLs like `https://evil.example.com` or `https://attacker:pass@api.openai.com` pass through. The `url.User` form can also confuse logging — some HTTP libraries log URLs with embedded credentials.

**Risk:** An attacker who can influence config (via a compromised setup wizard, env var, or checked-in config) can redirect API calls and harvest tokens sent in `Authorization` headers to the attacker-controlled host.

**Resolution:** SPEC NEEDED (small) or direct code fix.

**Mitigation:**
- `url.Parse`, reject `Scheme != "https"`, reject `URL.User != nil`, reject hosts with embedded credentials.
- Warn when `BaseURL` is not a well-known provider host (soft signal).
- Document that `BaseURL` is only for proxy endpoints under the operator's control.

---

#### M-4: Telemetry Backend Response Bodies Are Not Size-Bounded

**Location:** `pkg/telemetry/datadog/datadog.go`, `pkg/telemetry/posthog/posthog.go`, generic HTTP backend in `pkg/telemetry/backend.go`

**Description:** Backends issue `client.Do(req)` and `defer resp.Body.Close()`, but responses are not drained through `io.LimitReader`. A malicious or malfunctioning telemetry endpoint could:
- Return a multi-GiB response that, if ever read in full by `ioutil.ReadAll`-style logic, exhausts memory.
- Return a streaming body without `Content-Length` that keeps a connection alive indefinitely (bounded only by the 5-second HTTP timeout).

Currently the backends don't read the body at all, which is safe — but this is fragile. A future refactor that adds "log response on non-200 for diagnostics" would reintroduce the issue.

**Risk:** Latent DoS; currently mitigated only by the fact that we discard the body.

**Resolution:** Code fix.

**Mitigation:**
- Explicit `io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))` in every backend's response path, so the socket is drained bounded to 1 MiB.
- Encodes the invariant into the code so the next contributor cannot accidentally break it.

---

#### M-5: `TrackCommandExtended` Error Messages May Contain Credentials

**Location:** `pkg/telemetry/telemetry.go` — `TrackCommandExtended(name, args, duration, exitCode, errMsg, extra)`

**Description:** `errMsg` is stored in the telemetry event and dispatched to the configured backend when `ExtendedCollection` is enabled. The method does not sanitise: a typical error like `"failed to POST https://api.example.com/?apikey=sk-abc123: connection refused"` would be captured verbatim and uploaded to Datadog/PostHog/OTLP.

**Risk:** Credential exfiltration to third-party telemetry vendors. The user consented to telemetry but not to shipping their API keys.

**Resolution:** SPEC UPDATE to the credential storage spec, or a dedicated redaction helper.

**Mitigation:**
- Add `pkg/telemetry.SanitizeError(err error) string` that strips known credential patterns (query-string `apikey=*`, URLs with `userinfo`, bearer tokens, 32+ char hex strings that look like keys).
- Require callers of `TrackCommandExtended` to route `errMsg` through the sanitiser.
- Alternatively: have `TrackCommandExtended` apply sanitisation itself so callers cannot forget.

---

#### M-6: OTLP Backend Accepts Credentials in `WithOTelHeaders` Without Warning

**Location:** `pkg/telemetry/backend_otel.go` — `WithOTelHeaders`

**Description:** The OTel exporter is configured with arbitrary headers. Tool authors commonly put bearer tokens or API keys in `Authorization`, `X-API-Key`, etc. If the OTLP endpoint is misconfigured (HTTP, or cert-pinned to a wrong CA), credentials leak. Separately, these headers end up in log output when the HTTP client logs at DEBUG.

**Risk:** Credential leak via downgraded transport or through logging.

**Resolution:** Code fix + doc.

**Mitigation:**
- On `WithOTelHeaders`, inspect keys containing `auth|key|token|secret|bearer` (case-insensitive). Log a WARN pointing at the docs.
- Ensure the HTTP client's request-body logging middleware redacts header values for known-sensitive header names (this may already be done in `pkg/http/client_middleware.go` — verify).

---

#### M-7: GitHub App Token Has Broad Scope Across Two Repos

**Location:** `.github/workflows/goreleaser.yaml` (app-token step), `.goreleaser.yaml` (homebrew_casks token usage)

**Description:** A single `actions/create-github-app-token` step produces a token with write access to both `go-tool-base` and `homebrew`. That single token then flows through goreleaser. A bug, log-leak, or compromise of goreleaser equals write to both repos.

**Risk:** Compromise radius is larger than necessary. Separation of concerns is the defence.

**Resolution:** Workflow restructuring.

**Mitigation:**
- Two separate `create-github-app-token` steps: one for `go-tool-base` (contents:write, releases:write) and one for `homebrew` (contents:write). Expose them as `GORELEASER_RELEASE_TOKEN` and `GORELEASER_HOMEBREW_TOKEN` respectively.
- Goreleaser supports `GITHUB_TOKEN` and `HOMEBREW_TAP_GITHUB_TOKEN` as separate env vars — use this separation.

---

#### M-8: `install.sh` / `install.ps1` GITHUB_TOKEN Scoping Is Undocumented

**Location:** `install.sh:24-29`, `install.ps1:29-35`

**Description:** Both install scripts require `GITHUB_TOKEN` unconditionally. Nothing in the scripts or `docs/installation.md` says what scope is needed. Users habituated to the "just set `GITHUB_TOKEN`" pattern often use a PAT with `repo` scope — massively over-privileged for reading public-release assets.

**Risk:** Over-privileged PAT in use; higher blast radius if the token leaks (shell history, env dumps, misconfigured CI).

**Also important context:** For **public** release assets (GTB is public), no token is strictly needed at all. The token is required only for the API rate-limit window.

**Resolution:** Doc update + optional code change.

**Mitigation:**
- Document that `GITHUB_TOKEN` only needs `public_repo` read or a fine-grained token with `contents: read` on the repo.
- Make the token optional for public repos — the script only uses it to dodge the 60-req/hr anon API limit, which is unlikely to hit on a single install.
- Warn loudly if the provided token appears to have `repo` (admin) scope: the script could call `GET /user` and inspect `X-OAuth-Scopes` before proceeding.

---

### LOW Severity

#### L-1: Agent Tool Command Output Is Wrapped Into Errors Unredacted

**Location:** `internal/agent/tools.go` — `errors.Wrapf(failureErr, "\n%s", string(output))` on failed subprocess calls

**Description:** When an agent-exposed tool's subprocess fails, the full stderr+stdout is folded into the returned error. If the subprocess leaked a secret (env dump, traceback with a path, token in a URL), that leak reaches the AI conversation and any telemetry for the failed invocation.

**Risk:** Secondary leak of environment into the AI context. The existing "no AI exec tool" decision (H-2 from the prior audit) constrains the damage, but tools like `go_get` still invoke `go` which can print `GOPROXY`/`GOAUTH` in verbose mode.

**Resolution:** Code fix.

**Mitigation:**
- Truncate subprocess output to head + tail (N lines each) before wrapping.
- Route through a simple redactor that masks `Authorization: Bearer …`, `token=…`, and long hex strings.

---

#### L-2: Snapshot Encryption Accepts Any 32-Byte `key` Without Validation

**Location:** `pkg/chat/filestore.go` — `WithEncryption([]byte)`

**Description:** The `key` argument must be 32 bytes for AES-256-GCM. If the caller supplies fewer bytes the encrypter fails at first use; but it also accepts keys derived from weak sources (e.g. `[]byte("password1234567890123456789012")`). There is no requirement that the key come from `crypto/rand`, and no helper to generate one.

**Risk:** Developers may use human-readable strings as keys, which are low-entropy and vulnerable to guessing if the ciphertext is ever exposed.

**Resolution:** Code helper.

**Mitigation:**
- Provide `chat.GenerateEncryptionKey() ([]byte, error)` returning 32 random bytes from `crypto/rand`.
- Document strongly that keys must be from `crypto/rand` or derived via a KDF like Argon2id.

---

#### L-3: Telemetry Spill-File Cleanup Errors Are Silently Dropped

**Location:** `pkg/telemetry/spill.go` — `_ = os.Remove(f)` after successful flush

**Description:** If removing a spilled event file after successful delivery fails (permissions, concurrent access, read-only FS), the file persists and the event is re-flushed on next run. In at-most-once mode this causes duplicates; in at-least-once mode it silently accumulates disk usage.

**Risk:** Operational — duplicate events and/or slow disk fill, not security. Included for completeness.

**Resolution:** Code fix.

**Mitigation:** Log the error at WARN on the rare failure path, and include spill directory size in a future `doctor` check.

---

#### L-4: Machine ID Deterministic Across Cloned Systems

**Location:** `pkg/telemetry/machine.go`

**Description:** `HashedMachineID` combines OS machine-id, MAC, hostname, user. On a cloned VM / container image where these are identical, the ID is the same → analytics over-count a single system. This is already noted as accepted risk L-5 in `docs/development/security-decisions.md`.

**Resolution:** Already documented. No action.

---

#### L-5: TOCTOU on Agent Tools' Symlink Check

**Location:** `internal/agent/tools.go` — `isPathAllowed` resolves symlinks then file is opened separately

**Description:** Classic TOCTOU: symlink check and file open are not atomic. An attacker with local write access to the agent's working directory could swap a symlink between the check and the read. Inherent to POSIX filesystem APIs without `openat`.

**Risk:** Local attacker with write access to CWD can escape sandbox.

**Resolution:** Documented limitation; hard to fix without CGO.

**Mitigation:**
- Document the TOCTOU limitation in `docs/about/security.md`.
- In Go 1.24+, consider `os.Root` + `Root.Open` which provides `openat`-equivalent confinement without CGO.

---

#### L-6: `persist-credentials: false` Only Used by Scorecard Workflow

**Location:** `.github/workflows/*.yaml` other than `scorecard.yaml`

**Description:** `actions/checkout` defaults to `persist-credentials: true`, leaving a Git credential in `.git/config` that later steps could (accidentally or maliciously) use. Only the Scorecard workflow explicitly opts out. Read-only workflows (CodeQL, OSV, Trivy, Security, Test) would benefit from the same explicit opt-out.

**Risk:** Low; credential is scoped to the workflow's `GITHUB_TOKEN` permissions, which are already narrow.

**Resolution:** Workflow change.

**Mitigation:** Add `persist-credentials: false` to checkout in every read-only workflow.

---

#### L-7: Python Tooling Pinned to Exact Version but Transitives Unlocked

**Location:** `.github/workflows/goreleaser.yaml:53`, `.github/workflows/docs.yml:24` — `pip install zensical==0.0.33`

**Description:** `zensical` is pinned but its transitive closure is not. A compromised indirect dep would be pulled in at CI runtime.

**Risk:** Supply-chain exposure during docs/release builds.

**Resolution:** Config change.

**Mitigation:** Use `pip install --require-hashes -r requirements-lock.txt` generated via `pip-compile` or `uv pip compile`. Commit the lock file.

---

## Part 2: False Positives Excluded

The initial sweep flagged three items that on closer inspection are not risks:

1. **`install.sh` logging `Download URL: $download_url`** — The URL is GitHub's `browser_download_url` (public) and contains no token. The token is passed in an `Authorization` header on the subsequent `curl`, not embedded in the URL. No leak.

2. **`defaultMaxRedirects = 10` in `pkg/http/client.go`** — Reported as "missing redirect limit". The limit is explicitly set and sensible. The secondary concern about `WithMaxRedirects` accepting negative values can be addressed with a one-line validation but is not a security issue.

3. **Security headers missing on management endpoints** — Already accepted risk `L-3` in `security-decisions.md`. Correctly surfaced by the scanner; the standing decision is unchanged.

---

## Part 3: Recommended Spec / PR Workload

Issues grouped by the right venue for resolution.

### Needs a new spec (2–3 artefacts)

| ID | Subject | Draft-spec name |
|----|---------|-----------------|
| H-1 | Snapshot ID & generic file-identifier validation | `2026-04-17-snapshot-id-validation.md` |
| H-2, H-3 | ReDoS hardening for user/config-supplied regexes | `2026-04-17-regex-hardening.md` |
| M-3 | Chat provider BaseURL validation | `2026-04-17-chat-baseurl-validation.md` |
| M-5, M-6 | Telemetry redaction rules for errors and headers | extend `2026-04-02-credential-storage-hardening.md` with telemetry redaction section |

### Direct code / config fix (no spec)

| ID | Change |
|----|--------|
| H-4 | Renovate `minimumReleaseAge: 3d` on the patch rule; or `automergeType: branch` |
| H-5 | Pin `goreleaser` CLI to an exact version |
| H-6 | Add `concurrency:` block to `goreleaser.yaml` |
| M-1 | Wrap management HTTP handlers with `http.MaxBytesReader` |
| M-2 | Set `grpc.MaxRecvMsgSize`/`MaxSendMsgSize` defaults |
| M-4 | Add `io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))` in telemetry backends |
| M-7 | Split goreleaser workflow into two GitHub-App-token scopes |
| M-8 | Document `GITHUB_TOKEN` scope in `install.sh`/`install.ps1`; make optional for public repos |
| L-1 | Head/tail truncation + simple redactor on agent tool error wrapping |
| L-2 | `chat.GenerateEncryptionKey()` helper |
| L-3 | Log spill cleanup failures at WARN |
| L-6 | `persist-credentials: false` on all read-only workflows |
| L-7 | Generate a `requirements-lock.txt` for Python tooling |

### Documentation / accepted-risk

| ID | Action |
|----|--------|
| L-4 | Confirmed duplicate of existing accepted risk — no change |
| L-5 | Add to `docs/about/security.md` the TOCTOU limitation and `os.Root` future work |

---

## Part 4: Links to Existing Artefacts

- Prior audit: `docs/development/reports/security-audit-2026-04-02.md`
- Accepted-risk log: `docs/development/security-decisions.md`
- Draft specs referenced:
  - `docs/development/specs/2026-04-02-credential-storage-hardening.md`
  - `docs/development/specs/2026-04-02-generator-template-escaping.md`
  - `docs/development/specs/2026-04-02-remote-update-checksum-verification.md` (with Phase 2 GPG + WKD expansion)
  - `docs/development/specs/2026-04-02-url-scheme-validation.md`
  - `docs/development/specs/2026-04-02-config-env-prefix.md` (IMPLEMENTED)

---

*Report generated 2026-04-17. All line numbers reference the branch `docs/spec-security-review` HEAD. Findings validated by reading the cited source; three candidate findings excluded as false-positives after manual verification.*
