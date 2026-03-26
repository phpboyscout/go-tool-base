---
title: "Documentation Gaps Specification"
description: "Fill documentation gaps identified in review: version migration guides, coverage badge and CI enforcement, controls health/status documentation, API stability policy, and error catalogue."
date: 2026-03-24
status: IMPLEMENTED
tags:
  - specification
  - documentation
  - developer-experience
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Documentation Gaps Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   IMPLEMENTED

---

## Overview

A review of the GTB documentation identified five gaps that affect developer experience and project adoption:

1. **No version migration guides** -- Releases are tracked via GitHub Releases (no CHANGELOG.md by design), but there is no structured guidance for upgrading between versions when breaking changes occur.

2. **No coverage badge or CI enforcement** -- Test coverage is generated but not visible in the README and not enforced as a CI gate. Coverage regressions can slip in unnoticed.

3. **Controls health/status pattern undocumented** -- The controls package exposes a `WithStatus()` option, but the intended usage pattern and current implementation state are not documented.

4. **No API stability policy** -- Contributors and consumers have no way to know which types and interfaces are considered stable vs. experimental. Semver commitments are implicit.

5. **No error catalogue** -- Sentinel errors are scattered across packages with no central reference. Consumers discovering errors requires reading source code.

---

## Design Decisions

**Migration guides over CHANGELOG**: The project deliberately avoids a CHANGELOG.md because GitHub Releases serves that purpose. Migration guides are different -- they provide step-by-step upgrade instructions, not just a list of changes. They complement releases rather than duplicating them.

**Coverage threshold as a CI gate, not a merge block**: The coverage check should fail the CI run (making it visible) but not block merging. Some changes legitimately reduce coverage (e.g., removing dead code, adding error paths that are hard to test). The threshold is a signal, not a law.

**Status pattern: document the intent, mark current state**: The `WithStatus()` mechanism exists in the controls interface but the HTTP and gRPC implementations are no-ops (addressed in the code quality hardening spec). The documentation should describe the intended pattern so contributors understand the design, while clearly marking the current implementation state.

**Stability tiers over binary stable/unstable**: A three-tier system (Stable, Beta, Experimental) provides more nuance than a simple stable/unstable split. This matches the Go project's own approach and sets clear expectations for each tier.

**Error catalogue as a living document**: Rather than generating the error catalogue from code (which would require tooling and maintenance), it is maintained as a Markdown document. This allows adding context, examples, and handling guidance that code comments cannot easily provide.

---

## Public API Changes

None. This spec is documentation-only with one CI configuration change.

---

## Internal Implementation

### 1. Version Migration Guides

**New directory:** `docs/migration/`

**Template file:** `docs/migration/_template.md`

```markdown
---
title: "Migration Guide: vX.Y to vX.Z"
---

# Migrating from vX.Y to vX.Z

## Breaking Changes

### Change Title
**Package:** `pkg/example`
**Before:**
...
**After:**
...
**Migration:** Step-by-step instructions.

## Deprecations

### Deprecated API
**Package:** `pkg/example`
**Deprecated:** Description of what is deprecated.
**Replacement:** What to use instead.
**Removal planned:** vX.Z+1

## New Features

Brief description of new features relevant to migration.
```

**First migration doc:** `docs/migration/v0.x-to-v1.0.md` (or the next relevant version boundary)

The migration guide should cover:
- Logger interface change (`*log.Logger` / `*slog.Logger` to `logger.Logger`) if the unified logger spec is implemented first
- Port config key changes (`server.port` to `server.http.port` / `server.grpc.port`) from the code quality hardening spec
- gRPC reflection config requirement from the security hardening spec
- Any other breaking changes accumulated since the last tagged release

**MkDocs nav integration:** Add `migration/` section to `mkdocs.yml` nav.

### 2. Coverage Badge and CI Enforcement

**README badge:**

Add a coverage badge to the project README. The badge source depends on the CI platform:

```markdown
<!-- If using Codecov -->
[![Coverage](https://codecov.io/gh/phpboyscout/go-tool-base/branch/main/graph/badge.svg)](https://codecov.io/gh/phpboyscout/go-tool-base)

<!-- If using a local badge (generated in CI) -->
![Coverage](https://img.shields.io/badge/coverage-XX%25-brightgreen)
```

**CI threshold enforcement:**

Add a step to `.github/workflows/test.yaml` that checks coverage against a threshold:

