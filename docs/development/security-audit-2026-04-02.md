# Security Audit Report — go-tool-base

**Date:** 2026-04-02  
**Auditor Role:** Senior Security Analyst (Go specialist)  
**Scope:** Full codebase review — authentication, cryptography, input validation, supply chain, information disclosure  
**Branch:** `develop` (commit `50e9cbc`)  
**Status:** Reviewed — all findings triaged with owner responses

---

## Executive Summary

The go-tool-base project demonstrates a **strong security posture overall**. TLS configuration is hardened, HTTP clients use secure defaults, cryptographic operations use `crypto/rand`, and error handling is structured. Several findings were identified and triaged with the project owner. Code fixes, specification drafts, CI tooling, and documentation have been produced as part of this audit.

**Finding counts by severity and resolution:**

| Severity | Count | Spec Drafted | Fixed | Documented | False Positive |
|----------|-------|-------------|-------|------------|----------------|
| HIGH | 3 | 2 | 0 | 0 | 1 |
| MEDIUM | 8 | 2 | 2 | 3 | 1 |
| LOW | 6 | 0 | 1 | 4 | 1 |

---

## Part 1: Findings and Resolutions

### HIGH Severity

#### H-1: Unencrypted Credential Storage at Rest

**Location:** `pkg/config/container.go:116-118`, `pkg/setup/ai/ai.go:327-366`, `pkg/setup/github/github.go:189`

**Description:** API keys for AI providers (Claude, OpenAI, Gemini), GitHub tokens, and Bitbucket app passwords are stored as plaintext YAML/JSON in the user's config file on disk.

**Evidence:**
```go
// pkg/config/container.go:116-118
func (c *Container) WriteConfigAs(filename string) error {
    return c.v.WriteConfigAs(filename)
}
```

**Owner Response:** Agreed this is a concern. In non-development environments, credential management is expected to be handled externally (CSI drivers, Kubernetes Secrets, Vault). The solution must be selective — not all environments need OS keychain integration. The env-var approach already exists but isn't the default setup path.

**Resolution: SPEC DRAFTED**  
Specification: [`docs/development/specs/2026-04-02-credential-storage-hardening.md`](specs/2026-04-02-credential-storage-hardening.md)  
Key proposals: make env-var references the default in the setup wizard, optional OS keychain integration gated behind a build tag, documented trust model across deployment contexts.

---

#### H-2: Agent Tool Executes Arbitrary Commands

**Location:** `internal/agent/tools.go:196`

**Description:** Code at the cited location uses `exec.CommandContext` with a `//nolint:gosec` comment.

**Owner Response: FALSE POSITIVE.** There is no `exec_command` AI tool defined — this was an intentional decision to prevent AI from running arbitrary commands. The cited code is a tool-building helper with the `//nolint` tag because it's a helper function that cannot determine the safety of the constructing call being made. It is not an AI-accessible tool.

**Resolution: NO ACTION REQUIRED**

---

#### H-3: No Checksum Verification for Downloaded Release Binaries

**Location:** `pkg/setup/update.go` (download flow), `pkg/setup/checksum.go`

**Description:** The remote update download path relies solely on HTTPS transport integrity. The local `UpdateFromFile()` path already has SHA-256 verification.

**Owner Response:** Agreed, but noted that checksums hosted at the same VCS provider don't protect against full platform compromise. Needs a full spec to address trust model considerations.

**Resolution: SPEC DRAFTED**  
Specification: [`docs/development/specs/2026-04-02-remote-update-checksum-verification.md`](specs/2026-04-02-remote-update-checksum-verification.md)  
Key proposals: download and verify `checksums.txt` alongside release assets, fail-open by default with strict mode opt-in, cosign keyless verification as a future phase for defense-in-depth.

---

### MEDIUM Severity

#### M-1: Health Endpoints Expose Internal Error Messages

**Location:** `pkg/http/server.go:26-39`, `pkg/controls/controls.go:170`, `pkg/controls/healthcheck.go:62`

**Description:** Health endpoints return raw error messages from service health checks in the `ServiceStatus.Error` field.

**Owner Response:** The emitting of error messages is intentional. It is the developer's responsibility to ensure sensitive information does not leak through these endpoints.

**Resolution: DOCUMENTED (Accepted Risk)**  
- Added "Security Considerations" section to [`docs/how-to/register-health-checks.md`](../how-to/register-health-checks.md)
- Recorded in [`docs/development/security-decisions.md`](security-decisions.md) as accepted risk M-1

---

#### M-2: Stack Traces Logged at Non-Debug Levels

**Location:** `pkg/setup/middleware_builtin.go:50-56`, `pkg/config/container.go:139,141,203`

