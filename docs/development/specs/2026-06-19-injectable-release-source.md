---
title: "Injectable Release Source — a parallel-safe DI seam and reusable test double for self-update"
description: "Add a dependency-injected release.Provider seam (setup.WithReleaseProvider + an optional props.Tool.ReleaseProvider field preferred over the global registry), a public pkg/vcs/release/releasetest test double, and a mockery mock — so unit, integration, and E2E self-update tests share one coherent, network-free release source instead of three hand-rolled fakes and a registry-mutation footgun."
date: 2026-06-19
status: APPROVED
tags:
  - specification
  - update
  - self-update
  - release
  - testing
  - dependency-injection
  - generator
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Injectable Release Source Specification

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   19 June 2026

Status
:   APPROVED

---

## Summary

GTB's self-update subsystem (`pkg/setup`) resolves its `release.Provider`
exclusively through the global `release.Register` / `release.Lookup` registry,
keyed by the tool's `ReleaseSource.Type` (`github`, `gitlab`, …). There is no
dependency-injected seam to supply a provider directly. As a result:

1. **Tests cannot inject a provider without mutating global state.** The only
   override point is `release.Register("type", factory)` — a process-global map.
   Tests that mutate it cannot run under `t.Parallel()`, which directly
   contradicts the project rule that bans package-level mocking hooks (see
   `docs/how-to/testing.md` and CLAUDE.md § Testing).
2. **No reusable double exists.** The `pkg/setup` tests share a single local
   `fakeProvider` / `fakeRelease` / `fakeAsset` trio (defined in
   `update_checksum_test.go`). It is package-private, so neither downstream tools
   nor the e2e binary can reach it, and the full-pipeline `update_e2e_test.go`
   builds releases by hand (`createTarGz` + `manifestFor`). *(Correction during
   implementation: the trio is defined once and shared, not "hand-rolled three
   times" as an earlier draft of this spec claimed — so the consolidation win is
   smaller than first stated; see the trimmed Phase 2 below.)*
3. **The E2E binary's "stub release source" is a lie.** `cmd/e2e/main.go`'s
   doc-comment claims it "uses a stub release source so update/init flows never
   reach the network", but it actually configures a live GitLab source. The
   `@cli @smoke` update scenarios therefore reach (or fail to reach) the network,
   and the hermetic update outcomes Phase 9 of the test-coverage closure plan
   wants (already-latest no-op, version-not-found error, checksum/signature
   mismatch abort) cannot be expressed.

This spec adds a **dependency-injected release-source seam** plus a **reusable,
public test double**, so unit, integration, and E2E tests share one
network-free release source. It deliberately keeps the production registry
untouched as the default path — injection is purely additive and backwards
compatible.

This work **unblocks Phase 9** of
`docs/development/plans/2026-06-17-test-coverage-closure.md` and improves the
coherence of the already-completed Phase 6 (self-update Go-level e2e).

## Goals & Non-Goals

### Goals

- A **parallel-safe** way to supply a `release.Provider` to the self-update flow
  without touching the global registry.
- The seam must reach the **CLI/E2E path** (the update command is auto-wired by
  feature flags, so a command-only seam is insufficient — see Design Decision 1).
- A **public, reusable** test double (`pkg/vcs/release/releasetest`) that serves
  releases and assets entirely in memory, including the security-relevant
  variants: corrupt checksum and bad/missing signature.
- **Adopt** the public double in the full-pipeline `update_e2e_test.go` (proving
  it drives the real `Update()` end-to-end and giving Phase 3's BDD its
  reference). The white-box checksum/signature flow tests keep their
  purpose-built local fakes — they assert provider-internal state (a manifest
  call-counter, injected errors) that does not belong on a clean public double,
  and the trio is defined once, not duplicated. (A mockery `release.Provider`
  mock already exists; no new mock work is needed.)
- Make the **generator** and **generated tools** correct and benefit from the
  seam (downstream tools get a hermetic self-update test out of the box).
- **Backwards compatible**: no behavioural change when nothing is injected.

### Non-Goals

- No change to the production provider registry, the existing provider factories
  (`pkg/vcs/github`, `gitlab`, `gitea`, `direct`, …), or the wire format of
  `ReleaseSource`.