```yaml
- name: Check coverage threshold
  run: |
    THRESHOLD=70
    COVERAGE=$(go test -coverprofile=coverage.out ./... 2>&1 | grep -oP 'coverage: \K[0-9.]+' | tail -1)
    if [ -z "$COVERAGE" ]; then
      # Parse from coverage.out
      COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | tr -d '%')
    fi
    echo "Current coverage: ${COVERAGE}%"
    echo "Threshold: ${THRESHOLD}%"
    if (( $(echo "$COVERAGE < $THRESHOLD" | bc -l) )); then
      echo "::warning::Coverage ${COVERAGE}% is below threshold ${THRESHOLD}%"
      exit 1
    fi
```

The threshold starts conservatively (70%) and can be raised as coverage improves. The step uses `exit 1` to fail the check but GitHub Actions' `continue-on-error` can be used at the job level if desired.

**Coverage report upload:**

If the project uses Codecov or Coveralls, add the upload step:

```yaml
- name: Upload coverage
  uses: codecov/codecov-action@v4
  with:
    file: coverage.out
    flags: unittests
```

### 3. Controls Health/Status Documentation

**Modified file:** `docs/components/controls.md`

Add a section describing the health/status pattern:

```markdown
## Health & Status Checks

The controls package supports health and status checking through the
`WithStatus()` option:

    controls.WithStatus(statusFunc)

### Intended Pattern

Each controllable (HTTP server, gRPC server, custom controls) can register
a status function that reports its health. The controller aggregates these
status reports to provide an overall system health view.

### Current State

!!! warning "Partially Implemented"
    The `WithStatus()` option is wired into the controls lifecycle, but the
    default HTTP and gRPC `Status()` implementations are currently no-ops
    that always return nil. See the
    [Code Quality Hardening spec](../development/specs/2026-03-24-code-quality-hardening.md)
    for the plan to implement meaningful status checks.

### Usage

Status functions conform to `controls.StatusFunc`:

    type StatusFunc func() error

Returning `nil` indicates healthy. Returning an error indicates unhealthy
with the error describing the problem.

### Future Direction

The status mechanism is designed to support:
- HTTP `/healthz` endpoint aggregating all control statuses
- gRPC health checking protocol (`grpc.health.v1`)
- Kubernetes liveness and readiness probes
- Dashboard status pages
```

### 4. API Stability Policy

**New file:** `docs/api-stability.md`

The document defines three stability tiers:

**Stable** -- These types and interfaces will not have breaking changes within a major version. Deprecation requires at least one minor version of notice before removal in the next major version.

| Package | Type/Interface | Since |
|---------|---------------|-------|
| `pkg/props` | `Props` struct | v0.1 |
| `pkg/props` | `LoggerProvider`, `ConfigProvider`, `ErrorHandlerProvider` | v0.1 |
| `pkg/config` | `Containable` interface | v0.1 |
| `pkg/config` | `Container` (public methods) | v0.1 |
| `pkg/logger` | `Logger` interface | v1.0 (pending) |
| `pkg/logger` | `Level`, `Formatter` types | v1.0 (pending) |
| `pkg/controls` | `Controllable` interface | v0.1 |
| `pkg/controls` | `StartFunc`, `StopFunc`, `StatusFunc` types | v0.1 |
| `pkg/errorhandling` | `ErrorHandler` interface | v0.1 |
| `pkg/setup` | `Register*` functions | v0.1 |

**Beta** -- These APIs are functionally complete but may have minor breaking changes in minor versions. Changes will be documented in migration guides.

| Package | Type/Interface | Since |
|---------|---------------|-------|
| `pkg/chat` | Chat client interfaces | v0.x |
| `pkg/cmd/doctor` | Doctor command and check registration | v0.x |
| `pkg/http` | HTTP server functions | v0.x |
| `pkg/grpc` | gRPC server functions | v0.x |

**Experimental** -- These APIs may change significantly or be removed. Do not depend on them in production code without pinning to a specific version.

| Package | Type/Interface | Since |
|---------|---------------|-------|
| `internal/*` | All internal packages | - |

**Semver commitments:**

- Pre-v1.0: Minor versions may contain breaking changes. Patch versions are bug fixes only.
- Post-v1.0: Follows standard Go semver. Breaking changes require a major version bump.
- The `internal/` directory is always unstable regardless of version.

### 5. Error Catalogue

**New file:** `docs/components/errors.md`

The document lists all sentinel errors grouped by package:

