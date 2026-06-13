package root

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// executeOptions carries the injectable seams execute needs so tests can
// drive signal handling without raising real OS signals or terminating the
// test process. The zero value wires real signals and os.Exit.
type executeOptions struct {
	// signals is the signal source. When nil, execute subscribes to
	// os.Interrupt and syscall.SIGTERM via signal.Notify.
	signals chan os.Signal

	// forceExit terminates the process when a second signal arrives while the
	// first is still being handled. Defaults to os.Exit.
	forceExit errorhandling.ExitFunc
}

// Execute runs the root command with centralized error handling and a
// signal-aware execution context. SIGINT/SIGTERM cancel cmd.Context() so
// commands can unwind gracefully; a second signal force-exits immediately
// (kubectl/docker UX); a signal-terminated run exits 128+signum (130 for
// SIGINT, 143 for SIGTERM) through the ErrorHandler's exit path.
//
// It silences Cobra's default error output and routes any error returned by
// the command tree through ErrorHandler.Check at Fatal level. The buffered
// telemetry flush runs on every path — success, error, and cancellation —
// before any exit fires.
func Execute(rootCmd *setup.Command, props *p.Props) {
	execute(rootCmd, props, executeOptions{})
}

func execute(rootCmd *setup.Command, props *p.Props, opts executeOptions) {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return errors.WithHintf(err, "Run '%s --help' for usage.", cmd.CommandPath())
	})

	// The ErrorHandler's fatal path calls os.Exit, which skips deferred calls.
	// Flush exactly once: explicitly before every fatal exit below, and via
	// defer for the non-exiting paths.
	var flushOnce sync.Once

	flush := func() { flushOnce.Do(func() { flushTelemetry(props) }) }
	defer flush()

	ctx, receivedSignal := signalAwareContext(props, opts)

	err := rootCmd.ExecuteContext(ctx)

	if sig := receivedSignal(); sig != nil {
		// The run was interrupted: exit 128+signum regardless of what the
		// command tree returned (usually context.Canceled). An interrupt is a
		// deliberate user choice, not a failure — the non-zero exit code is the
		// signal, so the notice is logged at debug (LevelFatalQuiet), not error.
		// The exit path is otherwise identical to LevelFatal: the flush above has
		// already run, and the attached 128+signum code is honoured.
		flush()
		props.ErrorHandler.Check(
			errorhandling.WithExitCode(
				errors.Newf("interrupted by signal: %v", sig),
				signalExitCode(sig),
			), "", errorhandling.LevelFatalQuiet)

		return
	}

	if err != nil {
		if errors.Is(err, ErrUpdateComplete) {
			props.Logger.Warnf("update complete — please run the command again")

			return
		}

		flush()
		props.ErrorHandler.Check(err, "", errorhandling.LevelFatal)
	}
}

// signalAwareContext derives the cancellable execution context for the
// command tree. The first signal cancels the context (graceful shutdown);
// a second signal force-exits immediately so a hung cleanup can never trap
// the user. The returned func reports the first signal received, if any —
// call it only after the command tree has returned.
//
// The watcher goroutine is released when the calling function returns (via
// the registered cleanup), so it never outlives the run.
func signalAwareContext(props *p.Props, opts executeOptions) (context.Context, func() os.Signal) {
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := opts.signals
	if sigCh == nil {
		// Buffer two signals: the graceful first and the force-exit second.
		sigCh = make(chan os.Signal, signalBuffer)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	}

	forceExit := opts.forceExit
	if forceExit == nil {
		forceExit = os.Exit
	}

	done := make(chan struct{})

	var (
		mu       sync.Mutex
		received os.Signal
	)

	go func() {
		select {
		case sig := <-sigCh:
			mu.Lock()
			received = sig
			mu.Unlock()

			props.Logger.Warnf("received %v — shutting down gracefully (press again to force quit)", sig)
			cancel()
		case <-done:
			return
		}

		select {
		case sig := <-sigCh:
			// Second signal: force-exit immediately, mirroring kubectl/docker.
			forceExit(signalExitCode(sig))
		case <-done:
		}
	}()

	return ctx, func() os.Signal {
		// Stop watching, restore default disposition, and release the watcher.
		if opts.signals == nil {
			signal.Stop(sigCh)
		}

		close(done)
		cancel()

		mu.Lock()
		defer mu.Unlock()

		return received
	}
}

// signalExitCode maps a termination signal to the conventional 128+signum
// process exit code (130 for SIGINT, 143 for SIGTERM). Non-syscall signals
// fall back to the SIGINT code.
func signalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return signalExitBase + int(s)
	}

	return signalExitBase + int(syscall.SIGINT)
}

const (
	// signalExitBase is the conventional offset added to a signal number to
	// form the process exit code of a signal-terminated run.
	signalExitBase = 128

	// signalBuffer sizes the notification channel for the two signals the
	// lifecycle distinguishes: graceful (first) and force-exit (second).
	signalBuffer = 2
)

// flushTelemetry sends any buffered telemetry events and shuts down the
// backend. Uses a bounded background context so command-context cancellation
// does not interrupt the flush.
func flushTelemetry(props *p.Props) {
	// Gate on the collector's resolved enabled state, not the raw config key:
	// the collector may have been enabled via the TELEMETRY_ENABLED env var or
	// the tool-author ForceEnabled path even when telemetry.enabled is false,
	// and those buffered events must still be flushed.
	if props.Collector == nil || !props.Collector.Enabled() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
	defer cancel()

	_ = props.Collector.Close(ctx)
}
