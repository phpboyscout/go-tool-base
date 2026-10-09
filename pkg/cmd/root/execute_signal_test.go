package root

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
)

const executeTestTimeout = 5 * time.Second

// exitSpy records calls to the ErrorHandler's exit function instead of
// terminating the test process.
type exitSpy struct {
	codes []int
}

func (s *exitSpy) exit(code int) { s.codes = append(s.codes, code) }

type terminateCall struct {
	sig  os.Signal
	code int
}

// terminateSpy records the signal-ending terminate step instead of re-raising
// the signal at the test binary.
type terminateSpy struct {
	mu    sync.Mutex
	calls []terminateCall
}

func (s *terminateSpy) terminate(sig os.Signal, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, terminateCall{sig: sig, code: code})
}

func (s *terminateSpy) recorded() []terminateCall {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.calls)
}

// newSignalTestProps builds a minimal Props and the spy that records the exit
// codes execute asks for.
//
// The spy used to be handed to the ErrorHandler, which did the exiting.
// errorhandling v0.2.0 reports and RETURNS a code instead, so the seam moved to
// execute itself — wire the spy through executeOptions.exitProcess.
func newSignalTestProps() (*p.Props, *exitSpy) {
	spy := &exitSpy{}
	props := &p.Props{
		Logger:       logger.NewNoop(),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
	}

	return props, spy
}

// runExecute runs execute in a goroutine and fails the test if it does not
// return within the test timeout — proving prompt unwinding on cancellation.
func runExecute(t *testing.T, rootCmd *setup.Command, props *p.Props, opts executeOptions) {
	t.Helper()

	done := make(chan struct{})

	go func() {
		defer close(done)
		execute(rootCmd, props, opts)
	}()

	select {
	case <-done:
	case <-time.After(executeTestTimeout):
		t.Fatal("execute did not return promptly after signal-driven cancellation")
	}
}

// newBlockingCommand returns a root command whose RunE blocks until
// cmd.Context() is cancelled, then returns the context error.
func newBlockingCommand(started chan<- struct{}) *setup.Command {
	cmd := &cobra.Command{
		Use: "stub",
		RunE: func(c *cobra.Command, _ []string) error {
			close(started)
			<-c.Context().Done()

			return c.Context().Err()
		},
	}
	cmd.SetArgs([]string{})

	return setup.Wrap("", cmd)
}

// TestExecute_SignalCancelsContextAndEndsBySignal: the first signal cancels
// cmd.Context() (a command blocking on Done() unwinds promptly), and the run
// ends through terminate with that signal and 128+signum, never through the
// ordinary exit (spec 0207 D1, D3).
func TestExecute_SignalCancelsContextAndEndsBySignal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		signal   os.Signal
		wantCode int
	}{
		{name: "SIGINT", signal: syscall.SIGINT, wantCode: 130},
		{name: "SIGTERM", signal: syscall.SIGTERM, wantCode: 143},
		{name: "SIGHUP", signal: syscall.SIGHUP, wantCode: 129},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			props, spy := newSignalTestProps()
			started := make(chan struct{})
			rootCmd := newBlockingCommand(started)
			sigCh := make(chan os.Signal, 2)

			go func() {
				<-started
				sigCh <- tt.signal
			}()

			term := &terminateSpy{}

			runExecute(t, rootCmd, props, executeOptions{signals: sigCh, exitProcess: spy.exit, terminate: term.terminate})

			assert.Equal(t, []terminateCall{{sig: tt.signal, code: tt.wantCode}}, term.recorded())
			assert.Empty(t, spy.codes, "a signal-ended run does not take the ordinary exit")
		})
	}
}

// TestExecute_InterruptNoticeIsDebugNotError proves the review decision: an
// interrupted run still exits 128+signum, but the "interrupted by signal"
// notice is logged at debug (visible under --debug), never at error — an
// interrupt is a deliberate user choice, not a failure.
func TestExecute_InterruptNoticeIsDebugNotError(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	spy := &exitSpy{}
	props := &p.Props{
		Logger:       buf,
		ErrorHandler: errorhandling.New(logger.ToSlog(buf), nil),
	}

	started := make(chan struct{})
	rootCmd := newBlockingCommand(started)
	sigCh := make(chan os.Signal, 2)

	go func() {
		<-started
		sigCh <- syscall.SIGINT
	}()

	term := &terminateSpy{}

	runExecute(t, rootCmd, props, executeOptions{signals: sigCh, exitProcess: spy.exit, terminate: term.terminate})

	assert.Equal(t, []terminateCall{{sig: syscall.SIGINT, code: 130}}, term.recorded())

	var noticeAtDebug bool

	for _, e := range buf.Entries() {
		if strings.Contains(e.Message, "interrupted by signal") {
			assert.Equal(t, logger.DebugLevel, e.Level,
				"the interrupt notice must be logged at debug, not error")

			noticeAtDebug = true
		}

		assert.NotEqualf(t, logger.ErrorLevel, e.Level,
			"an interrupt must not log at error (saw %q)", e.Message)
	}

	assert.True(t, noticeAtDebug, "the interrupt notice must still be emitted at debug")
}