- No new on-disk config key to select an injected provider — injection is a
  compile-time/runtime concern (a `Provider` value), never serialised config.
- Not re-testing the full download→extract→**replace** matrix at the BDD layer;
  the binary self-replacement makes that unsafe against the shared E2E binary,
  and Phase 6 already covers it with an injected afero fs. BDD covers only
  outcomes that abort **before** replacement (see Testing Strategy).

## Design Decisions

### Decision 1 — Inject at the DI container (`props.Tool`), not only via an option

`pkg/cmd/update` already exposes a parallel-safe `WithUpdater(NewUpdaterFunc)`
seam, and `setup.NewUpdater` already takes `...UpdaterOption`. Either alone
would serve **direct** callers. But the built-in `update` command is registered
automatically by the feature-flag system in `pkg/cmd/root`; a tool's `main`
(including `cmd/e2e`) never calls `NewCmdUpdate` directly, so it has no place to
thread an option in. The only object every update path already reads is
`props.Tool`. Therefore the seam that reaches E2E **must** ride on `props.Tool`.

We add **both** layers, lowest-friction first:

- `setup.WithReleaseProvider(release.Provider) UpdaterOption` — for direct/unit
  callers (and the implementation primitive).
- `props.Tool.ReleaseProvider release.Provider` (runtime-only field) — read by
  `setup.NewUpdater` and preferred over the registry lookup. This is what makes
  E2E injection flow automatically.

Precedence in `NewUpdater`: explicit `WithReleaseProvider` option →
`props.Tool.ReleaseProvider` → registry `Lookup(type)` (unchanged default).

### Decision 2 — Store a ready `release.Provider`, not a `ProviderFactory`

Open Question 1 resolved toward the simpler shape: the field/option hold a
fully-constructed `release.Provider`. Tests build the double with the data they
need; there is no config subtree to thread. The production registry continues to
use `ProviderFactory(cfg, config)` — untouched. (Rationale: the factory exists to
turn *serialised config* into a provider; an injected provider has no serialised
config to honour.)

### Decision 3 — Import direction

`pkg/vcs/release` does **not** import `pkg/props` (verified). `props` already
depends on `pkg/config`, and `release` depends only on `pkg/config`. Adding a
`release.Provider` field to `props.Tool` introduces the single new edge
`props → release`, which is acyclic. The field is annotated `json:"-" yaml:"-"`,
exactly like the existing behavioural fields `Tool.Help` and
`SigningConfig.EmbeddedKeys`.

### Decision 4 — The test double is public (`pkg/`), not `internal/`

GTB is a framework; downstream tools build their own `update` flows on it. A
public `pkg/vcs/release/releasetest` lets them test their self-update wiring
hermetically too — the same reasoning that keeps `release.Provider` public. This
is the library-first principle from CLAUDE.md.

## Public API

### `pkg/props` — Tool field

```go
// ReleaseProvider, when non-nil, is the release backend the self-update
// subsystem uses, taking precedence over ReleaseSource.Type registry lookup.
// It is a runtime-only injection seam (tests, embedded/custom providers) and
// is never serialised. Production tools leave it nil and are resolved from the
// registry by ReleaseSource.Type as before.
ReleaseProvider release.Provider `json:"-" yaml:"-"`
```

`release` is imported as `gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release`.

### `pkg/setup` — updater option

```go
// WithReleaseProvider injects the release.Provider the SelfUpdater uses,
// bypassing the ReleaseSource.Type registry lookup. Parallel-safe: each call
// site receives its own provider, with no global registry mutation. Takes
// precedence over props.Tool.ReleaseProvider.
func WithReleaseProvider(p release.Provider) UpdaterOption
```

`NewUpdater` resolution becomes:

```go
releaseClient := optionProvider           // WithReleaseProvider, if set
if releaseClient == nil {
    releaseClient = p.Tool.ReleaseProvider // props field, if set
}
if releaseClient == nil {
    factory, err := release.Lookup(vcsProvider) // unchanged default
    // …
    releaseClient, err = factory(sourceCfg, p.Config)
}
```

When a provider is injected, the registry `Lookup` (and thus the
`requireReleaseToken` private-repo token gate that precedes it for
`ReleaseSource.Private`) is **skipped**: an injected provider is self-contained.
This is called out as Open Question 2.