**Description:** Panic recovery logged full stack traces at ERROR level. Config failures logged stack traces at WARN level.

**Owner Response:** Agreed. These should leverage debug-level logging for stack trace output.

**Resolution: FIXED**  
- `pkg/setup/middleware_builtin.go`: Panic details logged at ERROR (no stack), stack trace logged separately at DEBUG
- `pkg/config/container.go`: Stack traces moved from WARN/ERROR to DEBUG level at all three locations (lines 139, 141, 203)

---

#### M-3: Config File Permissions Not Restrictive

**Location:** `pkg/setup/ai/ai.go`, `pkg/setup/github/github.go`, `pkg/setup/init.go`

**Description:** Config files containing credentials were written with default umask permissions (typically `0644`).

**Owner Response:** Agreed. All config files should get `0600` permissions.

**Resolution: FIXED**  
- `pkg/setup/ai/ai.go`: Added `p.FS.Chmod(targetFile, 0o600)` after `WriteConfigAs`
- `pkg/setup/github/github.go`: Added `p.FS.Chmod(targetFile, 0o600)` after `WriteConfigAs`
- `pkg/setup/init.go`: Added `props.FS.Chmod(targetFile, 0o600)` after `WriteConfigAs` (with warning on failure)

---

#### M-4: Sensitive Data Not Cleared from Memory

**Location:** `pkg/vcs/repo/repo.go:302-315`, `pkg/vcs/bitbucket/release.go:85-86`

**Description:** SSH passphrases and VCS credentials remain in Go string variables for the process lifetime.

**Owner Response:** Agreed this is a limitation of Go's GC runtime. Document as accepted risk.

**Resolution: DOCUMENTED (Accepted Risk)**  
Recorded in [`docs/development/security-decisions.md`](security-decisions.md) as accepted risk M-4.

---

#### M-5: URL Opened via System Command Without Scheme Validation

**Location:** `pkg/telemetry/deletion.go:108-121`

**Description:** The `openURL()` function passes URLs to system commands without validating the scheme.

**Owner Response:** Agreed. Needs a spec.

**Resolution: SPEC DRAFTED**  
Specification: [`docs/development/specs/2026-04-02-url-scheme-validation.md`](specs/2026-04-02-url-scheme-validation.md)  
Key proposal: new `pkg/browser/` package with scheme allowlist (`https`, `http`, `mailto`), replacing both existing call sites.

---

#### M-6: `update --from-file` Reads Arbitrary Paths

**Location:** `pkg/cmd/update/update.go:56-77`

**Description:** The `--from-file` flag accepts arbitrary filesystem paths.

**Owner Response:** Arbitrary paths are by design. Should be documented.

**Resolution: DOCUMENTED (Accepted Risk)**  
Recorded in [`docs/development/security-decisions.md`](security-decisions.md) as accepted risk M-6.

---

#### M-7: Template Data from CLI Flags Not Escaped

**Location:** `internal/generator/skeleton.go:551-557`

**Description:** `text/template` renders scaffolded files with user-provided data. Code locations intentionally unescaped; non-code locations (YAML values, README prose) are not escaped either.

**Owner Response:** Template data is intentionally not escaped for code locations. However, values passed into non-code locations should be reviewed and escaped where appropriate. Needs a spec.

**Resolution: SPEC DRAFTED**  
Specification: [`docs/development/specs/2026-04-02-generator-template-escaping.md`](specs/2026-04-02-generator-template-escaping.md)  
Key proposals: context-specific escape functions (`escapeYAML`, `escapeMarkdown`, etc.), applied via pipe syntax only at non-code template locations.

---

#### M-8: Environment Variable Auto-Binding Could Cause Config Pollution

**Location:** `pkg/config/container.go:25-26`

**Description:** `viper.AutomaticEnv()` allows any matching environment variable to override config.

**Owner Response:** The binding is somewhat intentional, but adding a custom prefix is the right approach. The config package should allow consumers to specify a custom prefix (GTB uses `GTB_`, other tools define their own).

**Resolution: SPEC DRAFTED**  
Specification: [`docs/development/specs/2026-04-02-config-env-prefix.md`](specs/2026-04-02-config-env-prefix.md)  
Key proposals: `WithEnvPrefix` option leveraging Viper's `SetEnvPrefix()`, backward-compatible (no prefix = current behavior), generator scaffolds new tools with a derived prefix.

---

### LOW Severity

#### L-1: Debug-Mode Stack Traces in Error Handler

**Location:** `pkg/errorhandling/handling.go:119-121,134`

**Owner Response:** Intentional for development. Document accordingly.

**Resolution: DOCUMENTED**  
Recorded in [`docs/development/security-decisions.md`](security-decisions.md).