```markdown
# Error Catalogue

This document lists all sentinel errors defined across GTB packages.
All errors use `cockroachdb/errors` for wrapping and stack traces.

## `pkg/config`

| Error | Description | Typical Handling |
|-------|-------------|-----------------|
| `ErrConfigNotFound` | Config file does not exist at expected path | Check file path, fall back to defaults |
| `ErrConfigParseFailed` | Config file exists but cannot be parsed | Check file syntax (YAML/TOML/JSON) |

## `pkg/controls`

| Error | Description | Typical Handling |
|-------|-------------|-----------------|
| `ErrControllerAlreadyRunning` | Attempted to start a controller that is already running | Check controller state before starting |
| `ErrControllerNotRunning` | Attempted to stop a controller that is not running | Check controller state before stopping |

## `pkg/http`

| Error | Description | Typical Handling |
|-------|-------------|-----------------|
| (errors documented here after confirming actual sentinel errors in code) |

## `pkg/grpc`

| Error | Description | Typical Handling |
|-------|-------------|-----------------|
| (errors documented here after confirming actual sentinel errors in code) |

## `pkg/errorhandling`

| Error | Description | Typical Handling |
|-------|-------------|-----------------|
| (errors documented here after confirming actual sentinel errors in code) |

## `pkg/setup`

| Error | Description | Typical Handling |
|-------|-------------|-----------------|
| `ErrCommandAlreadyRegistered` | A command with the same name is already registered | Use unique command names |

...
```

The catalogue requires a pass through all packages to enumerate actual sentinel errors. The structure above is the template; the implementation phase will fill in concrete values from the codebase.

---

## Project Structure

```
docs/
├── api-stability.md              <- NEW: API stability policy and tier definitions
├── migration/
│   ├── _template.md              <- NEW: migration guide template
│   └── v0.x-to-v1.0.md          <- NEW: first migration guide (version TBD)
├── components/
│   ├── controls.md               <- MODIFIED: add health/status documentation
│   └── errors.md                 <- NEW: error catalogue
├── development/
│   └── specs/                    <- (this spec)

.github/workflows/
├── test.yaml                     <- MODIFIED: add coverage threshold step

README.md                         <- MODIFIED: add coverage badge
```

---

## Testing Strategy

### Documentation Tests

Documentation changes do not have traditional unit tests, but the following verification steps apply:

| Check | Method |
|-------|--------|
| All new Markdown files render correctly | `zensical build --clean` (fails on broken links, missing pages) |
| Migration template is valid | Manual review of structure |
| Error catalogue matches codebase | `grep -rn 'errors.New\|errors.Newf\|var Err' --include='*.go' pkg/` compared against catalogue entries |
| Coverage badge URL is valid | CI run produces expected badge |
| Coverage threshold works | Intentionally reduce coverage and verify CI fails |

### CI Tests

| Test | Scenario |
|------|----------|
| `TestCoverageThreshold_Above` | Normal test run stays above threshold |
| `TestCoverageThreshold_Below` | Verify the threshold check script correctly identifies coverage below the threshold (tested locally with a modified threshold) |

### Coverage

- This spec does not change Go source code, so coverage targets are not applicable.
- The CI coverage threshold itself starts at 70% and should be reviewed quarterly.

---

## Backwards Compatibility

- **No breaking changes**: This spec is documentation and CI configuration only.
- **Coverage threshold**: May cause existing PRs to show a failing check if coverage is currently below the threshold. The threshold should be set at or slightly below the current coverage level to avoid blocking existing work.
- **MkDocs nav changes**: Adding new pages to the nav does not affect existing pages.

---

## Future Considerations

- **Automated error catalogue generation**: A Go source code analyzer could extract sentinel errors and generate the catalogue automatically. This would ensure the catalogue stays in sync with the codebase. However, automated generation cannot provide the "Typical Handling" guidance, so a hybrid approach (auto-generated list + manual annotations) may be needed.

- **API stability linting**: A tool like `apidiff` (from `golang.org/x/exp`) can detect breaking changes in Go APIs. Integrating this into CI would enforce the stability policy automatically.

- **Coverage trending**: Beyond a simple threshold, tracking coverage over time (via Codecov or similar) provides visibility into whether coverage is improving or degrading across releases.

- **Migration guide automation**: GitHub Release notes could link to the corresponding migration guide automatically, improving discoverability.

- **Deprecation warnings**: Go's `// Deprecated:` comment convention could be enforced via linting to ensure all deprecated APIs are documented in both code and the stability policy.

- **Interactive error catalogue**: The error catalogue could be enhanced with search/filter functionality using MkDocs plugins, making it easier to find specific errors.

---

## Implementation Phases

### Phase 1 -- Migration Guide Infrastructure
1. Create `docs/migration/` directory
2. Create `docs/migration/_template.md` with the migration guide template
3. Create the first migration guide based on accumulated breaking changes
4. Add `migration/` section to `mkdocs.yml` nav