### `pkg/vcs/release/releasetest` — the test double

```go
package releasetest

// Source is a fully in-memory release.Provider for tests. It also implements
// release.ChecksumProvider and release.SignatureProvider. Construct it with
// New and one or more options; it never touches the network or disk.
type Source struct { /* unexported */ }

func New(opts ...Option) *Source

// Option configures a Source.
type Option func(*config)

// WithRelease registers a release at tag, exposing the given assets. The first
// registered release is the "latest" unless WithLatestTag overrides it.
func WithRelease(tag string, assets ...Asset) Option

// WithLatestTag sets which registered tag GetLatestRelease returns.
func WithLatestTag(tag string) Option

// WithMissingTag makes GetReleaseByTag(tag) return a not-found error
// (wrapping release.ErrNotSupported-style sentinel — see Error Handling).
func WithMissingTag(tag string) Option

// Asset describes one downloadable asset. Body is served verbatim by
// DownloadReleaseAsset. Helpers build the common shapes:
type Asset struct {
    Name string
    Body []byte
}

// TarGzAsset builds a .tar.gz asset whose single entry is the named binary
// with the given contents (mirrors goreleaser's archive layout and the
// OS/arch naming the updater's findReleaseAsset expects).
func TarGzAsset(toolName, binName, binBody string) Asset

// ChecksumsAsset builds a checksums.txt manifest over the given assets. With
// Corrupt set, the recorded hashes intentionally mismatch the asset bodies.
func ChecksumsAsset(corrupt bool, over ...Asset) Asset

// SignatureAsset builds a checksums.txt.sig detached signature over the
// manifest using entity. With BadSignature, it signs a different payload so
// verification fails against the served manifest.
func SignatureAsset(entity *openpgp.Entity, manifest Asset, bad bool) Asset
```

The double's behaviour maps directly onto the BDD scenarios:

| Scenario directive | Construction |
|---|---|
| already-latest no-op | `New(WithRelease("v1.0.0", …), WithLatestTag("v1.0.0"))`, current = `v1.0.0` |
| version not-found | `New(WithMissingTag("v9.9.9"))` |
| checksum mismatch abort | latest `v1.1.0` + `ChecksumsAsset(corrupt=true, …)` |
| signature mismatch abort | latest `v1.1.0` + good checksums + `SignatureAsset(entity, manifest, bad=true)` |
| (Phase 6, Go-only) happy apply | good asset + good checksums (+ good sig) |

### `mocks/` — generated mock

`mocks/pkg/vcs/release/Provider.go` **already exists** (the `.mockery.yml`
`all: true` recursive config already covers `pkg/vcs/release`), so no new mock
generation is required — it is available for expectation-style unit tests where
a configurable behavioural double is overkill. (The release interface is
unchanged by this spec; only a sentinel `var` is added.) Note: `just mocks`
surfaces substantial pre-existing mock drift across unrelated packages; that
cleanup is explicitly **out of scope** here and must not be swept into this
change.

## Generator Impact

`props.Tool` is scaffolded into every generated tool by
`internal/generator/templates/skeleton_root.go`
(`props.Tool{ ReleaseSource: props.ReleaseSource{…} }` via
`buildReleaseSourceDict`). Impact:

1. **Mandatory (compatibility):** the new `ReleaseProvider` field is additive and
   `json:"-"`; generated `props.Tool{…}` composite literals that don't set it
   still compile and behave identically (nil → registry path). The
   `skeleton_root.go` emission needs **no change**, but the generator's
   golden/AST tests (`internal/generator/...`) and a real
   `go run ./cmd/gtb generate project -p tmp && go build ./...` smoke must be
   re-run to confirm the scaffolded tree still builds against the updated
   `pkg/props`.