---

#### L-2: Provider Environment Variable Value Logged at Debug Level

**Location:** `pkg/chat/client.go`

**Owner Response:** Acceptable for debugging. Document accordingly.

**Resolution: DOCUMENTED**  
Recorded in [`docs/development/security-decisions.md`](security-decisions.md).

---

#### L-3: No Security Headers in Default HTTP Server

**Location:** `pkg/http/server.go`

**Owner Response:** Intentional. Document and provide how-to guides.

**Resolution: DOCUMENTED**  
Created [`docs/how-to/security-headers.md`](../how-to/security-headers.md) with middleware examples covering HSTS, CSP, X-Content-Type-Options, and X-Frame-Options.

---

#### L-4: SSH Key Discovery Scans Entire `~/.ssh`

**Location:** `pkg/setup/github/ssh.go:132-168`

**Owner Response:** Required for TUI initialisation. Only reads filenames, not contents.

**Resolution: DOCUMENTED (Accepted Risk)**  
Recorded in [`docs/development/security-decisions.md`](security-decisions.md).

---

#### L-5: Machine ID in Telemetry Deletion Emails

**Location:** `pkg/telemetry/deletion.go:97-98`, `pkg/telemetry/machine.go:17-35`

**Owner Response:** Should already be hashed. Verify.

**Resolution: FALSE POSITIVE**  
Verified: `HashedMachineID()` in `pkg/telemetry/machine.go` collects OS machine ID, MAC address, hostname, and username, joins them, SHA-256 hashes the result, and returns only the first 8 bytes as 16 hex characters. Raw identifiable values are never transmitted.

---

#### L-6: `fmt.Printf` for Server URL Instead of Logger

**Location:** `pkg/docs/serve.go:34`

**Owner Response:** Needs to be addressed to use info logging.

**Resolution: FIXED**  
Changed `fmt.Printf("Documentation server starting at %s\n", url)` to `slog.Info("Documentation server starting", "url", url)` using Go's standard library structured logger (no API signature change required).

---

### Informational (Positive Findings)

These security controls are **well-implemented** and require no action:

| Control | Location | Notes |
|---------|----------|-------|
| TLS 1.2+ enforced, AEAD ciphers only | `pkg/http/tls.go:12-28` | X25519 preferred, no weak suites |
| No `InsecureSkipVerify` anywhere | Codebase-wide | Verified via grep |
| HTTPS→HTTP redirect downgrade blocked | `pkg/http/client.go:139` | Explicit check in redirect policy |
| `crypto/rand` used consistently | `pkg/http/retry.go`, `pkg/chat/filestore.go` | No `math/rand` in security paths |
| SHA-256 checksums (local updates) | `pkg/setup/checksum.go` | Proper verification logic |
| HTTP request logging excludes headers/body | `pkg/http/client_middleware.go:60` | Prevents credential leakage |
| Ed25519 SSH keys, 12-char min passphrase | `pkg/setup/github/ssh.go:29` | Modern key type, good defaults |
| OAuth device flow (no local client secret) | `pkg/vcs/github/login.go` | Secure auth pattern |
| Sensitive config value masking in CLI output | `pkg/cmd/config/sensitive.go` | Pattern-based, shows last 4 chars only |
| Hardened HTTP client defaults | `pkg/http/client.go:22-30` | Proper timeouts, connection limits |
| FIPS-mode builds | `.goreleaser.yaml` | GOLANG_FIPS=1 |
| macOS binary notarization | `.goreleaser.yaml` | Apple notarization configured |
| Pre-commit `detect-private-key` hook | `.pre-commit-config.yaml` | Catches accidental secret commits |
| `gosec` linter enabled | `.golangci.yaml` | Catches common Go security issues |

---

## Part 2: CI Security Tooling

### Previously In Place

| Tool | Where | What it covers |
|------|-------|----------------|
| `golangci-lint` (with `gosec`) | CI + pre-commit | Static analysis, common Go vulns |
| `go test -race` | CI | Data race detection |
| `govulncheck` | Local only (`just vuln`) | Known CVEs in dependencies |
| `detect-private-key` | Pre-commit | Accidental secret commits |
| macOS notarization | Release pipeline | Binary signing |

### Added in This Audit

