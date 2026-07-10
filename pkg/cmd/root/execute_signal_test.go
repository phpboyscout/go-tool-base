package root

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
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

// newSignalTestProps builds a minimal Props whose ErrorHandler routes exits
// through the returned spy instead of os.Exit.
func newSignalTestProps() (*p.Props, *exitSpy) {
	spy := &exitSpy{}
	props := &p.Props{
		Logger: logger.NewNoop(),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil,
			errorhandling.WithExitFunc(spy.exit)),
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

// TestExecute_SignalCancelsContextAndSetsExitCode proves spec D1+D2: the
// first signal cancels cmd.Context() (a command blocking on Done() unwinds
// promptly) and the run exits 128+signum through the ErrorHandler.
func TestExecute_SignalCancelsContextAndSetsExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		signal   os.Signal
		wantCode int
	}{
		{name: "SIGINT exits 130", signal: syscall.SIGINT, wantCode: 130},
		{name: "SIGTERM exits 143", signal: syscall.SIGTERM, wantCode: 143},
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

			runExecute(t, rootCmd, props, executeOptions{signals: sigCh})

			assert.Equal(t, []int{tt.wantCode}, spy.codes,
				"a signal-terminated run must exit 128+signum via the ErrorHandler")
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
		Logger: buf,
		ErrorHandler: errorhandling.New(logger.ToSlog(buf), nil,
			errorhandling.WithExitFunc(spy.exit)),
	}

	started := make(chan struct{})
	rootCmd := newBlockingCommand(started)
	sigCh := make(chan os.Signal, 2)

	go func() {
		<-started
		sigCh <- syscall.SIGINT
	}()

	runExecute(t, rootCmd, props, executeOptions{signals: sigCh})

	assert.Equal(t, []int{130}, spy.codes, "interrupted run still exits 130")

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

// TestExecute_SecondSignalForcesExit proves spec D2: a second signal
// force-exits immediately even when the command ignores cancellation.
func TestExecute_SecondSignalForcesExit(t *testing.T) {
	t.Parallel()

	props, spy := newSignalTestProps()

	started := make(chan struct{})
	unblock := make(chan struct{})
	forceExited := make(chan int, 1)

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
		sigCh <- syscall.SIGINT
	}()

	opts := executeOptions{
		signals: sigCh,
		forceExit: func(code int) {
			forceExited <- code
			close(unblock) // let the hung command return so the test can finish
		},
	}

	runExecute(t, rootCmd, props, opts)

	select {
	case code := <-forceExited:
		assert.Equal(t, 130, code, "second signal must force-exit with 128+signum")
	default:
		t.Fatal("second signal did not trigger the force-exit path")
	}

	assert.NotEmpty(t, spy.codes, "interrupted run still reports the signal exit code")
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

	runExecute(t, rootCmd, props, executeOptions{signals: sigCh})

	assert.True(t, backend.closed, "telemetry must be flushed on the cancellation path")
	assert.Equal(t, []int{130}, spy.codes)
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
	props.ErrorHandler = errorhandling.New(logger.ToSlog(logger.NewNoop()), nil,
		errorhandling.WithExitFunc(func(code int) {
			spy.exit(code)

			flushedAtExit = backend.closed
		}))

	cmd := &cobra.Command{
		Use:  "failing",
		RunE: func(_ *cobra.Command, _ []string) error { return assert.AnError },
	}
	cmd.SetArgs([]string{})

	runExecute(t, setup.Wrap("", cmd), props, executeOptions{})

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

	runExecute(t, setup.Wrap("", cmd), props, executeOptions{})

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

	runExecute(t, setup.Wrap("", cmd), props, executeOptions{})

	assert.Empty(t, spy.codes, "ErrUpdateComplete must not be treated as a fatal error")
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
