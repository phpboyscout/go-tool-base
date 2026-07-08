---
name: race-safe-test-injection
description: Never mock via a package-level mutable var — it races under t.Parallel(). Inject dependencies through functional options, struct fields, or config, and run the race detector. Use whenever you need to fake exec/time/network/filesystem in Go tests.
---

# Race-safe test injection

Use this whenever a Go test needs to fake an external dependency — `exec`,
`time.Now`, the network, the filesystem, an env var. There is a tempting wrong
way and a correct way.

## The anti-pattern: package-level mocking vars

```go
// DON'T. A package-level mutable function var.
var execCommand = exec.CommandContext

func doWork() { execCommand(ctx, "git", "status") }
```

```go
// ...swapped in a test:
func TestDoWork(t *testing.T) {
    execCommand = fakeCommand   // <-- mutates package state
    defer func() { execCommand = exec.CommandContext }()
    // ...
}
```

This **races under `t.Parallel()`**: two parallel tests in the package read and
write the same global at the same time, and the race detector will (rightly)
fail the build. Even without `-race` it's fragile — test order leaks through the
shared var. The rule is blunt: **no package-level mutable vars as mock seams.**

## The fix: inject the seam

Pass the dependency in. Three equivalent shapes, pick what fits:

**Functional option** (good for constructors with optional deps):

```go
type runner struct{ exec execFn }
type Option func(*runner)
func WithExec(fn execFn) Option { return func(r *runner) { r.exec = fn } }

func New(opts ...Option) *runner {
    r := &runner{exec: exec.CommandContext} // real default
    for _, o := range opts { o(r) }
    return r
}
```

**Struct field** (good when the dep is core, not optional): make it a field set
at construction.

**Config field** (good when the dep is policy the caller already configures):
thread it through the config object the component already takes.

Now a test constructs its own instance with a fake and touches no global — so
`t.Parallel()` is safe and tests can't leak into each other.

```go
func TestDoWork(t *testing.T) {
    t.Parallel()
    r := New(WithExec(fakeCommand)) // local, isolated
    // ...
}
```

## A shared fakes package beats ad-hoc fakes

Centralise the common seams so every test uses the same, reviewed fakes. In
go-tool-base that's `internal/exectest`, which hands back ready-made injectables:

- `FakeLookPath(path)` / `MissingLookPath()` — `exec.LookPath` fakes (found /
  `ErrNotFound`).
- `NoopCommand()`, `EchoCommand(out)`, `FailCommand()` (non-zero exit),
  `TrackingCommand(&log)` (records `"name [args]"` for assertions) — `exec`
  command-factory fakes.

Build the equivalent for your project once; don't re-hand-roll a fake `exec.Cmd`
in every test file.

## Clocks are the same problem

`time.Now` as a package var is the same trap. Inject a `now func() time.Time`
(default `time.Now`) on the struct/option, and tests pass a controllable clock —
deterministic timing with no sleeps and no global mutation.

## Always run the detector

`go test -race ./...` is mandatory, not optional. For an intermittent race,
reproduce it reliably with `go test -race -count=5 ./path/to/pkg`. A green
`-race` run is the proof that your injection is actually race-free.

> A `PostToolUse` hook in this plugin flags the package-level seam automatically
> when you write one into non-test Go code — but understanding *why* is what
> keeps it out in the first place.