2. **Adopted (the user's "update generated code" ask):** scaffold a hermetic
   self-update test into generated tools using `releasetest`, mirroring how
   signing scaffolds `internal/trustkeys`. Concretely, emit a `*_release_test.go`
   (or extend the generated `main_test.go`) that constructs a
   `releasetest.New(...)`, sets `Tool.ReleaseProvider`, and asserts an
   already-latest no-op — proving the tool's update wiring works without network.
   **Emission is gated on the generated tool having `UpdateCmd` enabled** (OQ2 →
   option C): the test appears exactly when the tool actually has self-update and
   is absent otherwise, with no new generator flag — the same pattern as
   signing's conditional `internal/trustkeys` scaffold. This gives every
   update-capable downstream tool a working example of the seam.
3. **Docs:** `docs/components/` for the update/release subsystem and any
   generator docs that enumerate scaffolded files must reflect (2) if adopted.

## Error Handling

- The not-found path returns a wrapped sentinel so the update command's existing
  error surface ("release not found"/non-zero exit) is preserved. The double
  returns `errors.Wrapf(release.ErrReleaseNotFound, "tag %q", tag)`; this spec
  introduces `release.ErrReleaseNotFound` if no suitable sentinel exists today
  (the real GitHub/GitLab providers currently return provider-specific
  not-found errors — Open Question 4 covers whether to unify them).
- Corrupt-checksum and bad-signature paths rely on the **existing** verification
  errors in `pkg/setup` (checksum mismatch, signature verification failure); the
  double only supplies the bad bytes. No new error types for these.
- All double methods honour `ctx` cancellation and return wrapped
  `cockroachdb/errors` per `docs/development/error-handling.md`.

## Testing Strategy (TDD)

Per the project's TDD workflow, each phase writes failing tests first, derived
from the contracts above.

### Unit

- **`releasetest` self-tests** (`pkg/vcs/release/releasetest`): the double
  satisfies `release.Provider`, `ChecksumProvider`, `SignatureProvider`
  (compile-time `var _ release.Provider = (*Source)(nil)`); `GetLatestRelease` /
  `GetReleaseByTag` / `DownloadReleaseAsset` return the configured data;
  `WithMissingTag` yields the not-found sentinel; `TarGzAsset` round-trips
  through the updater's extractor; `ChecksumsAsset(corrupt)` and
  `SignatureAsset(bad)` produce genuinely mismatching artefacts. ≥90% coverage.
- **Seam tests** (`pkg/setup`): `NewUpdater` precedence (option > field >
  registry); injected provider skips registry `Lookup` and the
  `requireReleaseToken` gate; `WithReleaseProvider` is parallel-safe (two
  concurrent updaters with different doubles don't interfere).
- **Refactor** `update_e2e_test.go` onto `releasetest` (the clean, full-pipeline
  cases), keeping the white-box `update_checksum_test.go` /
  `update_signature_test.go` fakes in place (single definition; they need
  provider-internal hooks). These existing tests are the regression net — they
  must stay green with identical assertions.
- **Mock** smoke: a trivial expectation-style test using
  `mocks/pkg/vcs/release/Provider` to prove the generated mock is usable.
- **`props`**: `Tool` with a non-nil `ReleaseProvider` round-trips through
  JSON/YAML marshal **without** emitting the field (it is `-`).

### E2E BDD (Godog) — Phase 9 payoff

Per the Godog suitability assessment
(`2026-03-28-godog-bdd-strategy.md`), self-update is a multi-step,
user-visible CLI workflow → BDD adds value. `cmd/e2e/main.go` sets
`Tool.ReleaseProvider` from a `releasetest.Source` when an env gate is present
(e.g. `GTB_E2E_RELEASE_SCENARIO=already-latest|not-found|bad-checksum|bad-signature`),
and pins a deterministic `Version` so semver comparison is stable. New
`features/cli/update.feature` scenarios assert **outcomes that abort before any
binary swap** (safe against the shared E2E binary):

- already-latest → exit 0 + "up to date"/"latest" message;
- `--version <unknown>` → non-zero exit + clear not-found message;
- newer + corrupt checksum → non-zero exit + checksum-failure message, **binary
  unchanged**;
- newer + bad signature (require_signature) → non-zero exit + signature-failure
  message, **binary unchanged**.

The happy "newer → applies" path stays out of BDD (self-replacement); it is
covered Go-side in Phase 6's `update_e2e_test.go`, now on `releasetest`.

A new step is needed to pass per-scenario env into the subprocess
(`cli_steps_test.go` currently uses a fixed env) — small, scoped addition.

### Generator

- Golden/AST tests confirm the scaffolded tree compiles against the new
  `pkg/props`.
- If the scaffolded self-update test (Generator Impact 2) is adopted: a
  template-level test (in `internal/generator/templates`) asserting the emitted
  test references `releasetest` and `Tool.ReleaseProvider`, plus the standard
  `generate project -p tmp && go test ./...` generator-build integration check.

### Verification gates

`just ci` (tidy, generate, test, test-race, lint), `just mocks` clean,
≥90% coverage on new `pkg/` code, and the `@generator`/`@cli` Godog suites green.

## Migration & Compatibility

- **Additive, backwards compatible.** Nil `ReleaseProvider` + no option = today's
  behaviour exactly. No `ReleaseSource` wire-format change.
- Pre-1.0 API note (per CLAUDE.md): adding an exported field to `props.Tool` and a
  new `UpdaterOption` is non-breaking; `just apidiff` will show the additions as
  advisory. No migration note required, but the `docs/migration/` index gains a
  one-line "new injectable release seam" entry for the v1.0 guide.
- The `cmd/e2e/main.go` doc-comment is corrected to describe the now-real
  env-gated stub.

## Future Considerations

- Unifying provider not-found errors behind a single `release.ErrReleaseNotFound`
  sentinel across github/gitlab/gitea/direct (Open Question 4) — useful beyond
  tests but out of scope here unless trivially free.
- A `releasetest` HTTP mode (serve over `httptest.Server`) for providers that
  must exercise real transport — relevant to Phase 11 (WKD/GitLab integration),
  not this spec.
- Extending the seam to the **offline** updater (`UpdateFromFile`) is unnecessary
  (it already takes a local path); noted for completeness.

## Implementation Phases

1. **Seam + double (core).** `WithReleaseProvider`, `props.Tool.ReleaseProvider`,
   `NewUpdater` precedence; `release.ErrReleaseNotFound`;
   `pkg/vcs/release/releasetest`. TDD: seam precedence tests + double self-tests
   first. (The `release.Provider` mock already exists — no mock work.)
2. **Adopt the double in `update_e2e_test.go`.** Refactor the full-pipeline
   `Update()` tests onto `releasetest`; keep the white-box checksum/signature
   fakes (single definition, provider-internal hooks); keep assertions identical.
3. **E2E wiring + Phase 9 scenarios.** Env-gated `Tool.ReleaseProvider` in
   `cmd/e2e`, deterministic version, per-scenario env step, `update.feature`
   abort-before-replace scenarios; correct the `cmd/e2e` doc-comment.
4. **Generator.** Compatibility re-verification; optional scaffolded self-update
   test using `releasetest`; docs.

## Open Questions

All resolved at approval (2026-06-19); recorded here for provenance.

1. ~~Provider vs. ProviderFactory on the seam~~ — **Resolved** (Decision 2):
   ready `Provider`.
2. ~~Private-repo token gate when a provider is injected~~ — **Resolved → A**:
   **skip** the `ReleaseSource.Private → requireReleaseToken` gate when a provider
   is injected (option or field). An injected provider is self-contained and owns
   its own auth; the gate is an implementation detail of the registry path.
   Verified no downstream relies on it as a side effect.
3. ~~Scaffolded downstream self-update test: always vs. opt-in~~ — **Resolved →
   C**: **always emit**, but **gated on the generated tool having `UpdateCmd`
   enabled** (no new generator flag). Absent for update-disabled tools; mirrors
   signing's conditional `internal/trustkeys` scaffold.
4. ~~Unified not-found sentinel vs. local~~ — **Resolved → A**: introduce
   `release.ErrReleaseNotFound` now and require the **double** to use it; aligning
   the real github/gitlab/gitea/direct providers behind the sentinel is a tracked
   **follow-up** (noted under Future Considerations), accepting mild interim
   test/prod skew on the typed error (BDD also asserts exit-code/message, which is
   skew-free).
5. ~~E2E env surface~~ — **Resolved → A**: a single
   `GTB_E2E_RELEASE_SCENARIO=already-latest|not-found|bad-checksum|bad-signature`
   selector, decoded by one switch in `cmd/e2e` to a `releasetest` construction.
