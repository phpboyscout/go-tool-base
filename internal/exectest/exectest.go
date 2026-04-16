package exectest

import (
	"context"
	"fmt"
	"os/exec"
)

// FakeLookPath returns a lookup function that always succeeds with the given path.
func FakeLookPath(path string) func(string) (string, error) {
	return func(_ string) (string, error) { return path, nil }
}

// MissingLookPath returns a lookup function that always fails with "not found".
func MissingLookPath() func(string) (string, error) {
	return func(file string) (string, error) {
		return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
	}
}

// NoopCommand returns a command factory that produces a no-op process.
func NoopCommand() func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "true")
	}
}

// EchoCommand returns a command factory that echoes the given output.
func EchoCommand(output string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", output)
	}
}

// FailCommand returns a command factory that always exits non-zero.
func FailCommand() func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}
}

// FakeExecutable returns an osExecutable function that always returns the given path.
func FakeExecutable(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

// TrackingCommand returns a command factory that records each invocation
// (name + args) into the provided slice and returns a no-op process.
func TrackingCommand(log *[]string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*log = append(*log, fmt.Sprintf("%s %v", name, args))

		return exec.CommandContext(ctx, "true")
	}
}
