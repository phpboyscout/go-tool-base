---
title: "Extract pkg/workspace and pkg/changelog into standalone leaf modules"
description: "Extract two zero-coupling leaf packages out of go-tool-base into gitlab.com/phpboyscout/go/workspace and gitlab.com/phpboyscout/go/changelog. Both have no go-tool-base imports, no config adapter, no logger seam, and only pure in-package tests, so both are clean repoints (delete the package, repoint callers, no facade). workspace is a ~113-LOC afero-seam marker-file project-root detector; changelog is a ~800-LOC Conventional-Commits changelog generator/parser over go-git. pkg/forms was considered for the same batch but is excluded: it is internal generator-wizard tooling whose core justification huh v2 has since absorbed, and is handled by a separate delete-and-rewrite refactor rather than an extraction."
date: 2026-07-21
status: IN PROGRESS
tags:
  - specification
  - workspace
  - changelog
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/workspace` and `pkg/changelog` into standalone leaf modules

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-21

Status
:   IN PROGRESS (2026-07-21). Approved to start with the proposed defaults for Q1–Q4
    (keep `DefaultMarkers` slice, keep `os.Getwd()`, sequence workspace→changelog,
    minimal workspace microsite).
    **workspace: DONE** — `go/workspace` v0.1.0 published (repo id 84666754), microsite
    live, landing card added; GTB cut-over raised (pure repoint, one consumer). **changelog:
    next.**

Related
:   [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5 procedure, §11 pure-repoint findings),
    [controls extraction](../reports/2026-07-07-package-extraction-report.md) (the pure-repoint precedent this most resembles),
    [config extraction](2026-07-18-config-module-extraction.md) (IMPLEMENTED — clean-break repoint),
    [extraction report](../reports/2026-07-07-package-extraction-report.md) (readiness checklist — both listed "Ready now"),
    [forms delete-and-rewrite](2026-07-21-forms-native-huh-migration.md) (the sibling decision that removed the third package from this batch)

---

## 1. Summary

Two independent `pkg/` packages become standalone modules under the
`gitlab.com/phpboyscout/go/` subgroup:

| Package | Target module | Prod LOC | External deps | GTB coupling |
|---|---|---:|---|---|
| `pkg/workspace` | `go/workspace` | ~113 | `spf13/afero`, `cockroachdb/errors` | **zero** |
| `pkg/changelog` | `go/changelog` | ~800 | `go-git/v5`, `leodido/go-conventionalcommits`, `golang.org/x/mod/semver`, `cockroachdb/errors` | **zero** |

Both are the clean **pure-repoint** case the playbook (§11.1) describes: no
go-tool-base imports in production code, no `config_adapter.go`/`*FromProps`, no
config-key constants, no logger seam, no exported interfaces needing mocks, and no
package-level mutable state, `init()`, env reads, or goroutines. GTB owns no adapter
on top of either package, so the cut-over deletes the in-tree package and repoints
callers to the module import path with **no aliases and no facade**.

They are batched into one spec because the mechanics are identical; each still cuts
its own module, release, docs microsite, and GTB cut-over. They version and release
independently (no cross-module compatibility matrix — neither has sub-modules).

`pkg/forms` was originally part of this batch. Investigation (see the
[forms migration spec](2026-07-21-forms-native-huh-migration.md)) found it is used
only by the internal scaffolding generator — not reusable framework surface — and
that huh v2's native cross-group back-navigation plus reactive field funcs
(`TitleFunc`/`DescriptionFunc`/`WithHideFunc`) have absorbed the gap it was built to
fill. It is therefore **deleted and its consumers rewritten on native huh v2**, not
extracted. That work is a separate refactor spec.

## 2. Motivation

Both packages are on the extraction report's "Ready now" list and are the last
zero-coupling leaves other than the deliberately-deferred items. They are genuinely
reusable outside GTB:

- **workspace** answers "what is the project root?" by walking up from a start
  directory looking for marker files (`.git`, `go.mod`, a manifest). Any CLI wants
  this; the afero seam makes it testable without touching the real filesystem.
- **changelog** turns a repo's Conventional-Commits history (or a release-notes
  archive) into a structured, categorised changelog. Useful to any tool that
  reports what changed between two versions.

Extracting them shrinks GTB's surface, gives each its own release cadence and docs,
and continues the programme with two of its lowest-risk moves.

## 3. Shared extraction mechanics

Applied to both modules, per playbook §3–§5:

- **Repo & framework.** `phpboyscout/go/<name>` (public), bootstrapped from the
  standard cicd component set at the version GTB currently tracks — **v0.24.1** —
  with `go-lint`, `go-test` (`enable_e2e: false`; neither has a `test/e2e/` dir),
  `go-security`, `releaser-pleaser`, `zensical-pages`, `renovate-self`. No
  `goreleaser` (libraries, no binaries). Go **1.26.5**.
- **Guards.** `.golangci.yaml` (v2, local-prefix the module path, GTB linter set);
  `depfootprint_test.go` asserting `go list -deps ./...` excludes `go-tool-base`,
  viper, pflag, charmbracelet/charm.land, OpenTelemetry, and any cloud SDK; ≥90%
  coverage on the module's own packages.
- **No mocks.** Neither package exports an interface its own tests need, so per
  §11.4 ship **no** `.mockery.yml` / `mocks/` — keeps `go.sum` and the depfootprint
  lean; downstreams generate their own mocks if ever needed.
- **Conventions carried.** `cockroachdb/errors` for all errors; `*slog.Logger`
  would be the only logging seam but neither package logs, so none is added; typed
  values owned by the module; `LICENSE`, `CHANGELOG.md` (releaser-pleaser-owned),
  `README.md` with the shared toolkit header/badge.
- **Dep pin alignment.** After `go mod tidy`, diff every shared transitive against
  GTB's `go.mod` and bump to match (per the §10.6/§11.5 lesson): notably
  `cockroachdb/errors v1.14.0`, and for changelog `go-git/v5 v5.19.1`,
  `go-billy/v5 v5.9.0`, `golang.org/x/mod`, plus the `x/crypto v0.54.0` / `x/sys` /
  `x/net` / `x/text` family that go-git drags in. Expect the first pipeline's
  `osv-scanner`/`trivy` to flag any lagging transitive that local `govulncheck`
  passes; fold the bump into the bootstrap MR.
- **Docs & Pages.** Full Diátaxis microsite per module (`<name>.go.phpboyscout.uk`),
  Reference → pkg.go.dev, custom domain + Let's Encrypt (Pages visibility public or
  the cert never issues, §11.9); require N consecutive 200s before declaring the
  cert live (the config-module lesson).
- **Release order.** Cut `v0.1.0` via the Release-MR flow (human-gated). Confirm the
  tag via the GitLab API **before** probing `proxy.golang.org` (the negative-cache
  trap).
- **GTB cut-over (one MR per module).** Add the dependency; delete the in-tree
  package; repoint callers with no aliases; run the full suite + `just ci`; add a
  `docs/reference/migration/` note; stub the GTB component page to the microsite and
  sweep `docs/` for stale references (§11.6).

## 4. `pkg/workspace` → `go/workspace`

### 4.1 Surface (unchanged by the move)

```go
const DefaultMaxDepth = 100
var  ErrNotFound error
var  DefaultMarkers = []string{".gtb/manifest.yaml", "go.mod", ".git"}