// TestExecute_SecondSignalForcesExit: a second signal ends the run at once,
// through terminate with the second signal, even when the command ignores
// cancellation (spec 0207 D1).
func TestExecute_SecondSignalForcesExit(t *testing.T) {
	t.Parallel()

	props, spy := newSignalTestProps()

	started := make(chan struct{})
	unblock := make(chan struct{})

	cmd := &cobra.Command{
		Use: "stubborn",
		RunE: func(_ *cobra.Command, _ []string) error {
			close(started)
			// Deliberately ignores ctx cancellation — simulates hung cleanup.
			<-unblock

			return nil
		},
	}
	cmd.SetArgs([]string{})
	rootCmd := setup.Wrap("", cmd)

	sigCh := make(chan os.Signal, 2)

	go func() {
		<-started
		sigCh <- syscall.SIGINT
		sigCh <- syscall.SIGTERM
	}()

	term := &terminateSpy{}

	var release sync.Once

	opts := executeOptions{
		signals:     sigCh,
		exitProcess: spy.exit,
		terminate: func(sig os.Signal, code int) {
			term.terminate(sig, code)
			release.Do(func() { close(unblock) }) // let the hung command return so the test can finish
		},
	}

	runExecute(t, rootCmd, props, opts)

	calls := term.recorded()
	require.NotEmpty(t, calls, "second signal did not end the run")
	assert.Equal(t, terminateCall{sig: syscall.SIGTERM, code: 143}, calls[0], "the second signal ends the run")
	assert.Empty(t, spy.codes)
}

// TestExecute_FlushRunsOnCancellationPath proves spec D4: the deferred
// telemetry flush runs when a signal cancels the run, before the process
// exits through the ErrorHandler.
func TestExecute_FlushRunsOnCancellationPath(t *testing.T) {
	t.Parallel()

	props, spy := newSignalTestProps()

	backend := &spyTelemetryBackend{}
	props.Collector = telemetry.NewCollector(telemetry.Config{Enabled: true}, backend,
		"tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", p.DeliveryAtLeastOnce, false)

	started := make(chan struct{})
	rootCmd := newBlockingCommand(started)
	sigCh := make(chan os.Signal, 2)

	go func() {
		<-started
		sigCh <- syscall.SIGINT
	}()

	flushedAtTerminate := false
	term := &terminateSpy{}

	runExecute(t, rootCmd, props, executeOptions{signals: sigCh, exitProcess: spy.exit, terminate: func(sig os.Signal, code int) {
		flushedAtTerminate = backend.closed
		term.terminate(sig, code)
	}})

	assert.True(t, flushedAtTerminate, "telemetry must be flushed before the run ends by its signal")
	assert.Equal(t, []terminateCall{{sig: syscall.SIGINT, code: 130}}, term.recorded())
}

// TestExecute_FlushRunsBeforeFatalErrorExit proves the flush also runs on the
// ordinary fatal-error path: ErrorHandler.Check exits the process, so the
// flush cannot be left to a deferred call alone.
func TestExecute_FlushRunsBeforeFatalErrorExit(t *testing.T) {
	t.Parallel()

	props, spy := newSignalTestProps()

	backend := &spyTelemetryBackend{}
	props.Collector = telemetry.NewCollector(telemetry.Config{Enabled: true}, backend,
		"tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", p.DeliveryAtLeastOnce, false)

	flushedAtExit := false

	// Observe at the moment execute terminates: had the flush already run?
	// That question used to be asked from the handler's exit hook, because the
	// handler did the exiting. It now belongs to execute's own exit seam.
	exitProbe := func(code int) {
		spy.exit(code)

		flushedAtExit = backend.closed
	}

	cmd := &cobra.Command{
		Use:  "failing",
		RunE: func(_ *cobra.Command, _ []string) error { return assert.AnError },
	}
	cmd.SetArgs([]string{})

	runExecute(t, setup.Wrap("", cmd), props, executeOptions{exitProcess: exitProbe})

	require.Equal(t, []int{1}, spy.codes, "a normal error still exits 1")
	assert.True(t, flushedAtExit, "telemetry must be flushed before the fatal exit fires")
}

