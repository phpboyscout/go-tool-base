---
name: gtb-generator-manual-test
description: Manually test the gtb generate and regenerate commands end to end — scaffold a project, mutate it (add command/flag), run the regenerate loop, and prove the output compiles — against both the working branch and the previous released CLI. Use after any change to the generator templates (internal/generator/) or to a pkg/ library API that scaffolded code consumes, to catch template drift and downstream upgrade breakages before they ship.
---

# GTB generator manual test

The generator is the source of truth for every downstream tool's scaffolding, so
a template that lags a library API change ships broken code to users. `go test`
does not catch this: the generated project has its **own** module, and its
`go.mod` pins the **latest released** go-tool-base — not your branch — so a naive
build tests the old library. This skill is the repeatable procedure for
exercising the generator for real, in two complementary tracks.

Run this whenever you change `internal/generator/` **or** any `pkg/` public API
that scaffolded code calls (constructors, `props`, config adapters, logger,
telemetry, error handling).

## Two tracks — run both

- **Track 1 — Branch consistency.** Generate with the *branch* CLI, build the
  output against the *branch* library. Answers: "do my templates and my library
  changes agree?" Catches a template that still emits a pre-change API.
- **Track 2 — Upgrade path.** Generate with the *previous released* CLI, then
  `regenerate` with the *branch* CLI. Answers: "when a downstream user upgrades
  gtb and regenerates, what changes and what breaks?" Catches API drift a real
  user hits on upgrade.

## Environmental notes (this sandbox)

- **`-buildvcs=false` is mandatory** for every `go build/vet/test` of a generated
  project here. A plain build fails with `error obtaining VCS status: exit
  status 128` — an environmental Go build-stamp issue, not a code problem (same
  root cause as the skipped `internal/agent` test).
- **A `replace` directive is how you test against the branch.** The generated
  `go.mod` pins a released tag; point it at the working tree to build against
  your changes.
- Generate **outside the repo** (a scratch dir), never nested under go-tool-base
  — a nested second module confuses tooling.
- Generator output is built with **dave/jennifer** (`internal/generator/templates/*.go`),
  not text templates. To fix emitted code, edit the jennifer calls — grep the
  `templates/*.go` for the `jen.Qual(...)` that emits the line, not for a literal
  Go snippet.

## Setup

```bash
cd <go-tool-base repo root>
REPO=$PWD
just build                       # or: go build -o bin/gtb ./cmd/gtb
BR=$REPO/bin/gtb                 # the branch (test) CLI
SCRATCH=$(mktemp -d)             # or your session scratchpad
FEATURES=init,update,mcp,docs,doctor,changelog,keychain,ai,config,telemetry
```

Enable **all** features — `ai`, `config`, and `telemetry` are the ones that
exercise the config-adapter and telemetry paradigms; the default feature set
omits them.

## Track 1 — Branch consistency

```bash
G1=$SCRATCH/branchgen
$BR generate project --name branchgen --repo phpboyscout/branchgen \
    --features "$FEATURES" --git-backend gitlab --no-git --overwrite allow -p "$G1"

cd "$G1"
go mod edit -replace gitlab.com/phpboyscout/go-tool-base="$REPO"   # test vs branch
go mod tidy
go build -buildvcs=false ./...    # <-- must be clean
go vet   -buildvcs=false ./...
go test  -buildvcs=false ./...
```

A compile error here means a template emits an API your branch changed. Example
this skill was written from: the slog-first change to
`errorhandling.New(*slog.Logger, …)` left the skeleton passing a bare
`logger.Logger`, so generated `pkg/cmd/root/cmd.go` failed with
`cannot use l … as *slog.Logger`. Fix in
`internal/generator/templates/skeleton_root.go` (mirror GTB's own
`internal/cmd/root/root.go`: wrap with `logger.ToSlog(l)`), rebuild `$BR`,
regenerate, rebuild.

### Clean-run fixed point (do this first, it is the strongest invariant)

A freshly generated, **unchanged** project must survive both regenerate
directions with **zero** diff. If `regenerate manifest` or `regenerate project`
changes anything on a pristine project, the `generate` writer and the
`regenerate` scanner/writer disagree — that is a bug, not normalization.

```bash
cd "$G1"
git init -q && git add -A && git -c user.email=t@t -c user.name=t commit -qm baseline

$BR regenerate manifest -p "$G1"
git diff --quiet && echo "✅ manifest fixed point" || { echo "❌ regenerate manifest changed a clean project"; git --no-pager diff --stat; }

$BR regenerate project  -p "$G1"
git diff --quiet && echo "✅ project fixed point"  || { echo "❌ regenerate project changed a clean project"; git --no-pager diff --stat; }
```