type Workspace struct { Root, Marker string }
type Option     func(*detectConfig)
func WithMaxDepth(depth int) Option

func Detect(fs afero.Fs, startDir string, markers []string, opts ...Option) (*Workspace, error)
func DetectFromCWD(fs afero.Fs, markers []string, opts ...Option) (*Workspace, error)
```

- Deps: `spf13/afero` (first-class `afero.Fs` parameter — the seam is already
  idiomatic; the package never constructs an OS FS itself) and `cockroachdb/errors`.
- Tests: `workspace_test.go` (in-package), `cwd_test.go` + `example_test.go`
  (black-box) — all **pure**; the only GTB import is the package itself, which
  rewrites to the module path.

### 4.2 Consumers to repoint (one)

- `internal/cmd/resolve.go` — `ResolveProjectPath` calls
  `workspace.DetectFromCWD(p.FS, []string{".gtb/manifest.yaml"})` and reads
  `ws.Root`. Repoint the import; no behaviour change.

### 4.3 Decisions specific to workspace

1. **`DefaultMarkers` is an exported mutable slice.** Never reassigned or mutated
   in-tree, but a caller could. Proposed: keep it exported (the ergonomics are the
   point) and document it as read-only; optionally return a defensive copy via a
   `func DefaultMarkers() []string` accessor. See open question Q1.
2. **`DetectFromCWD` reads `os.Getwd()`.** This is ambient process state, not env
   detection, and is the explicit "from the current directory" convenience — the
   pure `Detect(fs, startDir, …)` core is already fully injected. Unlike `go/repo`'s
   `os.Getenv` removal (which broke the injectable-settings contract), `os.Getwd()`
   is the defining behaviour of this function. Proposed: keep it, and scope the
   `depfootprint`/AST guard (if any) to `os.Getenv`/`LookupEnv`, not `Getwd`. See Q2.

## 5. `pkg/changelog` → `go/changelog`

### 5.1 Surface (unchanged by the move)

```go
type Category int
const ( CategoryBreaking Category = iota; CategoryFeature; CategoryFix; CategoryPerformance; CategoryOther )

type Entry     struct { Category Category; Scope, Description, Raw string }
type Release   struct { Version string; Entries []Entry }
type Changelog struct { FromVersion, ToVersion string; Releases []Release }

func (c *Changelog) HasBreakingChanges() bool
func (c *Changelog) BreakingChanges() []Entry
func (c *Changelog) EntriesByCategory(cat Category) []Entry

type GenerateOption func(*generateConfig)
func WithSinceTag(tag string) GenerateOption
func WithMaxReleases(n int) GenerateOption
func WithIncludeAll() GenerateOption