| Tool | Workflow / Location | What it covers |
|------|---------------------|----------------|
| `govulncheck` | `.github/workflows/security.yaml` + `just vuln` | Known CVEs in Go dependencies (now in CI) |
| GitHub CodeQL | `.github/workflows/codeql.yaml` | SAST — deep semantic analysis for Go |
| Trivy | `.github/workflows/security.yaml` + `just trivy` | Dependency & license vulnerability scanning |
| gitleaks | `.github/workflows/security.yaml` + `just gitleaks` | Secret scanning across git history |
| OSV Scanner | `.github/workflows/security.yaml` + `just osv-scan` | Google's vulnerability database for Go modules |
| OpenSSF Scorecard | `.github/workflows/scorecard.yaml` | Repository security health assessment |
| SBOM generation | `.goreleaser.yaml` (sboms section) | Software Bill of Materials for releases |
| Aggregate scan | `just security` | Runs all local security scans in sequence |
| Security policy | `SECURITY.md` | Vulnerability reporting process |

### Remaining Recommendations (Not Yet Implemented)

| Tool | Priority | Notes |
|------|----------|-------|
| SLSA Provenance | P3 | Cryptographic build attestation — consider when pipeline matures |
| GitHub Actions SHA pinning | P3 | Currently pinned to major versions; SHA pinning + Renovate for supply chain hardening |

---

## Part 3: Action Items

### Immediate (Code fixes applied in this audit)

| # | Item | Status | Files Changed |
|---|------|--------|---------------|
| 1 | M-2: Move stack traces to DEBUG level | **DONE** | `pkg/setup/middleware_builtin.go`, `pkg/config/container.go` |
| 2 | M-3: Restrict config file permissions to 0600 | **DONE** | `pkg/setup/ai/ai.go`, `pkg/setup/github/github.go`, `pkg/setup/init.go` |
| 3 | L-6: Replace fmt.Printf with slog.Info | **DONE** | `pkg/docs/serve.go` |
| 4 | CI: Security scanning workflows | **DONE** | `.github/workflows/security.yaml`, `codeql.yaml`, `scorecard.yaml` |
| 5 | CI: SBOM generation in releases | **DONE** | `.goreleaser.yaml` |
| 6 | CI: Local security scan recipes | **DONE** | `justfile` (trivy, gitleaks, osv-scan, security) |
| 7 | Security policy | **DONE** | `SECURITY.md` |
| 8 | Documentation: Accepted risks | **DONE** | `docs/development/security-decisions.md` |
| 9 | Documentation: Security headers how-to | **DONE** | `docs/how-to/security-headers.md` |
| 10 | Documentation: Health check security | **DONE** | `docs/how-to/register-health-checks.md` |

### Specs Requiring Review and Approval

| # | Spec | File | Key Decision Points |
|---|------|------|---------------------|
| 11 | H-1: Credential storage hardening | `docs/development/specs/2026-04-02-credential-storage-hardening.md` | Keychain build tag vs runtime detection; default env var naming; GitHub OAuth flow interaction |
| 12 | H-3: Remote update checksum verification | `docs/development/specs/2026-04-02-remote-update-checksum-verification.md` | Strict mode default; cosign vs GPG for Phase 2; Bitbucket handling |
| 13 | M-5: URL scheme validation | `docs/development/specs/2026-04-02-url-scheme-validation.md` | Whether AllowedSchemes should be configurable; wrap vs replace cli/browser |
| 14 | M-7: Generator template escaping | `docs/development/specs/2026-04-02-generator-template-escaping.md` | Input validation for Name/Host/Org fields; escape function granularity |
| 15 | M-8: Config env var prefix | `docs/development/specs/2026-04-02-config-env-prefix.md` | Breaking vs non-breaking API approach; prefix validation; default for new tools |

### Follow-up Tasks (Post Spec Approval)

| # | Task | Depends On | Estimated Scope |
|---|------|-----------|-----------------|
| 16 | Implement credential storage hardening Phase 1 (env-var default in wizard) | Spec #11 approved | Small — setup wizard changes only |
| 17 | Implement credential storage hardening Phase 2 (optional keychain) | Spec #11 Phase 1 | Medium — new dependency, build tags |
| 18 | Implement remote update checksum verification | Spec #12 approved | Medium — touches all VCS providers |
| 19 | Implement URL scheme validation (`pkg/browser/`) | Spec #13 approved | Small — new package, two call sites |
| 20 | Implement generator template escaping | Spec #14 approved | Medium — template function map, audit all templates |
| 21 | Implement config env var prefix | Spec #15 approved | Medium — config package change, generator update |
| 22 | Review and update `SECURITY.md` contact email | — | Trivial — replace placeholder |
| 23 | Evaluate SLSA provenance for release pipeline | — | Research — no spec needed |
| 24 | Evaluate SHA pinning for GitHub Actions | — | Small — Renovate/Dependabot config |
| 25 | Run `/gtb-verify` on all code changes from this audit | — | Pre-commit gate |

---

*Report generated 2026-04-02. All line numbers reference the `develop` branch at commit `50e9cbc`. Updated with owner responses and resolutions on the same date.*