Both must be clean. The failure this guards against: `regenerate project`
rewrites `go.mod` from the embedded skeleton snapshot (which omits the `tool`
block's transitive deps) and, unlike `generate`, historically did **not** run
`go mod tidy` afterwards — so `go.mod` was left with ~200 deps stripped and the
manifest recorded that stripped `go.mod`'s hash. Fixed by running `go mod tidy`
in regenerate's post-processing (see `runPostRegenerationProcessing`); the
regression is locked by `TestRunPostRegenerationProcessing_RunsGoModTidyBeforeLint`.
If you see `go.mod` churn here again, that tidy has regressed.

### Mutation coverage (still Track 1)

Exercise the command-authoring surface, rebuilding after each:

```bash
$BR generate command -n serve --short "Run the server" -f "port:int:Port to listen on" -p "$G1"
$BR generate add-flag -c serve -n verbose -t bool -d "Verbose output" -p "$G1"
go mod tidy && go build -buildvcs=false ./...
grep -A6 'name: serve' .gtb/manifest.yaml     # flag recorded in the manifest
```

`-f`/`--flags` uses `name:type:description`; `add-flag` addresses nested commands
by slash path (`parent/child/leaf`), never the bare leaf or a dotted form.

### Manifest round-trip — does `regenerate manifest` rebuild correctly from source?

`regenerate manifest` rescans the project source and rewrites
`.gtb/manifest.yaml`. Verify it recovers the command tree **and** preserves
project metadata. Run it **with the existing manifest in place** — that is the
supported direction (it updates from source while keeping project-level fields):

```bash
cp .gtb/manifest.yaml /tmp/manifest.before
$BR regenerate manifest -p "$G1"

# 1. custom commands/flags recovered from source:
grep -A8 'name: serve' .gtb/manifest.yaml            # serve + port + verbose present
# 2. feature enablement preserved (all you generated, not just the baseline):
sed -n '/features:/,/release_source/p' .gtb/manifest.yaml | grep -c 'name:'   # == generated count (e.g. 10)
```

Expected: the full command tree (including commands/flags you added) is present
and the features list is unchanged. `regenerate manifest` **normalizes** as it
rewrites — it drops the per-file and per-command `hashes:` blocks and fills
omitted flag defaults (`default: "0"`) — so the file is not byte-identical to
`/tmp/manifest.before`; that normalization is expected, not a failure. A missing
**command or flag** you added, however, is a real scanner bug.

Then confirm the reverse direction consumes it faithfully:

```bash
$BR regenerate project -p "$G1"                       # registration from the (preserved) manifest
go mod tidy && go build -buildvcs=false ./...          # SetFeatures(...) kept -> opt-in commands survive
```

**Known boundary — do not misread as a regression.** A *from-scratch* rebuild
(manifest deleted before the scan) cannot recover feature enablement for
library-provided commands: it infers only the baseline `init/update/mcp/docs`
and drops `doctor/changelog/keychain/ai/config/telemetry`, `docs_layout`, and all
hashes. This is inherent to source-scanning and **identical on the released CLI**
(verify by running the same delete-then-scan on `$REL`), because the enabled
built-in feature set is not fully derivable from generated source. `regenerate
manifest` is meant to update an existing manifest, not reconstruct one from
nothing — so treat the delete-first path as a boundary probe, not a pass/fail.

### Runtime smoke

Compiling is necessary but not sufficient — run the binary:

```bash
go build -buildvcs=false -o "$SCRATCH/branchgen-bin" ./cmd/...
"$SCRATCH/branchgen-bin" --help
printf 'telemetry:\n  enabled: true\n  local_only: true\n' > "$SCRATCH/cfg.yaml"
"$SCRATCH/branchgen-bin" --config "$SCRATCH/cfg.yaml" telemetry status   # builds the collector
```

`telemetry status` should report `enabled (local-only)` and a file backend —
proof the collector (and the decoupled telemetry types) construct at runtime.

## Track 2 — Upgrade path (previous release → branch regenerate)

Get the previous released CLI into an isolated location (do not clobber `$BR`):

```bash
PREV=v0.29.0     # the latest release BEFORE this branch; `$BR version` warns which tag you're ahead of
GOBIN=$SCRATCH/relbin go install gitlab.com/phpboyscout/go-tool-base/cmd/gtb@$PREV
REL=$SCRATCH/relbin/gtb
```

Generate with the **released** CLI, snapshot a git baseline, then run the
**full regenerate loop** with the **branch** CLI:

```bash
G2=$SCRATCH/upgradetest
$REL generate project --name upgradetest --repo phpboyscout/upgradetest \
     --features "$FEATURES" --git-backend gitlab --no-git --overwrite allow -p "$G2"
cd "$G2"
git init -q && git add -A && git -c user.email=t@t -c user.name=t commit -qm baseline

# Full loop: source -> manifest, then manifest -> registration/source
$BR regenerate manifest -p "$G2"     # rebuild .gtb/manifest.yaml by scanning source
$BR regenerate project  -p "$G2"     # rewrite command registration from the manifest

git --no-pager diff --stat           # <-- what the new CLI changes on upgrade
```

`regenerate` runs golangci-lint internally and **still writes files even when it
fails**, so read its output: a `typecheck` error is the breakage signal.

### Interpreting Track 2

- **The diff is the "what changed".** Expect template drift (e.g.
  `errorhandling.New(l, …)` → `errorhandling.New(logger.ToSlog(l), …)`) and
  manifest-format bumps. Review it as the change a downstream maintainer will see
  in their PR after upgrading.
- **`undefined: <symbol>` is the expected "breakage".** The branch CLI emits code
  that needs a branch-version API (e.g. `undefined: logger.ToSlog`) while the
  project still pins the old release. This is **not** a generator bug — it is the
  rule that *regenerating and bumping the go-tool-base dependency must happen in
  lockstep*. Confirm the upgrade is clean by bumping and rebuilding:

  ```bash
  go mod edit -replace gitlab.com/phpboyscout/go-tool-base="$REPO"   # stand in for the real dep bump
  go mod tidy && go build -buildvcs=false ./...                       # must be clean
  ```

- **A build failure that survives the dependency bump is a real bug** — the
  regenerated code is wrong against the new library. Fix the template (Track 1
  loop) and re-run.

## Cleanup

```bash
rm -rf "$SCRATCH"
git checkout -- go.mod go.sum 2>/dev/null || true   # if you ever added a replace inside the repo (you shouldn't)
```

## Pass criteria

- Track 1: generated project builds, vets, and tests clean against the branch;
  `generate command`/`add-flag` mutations rebuild clean; the binary runs and
  `telemetry status` reports the collector.
- Track 2: the regenerate diff is understood and intentional; the only build
  breakage is a lockstep-dependency `undefined:` that clears after bumping
  go-tool-base to the branch.