func GenerateFromRepo(repoPath string, opts ...GenerateOption) (string, error)
func Parse(rawNotes string) *Changelog
func ParseFromArchive(r io.Reader) (*Changelog, error)
func FormatSummary(cl *Changelog) string
```

- Deps: `go-git/v5` (+ `plumbing`, `plumbing/object`, `plumbing/storer`) via the
  on-disk `git.PlainOpenWithOptions(…, {DetectDotGit:true})` path — it does **not**
  pull the aferobilly/billy in-memory bridge; `leodido/go-conventionalcommits`
  (+ `/parser`) for commit parsing; `golang.org/x/mod/semver` for tag ordering;
  `cockroachdb/errors`.
- Regexes: the four in `parse.go` are `regexp.MustCompile` on build-time literals —
  no runtime/user patterns, so **no `regexutil.CompileBounded` routing** is needed.
- Tests: all four `*_test.go` are in-package and **pure**. `generate_test.go`
  creates **real on-disk git repos** (`t.TempDir()` + `git.PlainInit` + commits +
  tags), including an empty-repo case. `parse_test.go` reads the relative fixture
  `testdata/multi_release.md` (moves with the package). No memfs, no cross-coupling.

### 5.2 Consumers to repoint (four)

1. `cmd/changelog/generate.go` — the standalone `cmd/changelog` dev-tool binary;
   uses the `Generate*` options + `GenerateFromRepo`. **Stays in GTB** (it's GTB
   tooling); just repoint the import.
2. `pkg/cmd/changelog/changelog.go` — the embedded `changelog` CLI command
   (`NewCmdChangelog`, registered in `pkg/cmd/root/root.go`); uses `Parse`,
   `Changelog`, `Release`.
3. `pkg/cmd/changelog/changelog_test.go` — repoint alongside (2).
4. `pkg/setup/update.go` — `SelfUpdater.GetStructuredReleaseNotes` uses
   `ParseFromArchive`, `Parse`, `Changelog`.

None owns a config adapter or config-key constant over changelog, so all four are
plain import-path repoints.

### 5.3 Decisions specific to changelog

1. **`cmd/changelog` binary stays in GTB.** It is a repo-local generation tool, not
   framework surface; only its import path changes. Confirmed direction, not an open
   question.
2. **No docs loss to audit beyond the component page.** `docs/reference/cli/changelog.md`
   documents the *embedded CLI command* (a GTB concern — stays); the microsite covers
   the library API. The component page `docs/explanation/components/changelog.md`
   stubs to the microsite. Sweep `docs/` for the old import path per §11.6.

## 6. Non-goals

- No API changes to either package beyond the possible `DefaultMarkers` accessor
  (Q1). This is a relocation, not a redesign.
- No change to the embedded `changelog` CLI command's behaviour or flags.
- `pkg/forms` is out of scope here — see its own spec.
- Downstream adopters (afmpeg, other tools) repoint at their own pace once GTB
  ships; not touched by this wave.

## 7. Open questions

- **Q1 — `DefaultMarkers` exposure.** Keep the exported mutable slice (documented
  read-only), or switch to a `DefaultMarkers()` accessor returning a fresh copy?
  Recommendation: keep the slice for v0.1.0 (zero in-tree mutation; the accessor is
  a breaking change we can make later, cheaply, pre-1.0). *Proposed: keep.*
- **Q2 — `os.Getwd()` in `DetectFromCWD`.** Keep as-is (recommended — it is the
  function's purpose), or drop `DetectFromCWD` from the module and leave the CWD
  read to the caller composing `Detect`? Recommendation: keep. *Proposed: keep.*
- **Q3 — batch vs sequence.** Build/publish/cut-over the two modules together in one
  work session with two cut-over MRs, or fully finish workspace before starting
  changelog? Recommendation: do workspace end-to-end first (smallest, single
  consumer — validates the recipe), then changelog. *Proposed: sequence,
  workspace first.*
- **Q4 — microsite depth for a ~113-LOC package.** workspace is tiny; is a full
  Diátaxis microsite warranted or is a getting-started + one how-to + one
  explanation (afero seam / marker-walk semantics) sufficient? Recommendation: the
  smaller three-to-four-page site (matches aferobilly's scale). *Proposed: minimal
  microsite.*

## 8. Acceptance criteria

Per module, mirroring the playbook's "not extracted until step 7":

1. Module builds, vets, `go test -race` passes, lint 0, `govulncheck` clean,
   `depfootprint` green, coverage ≥90%.
2. `v0.1.0` published via the Release-MR flow; tag resolves via the proxy; a
   throwaway consumer imports and builds it.
3. Microsite live at `<name>.go.phpboyscout.uk` (HTTPS, N-consecutive-200s); toolkit
   landing card added.
4. GTB cut-over MR merged: in-tree package deleted, all callers repointed, `just ci`
   green, migration note added, component page stubbed, `docs/` swept.
5. Extraction report checklist ticked for the package.