### Phase 2 -- Coverage Badge and CI Enforcement
1. Determine current coverage baseline: `go test -cover ./...`
2. Set threshold at or slightly below current coverage
3. Add coverage threshold check step to `.github/workflows/test.yaml`
4. Add coverage badge to README
5. Optionally configure Codecov/Coveralls integration

### Phase 3 -- Controls Health/Status Documentation
1. Add health/status section to `docs/components/controls.md`
2. Document the intended pattern, current state, and future direction
3. Cross-reference the code quality hardening spec

### Phase 4 -- API Stability Policy
1. Create `docs/api-stability.md`
2. Audit all public types and interfaces for stability classification
3. Document semver commitments
4. Add to `mkdocs.yml` nav

### Phase 5 -- Error Catalogue
1. Enumerate all sentinel errors across packages: `grep -rn 'var Err' --include='*.go' pkg/`
2. Create `docs/components/errors.md` with complete catalogue
3. Add handling guidance for each error
4. Add to `mkdocs.yml` nav

### Phase 6 -- Missing Component Pages and Stale Doc Fixes

A secondary audit (2026-03-25) identified three undocumented packages and
several stale pages. All addressed in this phase.

**New component pages:**

1. Create `docs/components/logger.md` — Logger interface, backends (charm/slog/noop),
   options, slog interoperability, testing
2. Create `docs/components/output.md` — Writer, Format constants, dual-format pattern,
   JSON marshalling, `--output` flag integration
3. Create `docs/components/version.md` — Version interface, Info struct, ldflags wiring,
   CompareVersions, FormatVersionString, IsDevelopment
4. Update `docs/components/index.md` — add logger, output, version rows to the table

**Stale page fixes:**

5. Update `docs/concepts/interface-design.md` (dated Feb 17):
   - Add Logger interface section
   - Add narrow provider interfaces (LoggerProvider, ConfigProvider, ErrorHandlerProvider)
   - Replace monolithic Controllable with the narrowed Runner/StateAccessor/Configurable/ChannelProvider design
   - Fix `SetLogger` / `GetLogger` signatures (`logger.Logger` not `*slog.Logger`)
6. Fix `docs/components/setup/index.md` — Version Management section incorrectly
   attributes `CompareVersions`/`FormatVersionString` to `pkg/setup`; redirect to `pkg/version`
7. Update `docs/concepts/auto-update.md` — clarify that `IsLatestVersion` delegates
   version comparison to `pkg/version`

### Phase 7 -- Critical Staleness Fixes (secondary audit 2026-03-25)

A second pass identified critical type errors across four docs caused by the
unified-logger-abstraction spec (2026-03-23) changing `*slog.Logger` to
`logger.Logger`.

1. `docs/components/controls.md` — fix 7 occurrences of `*slog.Logger` in
   interface definitions, struct fields, factory signatures, and code examples
2. `docs/concepts/service-orchestration.md` — fix 1 reference to `slog.Logger`
3. `docs/components/setup/middleware.md` — fix `WithTiming` and `WithRecovery`
   signatures from `*slog.Logger` to `logger.Logger`
4. `docs/concepts/functional-options.md` — fix `WithLogger` signature and
   `ControllerOpt` parameter type; replace `slog.Default()` with `logger.NewNoop()`
5. `docs/concepts/interface-design.md` — fix `Dump()` → `Dump(w io.Writer)`
6. Create `docs/concepts/logging.md` — logging concept page per unified-logger spec requirement

### Phase 8 -- Verification
1. Run `zensical build --clean` to verify all documentation renders without errors
2. Verify coverage badge displays correctly
3. Verify coverage threshold CI step works
4. Cross-check error catalogue against codebase

---

## Verification

```bash
# Build documentation site
zensical build --clean

# Verify new files exist
test -f docs/migration/_template.md && echo "migration template exists"
test -f docs/api-stability.md && echo "api stability doc exists"
test -f docs/components/errors.md && echo "error catalogue exists"

# Verify coverage threshold is configured
grep -n 'coverage' .github/workflows/test.yaml
# Should show threshold check step

# Verify coverage badge in README
grep -n 'coverage' README.md
# Should show badge markup

# Cross-check error catalogue completeness
# Compare sentinel errors in code vs documented errors
grep -rn 'var Err' --include='*.go' pkg/ | wc -l
grep -c '| `Err' docs/components/errors.md
# Counts should be close (catalogue may group or exclude internal errors)

# Verify documentation nav includes new pages
grep -n 'migration\|api-stability\|errors' mkdocs.yml
```
