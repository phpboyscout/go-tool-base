---
title: Testing & Mocking
description: Strategies for unit testing commands using mocks and virtual filesystems.
date: 2026-02-16
tags: [how-to, testing, mocking, unit-tests]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Testing & Mocking

One of the primary goals of GTB is to make CLI tools easily testable. By using the `Props` container, you can inject mock behaviors for filesystems, logging, and configuration.

## Building a test `Props` in one call

The fastest way to get a fully-wired `*props.Props` for a test — with a noop logger, in-memory filesystem, noop telemetry collector, inert error handler and a usable empty config — is the public `test.New` helper:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/props/test"

func TestMyCommand(t *testing.T) {
    t.Parallel()

    p := test.New(
        test.WithTool(props.Tool{Name: "mytool", EnvPrefix: "MYTOOL"}),
    )
    // ... run your command logic with p ...
}
```

It is hermetic (no disk, network, keychain, or `os.Exit`) and returns a fresh instance per call, so it is safe under `t.Parallel()`. See the full default table and option list in the [Props component reference](../explanation/components/props.md#testing-with-props). The manual patterns below remain available when you need finer control.

## Mocking the Filesystem

GTB uses `afero` for filesystem operations. In your tests, you can use `afero.NewMemMapFs()` to simulate a filesystem without touching the disk:

```go
func TestMyCommand(t *testing.T) {
    fs := afero.NewMemMapFs()
    _ = afero.WriteFile(fs, "/config.yaml", []byte("key: value"), 0644)

    props := &props.Props{
        FS: fs,
        // ... other props
    }

    // Now run your command logic using these props
}
```

## Mocking Configuration

The `pkg/config` package provides an in-memory container builder for testing:

```go
cfg := config.NewReaderContainer(fs,
    config.WithLogger(logger.ToSlog(logger.NewNoop())),
    config.WithConfigFormat("yaml"),
    config.WithConfigReaders(bytes.NewReader([]byte("key: test-value"))),
)
props.Config = cfg
```

For the config-specific recipes — the generated `Containable`/`Observable` mocks
and testing observer behaviour — see [How to Test Code That Uses Configuration](test-configuration.md).

## Best Practices for Tests

- **Avoid Global State**: Do not rely on environment variables or global `os` calls. Use the abstractions provided in `Props`.
- **Table Driven Tests**: Use Go's table-driven test pattern to verify your command logic against multiple input/config scenarios.
- **Capture Output**: You can provide a custom `io.Writer` to the `Logger` in your tests to verify exactly what is being logged.

## Race Condition Avoidance

All tests should pass `go test -race ./...`. The following rules prevent data races and ensure `t.Parallel()` can be used safely.

### No package-level mocking hooks

**Do not** create package-level `var` for test mocking. This pattern is fundamentally incompatible with `t.Parallel()` because concurrent tests mutate and restore the same global:

```go
// BAD — races when tests run in parallel
var execLookPath = exec.LookPath

func TestFoo(t *testing.T) {
    old := execLookPath
    defer func() { execLookPath = old }()
    execLookPath = func(file string) (string, error) { return "/fake", nil }
    // ...
}
```

Instead, inject dependencies through functional options or struct fields:

```go
// GOOD — each test gets its own instance, no shared mutable state
type Config struct {
    ExecLookPath func(string) (string, error)
}

func TestFoo(t *testing.T) {
    t.Parallel()
    cfg := Config{ExecLookPath: exectest.FakeLookPath("/fake")}
    // ...
}
```

The `internal/exectest` package provides common fakes for `exec.LookPath` and `exec.CommandContext`:

| Helper | Description |
|--------|-------------|
| `exectest.FakeLookPath(path)` | Always returns the given path |
| `exectest.MissingLookPath()` | Always returns "not found" |
| `exectest.EchoCommand(output)` | Returns an `echo` command with the given output |
| `exectest.FailCommand()` | Returns a command that exits non-zero |
| `exectest.NoopCommand()` | Returns a no-op command |
| `exectest.TrackingCommand(&log)` | Records invocations into a string slice |
| `exectest.FakeExecutable(path)` | Fake `os.Executable` returning the given path |

### Registry-aware tests

The `pkg/setup` registries (`globalMiddleware`, `featureMiddleware`, `globalRegistry`) are package-level shared state protected by mutexes. Tests that call `ResetRegistryForTesting()` wipe this state, making them logically incompatible with `t.Parallel()` against other tests in the same package — the mutex prevents data races but does not prevent state interleaving.

**Rule**: tests that call `setup.ResetRegistryForTesting()` or `setup.RegisterMiddleware()` / `setup.RegisterChecks()` must **not** use `t.Parallel()` unless they register to unique feature names and do not reset.

Tests that only _read_ from the registry (e.g. `setup.GetChecks()`) with distinct feature names _can_ use `t.Parallel()` safely — the mutex guarantees memory visibility.

The release-provider registry (`forge.Register`) is the same kind of global mutable state. To test self-update without mutating it, inject a provider through the parallel-safe DI seam (`setup.WithReleaseProvider` / `props.Tool.ReleaseProvider`) and drive it with the in-memory double from `forge/test` — see [Release Provider › `releasetest`](https://forge.go.phpboyscout.uk/how-to/testing/).

### Avoid `cobra.OnFinalize`

`cobra.OnFinalize` mutates a package-level slice inside the cobra library. Constructing multiple root commands in parallel (common in tests) races on this slice. Use `defer` in `Execute()` or middleware instead. See [the race remediation spec](../development/specs/2026-04-15-test-race-remediation.md) for the full rationale.

### `t.Parallel()` + `t.Setenv()` are incompatible

Go's testing framework panics if a test calls both `t.Parallel()` and `t.Setenv()`. Tests that modify environment variables must remain serial. Prefer injecting values through `Props`, `Config`, or functional options instead of environment variables where possible.

## Testing interactive (huh / charm) forms

Code that drives a `charm.land/huh` form calls `form.Run()`, which blocks on a real TTY — a naive unit test hangs. huh's **accessible mode** (auto-enabled by `TERM=dumb`) turns the form into line-based prompts that read `os.Stdin`, so you can drive the *real* form with scripted input; alternatively inject the form (the `WithForm` pattern) or drive it as a Bubble Tea model. The full guide, with copy-paste helpers and a decision table, is in [Testing huh / charm interactive forms](../development/testing/huh-form-testing.md).
