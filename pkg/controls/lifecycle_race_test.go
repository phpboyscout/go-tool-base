package controls_test

import (
	"context"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// TestStart_ShutdownDuringStartupNoCancelRace verifies D8: a shutdown triggered
// while Start() is still wiring up async health checks must not race on
// healthCheckEntry.cancel. The signal handler and message processor are launched
// by Start before the per-check CancelFuncs are recorded; a shutdown in that
// window has the message processor read entry.cancel (via cancelHealthChecks)
// while Start is still writing it.
//
// A buffered signal pre-loaded before Start guarantees the signal handler fires
// at the earliest possible moment after the control goroutines launch, maximally
// overlapping the two accesses. Run under -race, this flags the unsynchronised
// read/write if the setup ordering regresses.
func TestStart_ShutdownDuringStartupNoCancelRace(t *testing.T) {
	t.Parallel()

	for iter := 0; iter < 20; iter++ {
		func() {
			sigs := make(chan os.Signal, 1)
			// Pre-load the signal so the handler stops the controller the instant
			// the control goroutines start.
			sigs <- syscall.SIGTERM

			c := controls.NewController(context.Background(),
				controls.WithLogger(logger.ToSlog(logger.NewNoop())),
			)
			c.SetSignalsChannel(sigs)

			// Register several async checks so startAsyncHealthChecks writes many
			// entry.cancel fields, widening the overlap with cancelHealthChecks.
			for i := 0; i < 16; i++ {
				name := "async-" + string(rune('a'+i))
				require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
					Name: name,
					Check: func(_ context.Context) controls.CheckResult {
						return controls.CheckResult{Status: controls.CheckHealthy}
					},
					Interval: time.Hour, // async, but never actually re-ticks during the test
					Type:     controls.CheckTypeReadiness,
				}))
			}

			c.Start()

			require.Eventually(t, func() bool {
				return c.GetState() == controls.Stopped
			}, 5*time.Second, time.Millisecond)

			c.Wait()
		}()
	}
}

// TestSupervise_ErrorForwardDoesNotLeakOnShutdown verifies D9: a service that
// forwards a genuine error must never leave its supervisor goroutine blocked on
// an unbuffered error-channel send after the error handler has exited. The send
// is select-guarded on shutdown completion, so a late forward falls through
// instead of leaking.
//
// This is a stress/leak guard rather than a deterministic reproduction: the
// window (a supervisor descheduled between error classification and the send,
// spanning a full shutdown) is near-unreachable in practice, but a regression to
// an unguarded blocking send would accumulate leaked goroutines across the
// iterations and fail the post-settle goroutine-count check.
func TestSupervise_ErrorForwardDoesNotLeakOnShutdown(t *testing.T) {
	t.Parallel()

	before := runtime.NumGoroutine()

	for iter := 0; iter < 40; iter++ {
		var starts atomic.Int32

		c := controls.NewController(context.Background(),
			controls.WithoutSignals(),
			controls.WithLogger(logger.ToSlog(logger.NewNoop())),
		)
		// A flapping service: returns a genuine error immediately with a tiny
		// backoff, so it forwards errors rapidly right up to shutdown.
		c.Register("flapper",
			controls.WithStart(func(_ context.Context) error {
				starts.Add(1)
				return errors.New("flap")
			}),
			controls.WithStop(func(_ context.Context) {}),
			controls.WithRestartPolicy(controls.RestartPolicy{
				MaxRestarts:    1_000_000,
				InitialBackoff: time.Microsecond,
				MaxBackoff:     time.Microsecond,
			}),
		)

		c.Start()

		// Let it flap a little, then stop while errors are in flight.
		require.Eventually(t, func() bool { return starts.Load() >= 2 }, 5*time.Second, time.Microsecond)
		c.Stop()
		c.Wait()
	}

	// After all controllers have fully stopped and unwound, the goroutine count
	// must return to baseline. A leaked blocking send would keep supervisor
	// goroutines parked above baseline.
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= before+2
	}, 5*time.Second, 20*time.Millisecond, "supervisor goroutines must not leak on error-forward during shutdown")

	assert.LessOrEqual(t, runtime.NumGoroutine(), before+2)
}
