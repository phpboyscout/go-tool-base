package steps_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cucumber/godog"

	"gitlab.com/phpboyscout/go-tool-base/cli/test/e2e/support"
)

type signalWorldKey struct{}

// signalWorld owns a long-running gtb subprocess so a scenario can deliver a
// real OS signal to it and observe the process exit code and output. It is
// distinct from cliWorld, which runs the binary to completion synchronously.
type signalWorld struct {
	binaryPath string
	configDir  string

	cmd     *exec.Cmd
	stdout  *support.SyncBuffer
	stderr  *support.SyncBuffer
	waitErr error
	status  syscall.WaitStatus

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
	ctx.Step(`^the gtb binary is running the "([^"]*)" command with SIGINT ignored$`, theGTBBinaryIsRunningCommandWithSIGINTIgnored)
	ctx.Step(`^I send (SIGINT|SIGTERM|SIGHUP) to the running gtb process$`, iSendSignalToRunningProcess)
	ctx.Step(`^the gtb process is terminated by (SIGINT|SIGTERM|SIGHUP)$`, theGTBProcessIsTerminatedBy)
	ctx.Step(`^the gtb process is still running$`, theGTBProcessIsStillRunning)
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

	// stillRunningWindow is how long an ignored signal is given to (wrongly)
	// end the process before the scenario concludes it was ignored.
	stillRunningWindow = 500 * time.Millisecond
)

var namedSignals = map[string]syscall.Signal{
	"SIGINT":  syscall.SIGINT,
	"SIGTERM": syscall.SIGTERM,
	"SIGHUP":  syscall.SIGHUP,
}

// readyMarkers maps each signal fixture to the line it prints when it is safe to
// interrupt. Keyed by command so a new fixture registers its marker here rather
// than growing a parallel "is running" step.
var readyMarkers = map[string]string{
	"block":     blockReadyMarker,
	"supervise": superviseReadyMarker,
}

func theGTBBinaryIsRunningCommand(ctx context.Context, command string) (context.Context, error) {
	return startSignalFixture(ctx, command, false)
}

// theGTBBinaryIsRunningCommandWithSIGINTIgnored starts the fixture the way a
// script starts a background job: exec.Cmd cannot set an ignored disposition,
// so sh sets it and execs the binary, which inherits it (spec 0207 D2).
func theGTBBinaryIsRunningCommandWithSIGINTIgnored(ctx context.Context, command string) (context.Context, error) {
	return startSignalFixture(ctx, command, true)
}

func startSignalFixture(ctx context.Context, command string, ignoreSIGINT bool) (context.Context, error) {
	w := getSignalWorld(ctx)

	path, err := support.BinaryPath()
	if err != nil {
		return ctx, fmt.Errorf("failed to build gtb binary: %w", err)
	}

	w.binaryPath = path
	w.stdout = &support.SyncBuffer{}
	w.stderr = &support.SyncBuffer{}

	args := []string{command, "--ci", "--config", filepath.Join(w.configDir, "config.yaml")}

	// Do NOT tie the subprocess to the scenario context: cancelling that
	// context would deliver SIGKILL via CommandContext and defeat the test.
	// The After hook guarantees cleanup instead.
	if ignoreSIGINT {
		w.cmd = exec.Command("sh", append([]string{"-c", `trap "" INT; exec "$0" "$@"`, w.binaryPath}, args...)...) //nolint:gosec // test-only: command is from a Gherkin step
	} else {
		w.cmd = exec.Command(w.binaryPath, args...) //nolint:gosec // test-only: command is from a Gherkin step
	}

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

func iSendSignalToRunningProcess(ctx context.Context, name string) error {
	w := getSignalWorld(ctx)

	if w.cmd == nil || w.cmd.Process == nil {
		return fmt.Errorf("no running gtb process to signal")
	}

	// A runner started with SIGINT ignored hands that ignore to every fixture,
	// so a SIGINT scenario would wait forever (spec 0207 D2).
	if name == "SIGINT" && signal.Ignored(syscall.SIGINT) {
		return fmt.Errorf("this test runner was started with SIGINT ignored, so the fixture ignores it too: %w", godog.ErrSkip)
	}

	if err := w.cmd.Process.Signal(namedSignals[name]); err != nil {
		return fmt.Errorf("failed to send %s: %w", name, err)
	}

	return nil
}

// theGTBProcessIsTerminatedBy reads the wait status: a run a signal ended dies
// by it after its drain, which an exit code cannot express (spec 0207 D1).
func theGTBProcessIsTerminatedBy(ctx context.Context, name string) error {
	w := getSignalWorld(ctx)

	select {
	case <-w.waitDone():
	case <-time.After(signalExitTimeout):
		return fmt.Errorf("gtb process did not end within %s\nstdout:\n%s\nstderr:\n%s",
			signalExitTimeout, w.stdout.String(), w.stderr.String())
	}

	if !w.status.Signaled() || w.status.Signal() != namedSignals[name] {
		return fmt.Errorf("expected death by %s, got signalled=%t signal=%v exit status=%d\nstdout:\n%s\nstderr:\n%s",
			name, w.status.Signaled(), w.status.Signal(), w.status.ExitStatus(), w.stdout.String(), w.stderr.String())
	}

	return nil
}

func theGTBProcessIsStillRunning(ctx context.Context) error {
	w := getSignalWorld(ctx)

	select {
	case <-w.waitDone():
		return fmt.Errorf("gtb process ended, expected it to keep running\nstdout:\n%s\nstderr:\n%s",
			w.stdout.String(), w.stderr.String())
	case <-time.After(stillRunningWindow):
		return nil
	}
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

// awaitExit waits for the process exactly once and records how it ended.
func (w *signalWorld) awaitExit() {
	w.waitOnce.Do(func() {
		w.waitErr = w.cmd.Wait()
		w.status, _ = w.cmd.ProcessState.Sys().(syscall.WaitStatus)
	})
}