// TestExecute_SuccessDoesNotExit confirms a clean run neither calls the exit
// function nor reports a signal.
func TestExecute_SuccessDoesNotExit(t *testing.T) {
	t.Parallel()

	props, spy := newSignalTestProps()

	ran := false
	cmd := &cobra.Command{
		Use: "ok",
		RunE: func(_ *cobra.Command, _ []string) error {
			ran = true

			return nil
		},
	}
	cmd.SetArgs([]string{})

	runExecute(t, setup.Wrap("", cmd), props, executeOptions{exitProcess: spy.exit})

	assert.True(t, ran)
	assert.Empty(t, spy.codes, "a successful run must not invoke the exit function")
}

// TestExecute_UpdateCompleteReturnsCleanly preserves the existing
// ErrUpdateComplete special case under the signal-aware wrapper.
func TestExecute_UpdateCompleteReturnsCleanly(t *testing.T) {
	t.Parallel()

	props, spy := newSignalTestProps()

	cmd := &cobra.Command{
		Use:  "updated",
		RunE: func(_ *cobra.Command, _ []string) error { return ErrUpdateComplete },
	}
	cmd.SetArgs([]string{})

	runExecute(t, setup.Wrap("", cmd), props, executeOptions{exitProcess: spy.exit})

	assert.Empty(t, spy.codes, "ErrUpdateComplete must not be treated as a fatal error")
}

// TestExecute_ErrUpdateCompleteStillTellsTheUser pins the user-visible half of
// spec 0002 D10. The message used to come from an explicit Logger.Warn in
// execute; it now travels on the sentinel as an Outcome, and Fatal reports it.
// Exit code and log line are separate promises, and only the code was covered.
func TestExecute_ErrUpdateCompleteStillTellsTheUser(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	props := &p.Props{
		Logger:       buf,
		ErrorHandler: errorhandling.New(logger.ToSlog(buf), nil),
	}

	cmd := &cobra.Command{
		Use:  "root",
		RunE: func(*cobra.Command, []string) error { return ErrUpdateComplete },
	}
	cmd.SetArgs([]string{})

	exited := false
	execute(setup.Wrap("", cmd), props, executeOptions{exitProcess: func(int) { exited = true }})

	assert.False(t, exited, "a completed update exits zero, so nothing terminates")
	assert.True(t, buf.Contains("update complete — please run the command again"),
		"the outcome's message must still reach the user")
}

// TestSignalExitCode covers the 128+signum mapping, including the fallback
// for non-syscall signals.
func TestSignalExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{name: "SIGINT", sig: syscall.SIGINT, want: 130},
		{name: "SIGTERM", sig: syscall.SIGTERM, want: 143},
		{name: "os.Interrupt", sig: os.Interrupt, want: 130},
		{name: "non-syscall signal falls back to SIGINT", sig: fakeSignal{}, want: 130},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, signalExitCode(tt.sig))
		})
	}
}

type fakeSignal struct{}

func (fakeSignal) String() string { return "fake" }
func (fakeSignal) Signal()        {}

// TestExecute_FailedDrainExitsWithItsOwnCode: an error the command returns
// after a signal, other than the cancellation, is reported like any failure and
// ends the run with its own code, not by the signal (spec 0207 D4).
func TestExecute_FailedDrainExitsWithItsOwnCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		returned   func(ctx context.Context) error
		wantExit   []int
		wantSignal bool
	}{
		{name: "a failed drain", returned: func(context.Context) error { return assert.AnError }, wantExit: []int{1}},
		{name: "nil", returned: func(context.Context) error { return nil }, wantSignal: true},
		{name: "the context's error", returned: func(ctx context.Context) error { return ctx.Err() }, wantSignal: true},
		{name: "a wrapped cancellation", returned: func(ctx context.Context) error {
			return fmt.Errorf("worker stopped: %w", ctx.Err())
		}, wantSignal: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			props, spy := newSignalTestProps()
			started := make(chan struct{})

			cmd := &cobra.Command{
				Use: "draining",
				RunE: func(c *cobra.Command, _ []string) error {
					close(started)
					<-c.Context().Done()

					return tt.returned(c.Context())
				},
			}
			cmd.SetArgs([]string{})

			sigCh := make(chan os.Signal, 2)

			go func() {
				<-started
				sigCh <- syscall.SIGTERM
			}()

			term := &terminateSpy{}

			runExecute(t, setup.Wrap("", cmd), props, executeOptions{signals: sigCh, exitProcess: spy.exit, terminate: term.terminate})

			assert.Equal(t, tt.wantExit, spy.codes)

			if tt.wantSignal {
				assert.Equal(t, []terminateCall{{sig: syscall.SIGTERM, code: 143}}, term.recorded())
			} else {
				assert.Empty(t, term.recorded(), "a failed drain does not end by the signal")
			}
		})
	}
}
