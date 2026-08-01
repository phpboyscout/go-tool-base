package steps_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"

	"gitlab.com/phpboyscout/go-tool-base/test/e2e/support"
)

type signalWorldKey struct{}

// signalWorld owns a long-running gtb subprocess so a scenario can deliver a
// real OS signal to it and observe the process exit code and output. It is
// distinct from cliWorld, which runs the binary to completion synchronously.
type signalWorld struct {
	binaryPath string
	configDir  string

	cmd      *exec.Cmd
	stdout   *support.SyncBuffer
	stderr   *support.SyncBuffer
	waitErr  error
	exitCode int

	waitOnce sync.Once
}

func getSignalWorld(ctx context.Context) *signalWorld {
	return ctx.Value(signalWorldKey{}).(*signalWorld)
}

func initSignalSteps(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		tmpDir, err := os.MkdirTemp("", "gtb-e2e-signal-*")
		if err != nil {
			return ctx, fmt.Errorf("failed to create temp config dir: %w", err)
		}

		cfgPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte("log:\n  level: info\n"), 0o600); err != nil {
			return ctx, fmt.Errorf("failed to write temp config: %w", err)
		}

		return context.WithValue(ctx, signalWorldKey{}, &signalWorld{configDir: tmpDir}), nil
	})

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w := getSignalWorld(ctx)

		// Best-effort: kill a process the scenario left running, then clean up.
		if w.cmd != nil && w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
			w.awaitExit()
		}

		if w.configDir != "" {
			_ = os.RemoveAll(w.configDir)
		}

		return ctx, nil
	})

	ctx.Step(`^the gtb binary is running the "([^"]*)" command$`, theGTBBinaryIsRunningCommand)
	ctx.Step(`^I send SIGINT to the running gtb process$`, iSendSIGINTToRunningProcess)
	ctx.Step(`^the gtb process exits with code (\d+)$`, theGTBProcessExitsWithCode)
	ctx.Step(`^the running process stdout contains "([^"]*)"$`, theRunningProcessStdoutContains)
	ctx.Step(`^the running process output contains "([^"]*)" exactly (\d+) time\(s\)$`, theRunningProcessOutputContainsNTimes)
}

const (
	// blockReadyMarker is printed by the `block` fixture once it is parked on
	// cmd.Context().Done(); waiting for it removes a signal-before-ready race.
	blockReadyMarker = "blocking until interrupted"

	// superviseReadyMarker is the equivalent for the `supervise` fixture, which
	// is ready once its controls.Controller is running.
	superviseReadyMarker = "supervisor running"

	signalReadyTimeout = 10 * time.Second
	signalExitTimeout  = 10 * time.Second
	signalPollInterval = 10 * time.Millisecond
)

// readyMarkers maps each signal fixture to the line it prints when it is safe to
// interrupt. Keyed by command so a new fixture registers its marker here rather
// than growing a parallel "is running" step.
var readyMarkers = map[string]string{
	"block":     blockReadyMarker,
	"supervise": superviseReadyMarker,
}

func theGTBBinaryIsRunningCommand(ctx context.Context, command string) (context.Context, error) {
	w := getSignalWorld(ctx)

	path, err := support.BinaryPath()
	if err != nil {
		return ctx, fmt.Errorf("failed to build gtb binary: %w", err)
	}

	w.binaryPath = path
	w.stdout = &support.SyncBuffer{}
	w.stderr = &support.SyncBuffer{}

	// Do NOT tie the subprocess to the scenario context: cancelling that
	// context would deliver SIGKILL via CommandContext and defeat the test.
	// The After hook guarantees cleanup instead.
	w.cmd = exec.Command(w.binaryPath, command, //nolint:gosec // test-only: command is from a Gherkin step
		"--ci", "--config", filepath.Join(w.configDir, "config.yaml"))
	w.cmd.Env = append(os.Environ(), "HOME="+w.configDir)
	w.cmd.Stdout = w.stdout
	w.cmd.Stderr = w.stderr

	if err := w.cmd.Start(); err != nil {
		return ctx, fmt.Errorf("failed to start gtb %q: %w", command, err)
	}

	// Wait until the fixture confirms it is ready, so the signal can never
	// arrive before the command has installed its context handler.
	marker, ok := readyMarkers[command]
	if !ok {
		return ctx, fmt.Errorf("no ready marker registered for fixture %q", command)
	}

	if err := w.waitForStdout(marker, signalReadyTimeout); err != nil {
		return ctx, err
	}

	return ctx, nil
}

func iSendSIGINTToRunningProcess(ctx context.Context) error {
	w := getSignalWorld(ctx)

	if w.cmd == nil || w.cmd.Process == nil {
		return fmt.Errorf("no running gtb process to signal")
	}

	if err := w.cmd.Process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("failed to send SIGINT: %w", err)
	}

	return nil
}

func theGTBProcessExitsWithCode(ctx context.Context, expected int) error {
	w := getSignalWorld(ctx)

	select {
	case <-w.waitDone():
	case <-time.After(signalExitTimeout):
		return fmt.Errorf("gtb process did not exit within %s after SIGINT\nstdout:\n%s\nstderr:\n%s",
			signalExitTimeout, w.stdout.String(), w.stderr.String())
	}

	if w.exitCode != expected {
		return fmt.Errorf("expected exit code %d, got %d\nstdout:\n%s\nstderr:\n%s",
			expected, w.exitCode, w.stdout.String(), w.stderr.String())
	}

	return nil
}

func theRunningProcessStdoutContains(ctx context.Context, substr string) error {
	w := getSignalWorld(ctx)

	if !strings.Contains(w.stdout.String(), substr) {
		return fmt.Errorf("process stdout does not contain %q\nstdout:\n%s\nstderr:\n%s",
			substr, w.stdout.String(), w.stderr.String())
	}

	return nil
}

// theRunningProcessOutputContainsNTimes asserts an exact occurrence count across
// the process's combined output. Counting rather than merely matching is the
// point: it is what distinguishes one signal handler from two, which a
// "contains" assertion would pass either way.
func theRunningProcessOutputContainsNTimes(ctx context.Context, substr string, want int) error {
	w := getSignalWorld(ctx)

	combined := w.stdout.String() + w.stderr.String()

	if got := strings.Count(combined, substr); got != want {
		return fmt.Errorf("expected %q %d time(s) in process output, found %d\nstdout:\n%s\nstderr:\n%s",
			substr, want, got, w.stdout.String(), w.stderr.String())
	}

	return nil
}

// waitForStdout polls the captured stdout until it contains marker or the
// timeout elapses.
func (w *signalWorld) waitForStdout(marker string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if strings.Contains(w.stdout.String(), marker) {
			return nil
		}

		time.Sleep(signalPollInterval)
	}

	return fmt.Errorf("timed out waiting for %q on stdout\nstdout:\n%s\nstderr:\n%s",
		marker, w.stdout.String(), w.stderr.String())
}

// waitDone returns a channel closed once the process has exited and its exit
// code has been recorded.
func (w *signalWorld) waitDone() <-chan struct{} {
	done := make(chan struct{})

	go func() {
		w.awaitExit()
		close(done)
	}()

	return done
}

// awaitExit waits for the process exactly once and records its exit code.
func (w *signalWorld) awaitExit() {
	w.waitOnce.Do(func() {
		w.waitErr = w.cmd.Wait()
		w.exitCode = w.cmd.ProcessState.ExitCode()
	})
}
