package controls_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
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

// newQuietController builds a controller with no signals and a noop logger,
// the standard setup for the lifecycle regression tests.
func newQuietController(t *testing.T, opts ...controls.ControllerOpt) *controls.Controller {
	t.Helper()

	base := []controls.ControllerOpt{
		controls.WithoutSignals(),
		controls.WithLogger(logger.NewNoop()),
	}
	base = append(base, opts...)

	return controls.NewController(context.Background(), base...)
}

// TestErrorHandler_NoBusySpinAfterStop is the busy-spin regression. After Stop,
// the error/context handler goroutine must exit (not spin on a permanently-ready
// ctx.Done() case). We assert the goroutine count returns to baseline and stays
// bounded for an idle second.
func TestErrorHandler_NoBusySpinAfterStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := controls.NewController(ctx,
		controls.WithoutSignals(),
		controls.WithLogger(logger.NewNoop()),
	)
	c.Register("svc",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
	)

	before := runtime.NumGoroutine()
	c.Start()

	// Trigger shutdown via context cancellation (exercises the ctx.Done() branch).
	cancel()

	require.Eventually(t, func() bool {
		return c.GetState() == controls.Stopped
	}, 5*time.Second, 10*time.Millisecond)

	c.Wait()

	// Allow goroutines to unwind, then confirm we have not leaked the handler
	// goroutines and that the count is stable (not climbing from a spin spawning
	// or churning work).
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= before+1
	}, 5*time.Second, 20*time.Millisecond, "controller goroutines should terminate after Stop")

	stable := runtime.NumGoroutine()
	time.Sleep(500 * time.Millisecond)
	assert.LessOrEqual(t, runtime.NumGoroutine(), stable+1,
		"goroutine count should remain bounded for an idle second post-Stop")
}

// TestRestart_RunOutcomes exercises D1/D2: clean exit, context cancel, and error
// outcomes each produce the correct supervisor behaviour, and a service whose
// Start returns nil immediately (background-serving) is NOT restarted.
func TestRestart_RunOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// start returns this; if blockUntilCtx, Start blocks until ctx done then
		// returns startErr.
		startErr      error
		blockUntilCtx bool
		policy        *controls.RestartPolicy
		// minStarts/maxStarts bound the number of Start invocations observed
		// before we Stop the controller.
		wantStarts int32
	}{
		{
			name:       "clean-start-not-restarted",
			startErr:   nil,
			policy:     &controls.RestartPolicy{MaxRestarts: 5, InitialBackoff: 5 * time.Millisecond},
			wantStarts: 1, // returning nil is a clean start, never restarted
		},
		{
			name:          "context-cancel-not-restarted",
			blockUntilCtx: true,
			startErr:      context.Canceled,
			policy:        &controls.RestartPolicy{MaxRestarts: 5, InitialBackoff: 5 * time.Millisecond},
			wantStarts:    1, // ctx cancellation is shutdown, not a failure
		},
		{
			name:       "error-restarts-up-to-max",
			startErr:   fmt.Errorf("boom"),
			policy:     &controls.RestartPolicy{MaxRestarts: 3, InitialBackoff: 5 * time.Millisecond, MaxBackoff: 20 * time.Millisecond},
			wantStarts: 4, // 1 initial + 3 restarts
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var starts atomic.Int32

			// Collect everything sent on errs and assert no nil ever arrives.
			errs := make(chan error, 64)
			var sawNil atomic.Bool

			c := newQuietController(t)
			c.SetErrorsChannel(errs)

			c.Register("svc",
				controls.WithStart(func(ctx context.Context) error {
					starts.Add(1)
					if tc.blockUntilCtx {
						<-ctx.Done()
					}
					return tc.startErr
				}),
				controls.WithStop(func(_ context.Context) {}),
				controls.WithRestartPolicy(*tc.policy),
			)

			// Drain errs, flagging any nil.
			go func() {
				for e := range errs {
					if e == nil {
						sawNil.Store(true)
					}
				}
			}()

			c.Start()

			switch tc.name {
			case "error-restarts-up-to-max":
				require.Eventually(t, func() bool {
					return starts.Load() >= tc.wantStarts
				}, 5*time.Second, 10*time.Millisecond)
			default:
				// Give the supervisor time to (wrongly) restart if buggy.
				time.Sleep(150 * time.Millisecond)
			}

			c.Stop()
			c.Wait()
			close(errs)

			assert.Equal(t, tc.wantStarts, starts.Load(), "unexpected number of Start invocations")
			assert.False(t, sawNil.Load(), "nil must never be sent on the error channel")
		})
	}
}

// TestStart_Idempotent verifies D3: a double Start does not double-start services
// and Wait still returns.
func TestStart_Idempotent(t *testing.T) {
	t.Parallel()

	var starts atomic.Int32

	c := newQuietController(t)
	c.Register("svc",
		controls.WithStart(func(_ context.Context) error { starts.Add(1); return nil }),
		controls.WithStop(func(_ context.Context) {}),
	)

	c.Start()
	c.Start() // second call must be a no-op

	require.True(t, c.IsRunning())

	// Wait for the single start to land.
	require.Eventually(t, func() bool {
		return starts.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)

	// Give a buggy double-start a chance to manifest.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), starts.Load(), "service must start exactly once")

	c.Stop()

	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() hung after idempotent Start")
	}
}

// TestNilFuncs_NoPanic verifies D5: a service registered without Start/Stop does
// not panic at start or shutdown.
func TestNilFuncs_NoPanic(t *testing.T) {
	t.Parallel()

	c := newQuietController(t)
	c.Register("no-funcs") // no WithStart / WithStop

	require.NotPanics(t, func() {
		c.Start()
		c.Stop()
		c.Wait()
	})

	assert.True(t, c.IsStopped())
}

// TestSignals_WithoutSignals verifies D6: WithoutSignals leaves the signal channel
// nil so SIGINT/SIGTERM keep their default disposition (no Notify registered).
func TestSignals_WithoutSignals(t *testing.T) {
	t.Parallel()

	c := controls.NewController(context.Background(),
		controls.WithoutSignals(),
		controls.WithLogger(logger.NewNoop()),
	)
	assert.Nil(t, c.Signals(), "WithoutSignals must leave the signal channel nil")
}

// TestSignals_CustomChannelReceives verifies a custom channel via SetSignalsChannel
// actually drives shutdown.
func TestSignals_CustomChannelReceives(t *testing.T) {
	t.Parallel()

	sigs := make(chan os.Signal, 1)

	c := controls.NewController(context.Background(),
		controls.WithLogger(logger.NewNoop()),
	)
	c.SetSignalsChannel(sigs)

	c.Register("svc",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
	)

	c.Start()

	sigs <- syscall.SIGTERM

	require.Eventually(t, func() bool {
		return c.GetState() == controls.Stopped
	}, 5*time.Second, 10*time.Millisecond, "custom signal channel should drive shutdown")
}

// TestForceStop_AbandonsContextIgnoringStop verifies D6: a StopFunc that ignores
// its context is abandoned at the shutdown deadline and Wait() returns.
func TestForceStop_AbandonsContextIgnoringStop(t *testing.T) {
	t.Parallel()

	stopEntered := make(chan struct{})

	c := newQuietController(t, controls.WithShutdownTimeout(100*time.Millisecond))
	c.Register("stubborn",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {
			close(stopEntered)
			// Ignore ctx entirely and block far longer than the shutdown timeout.
			time.Sleep(10 * time.Second)
		}),
	)

	c.Start()
	c.Stop()

	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()

	select {
	case <-stopEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("stop func was never entered")
	}

	select {
	case <-done:
		// Wait returned despite the stubborn stop — force-stop worked.
	case <-time.After(3 * time.Second):
		t.Fatal("Wait() hung: context-ignoring StopFunc was not abandoned at deadline")
	}

	assert.True(t, c.IsStopped())
}

// TestStop_ReverseOrder verifies D6: services stop in reverse registration order.
func TestStop_ReverseOrder(t *testing.T) {
	t.Parallel()

	var order []string
	var mu sync.Mutex

	c := newQuietController(t)
	for _, name := range []string{"first", "second", "third"} {
		name := name
		c.Register(name,
			controls.WithStart(func(_ context.Context) error { return nil }),
			controls.WithStop(func(_ context.Context) {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
			}),
		)
	}

	c.Start()
	c.Stop()
	c.Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"third", "second", "first"}, order,
		"services should stop in reverse registration order")
}

// TestReadiness_FailsClosedBeforeFirstAsyncCheck verifies D7: an async check
// reports not-ready until its first run completes.
func TestReadiness_FailsClosedBeforeFirstAsyncCheck(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var ran atomic.Bool

	c := newQuietController(t)
	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "slow-async",
		Check: func(_ context.Context) controls.CheckResult {
			<-release // block the first run until the test releases it
			ran.Store(true)
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
		Interval: 10 * time.Millisecond,
		Type:     controls.CheckTypeReadiness,
	}))

	c.Start()
	t.Cleanup(func() {
		close(release)
		c.Stop()
		c.Wait()
	})

	// Before the first async run completes, readiness must fail closed.
	report := c.Readiness()
	assert.False(t, report.OverallHealthy,
		"readiness must report not-ready until the first async check completes")
	assert.False(t, ran.Load())
}

// TestWithRestartResetInterval_ResetsCounter verifies D2: after running healthily
// for at least the reset interval, the consecutive-failure counter resets so the
// service is not prematurely declared as having exceeded MaxRestarts.
func TestWithRestartResetInterval_ResetsCounter(t *testing.T) {
	t.Parallel()

	var starts atomic.Int32
	// Fail the first two runs quickly, then run "healthily" (block on ctx) so the
	// reset interval elapses. With a tiny reset interval the counter resets and
	// the service is never declared max-restarts-exceeded.
	c := newQuietController(t)
	c.Register("flaky",
		controls.WithStart(func(ctx context.Context) error {
			n := starts.Add(1)
			if n <= 2 {
				return fmt.Errorf("startup failure %d", n)
			}
			// Healthy run: block until shutdown.
			<-ctx.Done()
			return ctx.Err()
		}),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithRestartPolicy(controls.RestartPolicy{
			MaxRestarts:    2,
			InitialBackoff: 5 * time.Millisecond,
		}),
		controls.WithRestartResetInterval(1*time.Millisecond),
	)

	c.Start()

	require.Eventually(t, func() bool {
		return starts.Load() >= 3
	}, 5*time.Second, 10*time.Millisecond, "service should reach a healthy run after transient failures")

	info, ok := c.GetServiceInfo("flaky")
	require.True(t, ok)
	if info.Error != nil {
		assert.NotContains(t, info.Error.Error(), "max restarts exceeded",
			"counter should have reset during the healthy window")
	}

	c.Stop()
	c.Wait()
}

// TestWithRestartResetInterval_ImpliesPolicy verifies that applying the reset
// interval to a service with no restart policy creates one (so the interval
// takes effect) without otherwise enabling restarts.
func TestWithRestartResetInterval_ImpliesPolicy(t *testing.T) {
	t.Parallel()

	var starts atomic.Int32

	c := newQuietController(t)
	c.Register("reset-only",
		controls.WithStart(func(_ context.Context) error { starts.Add(1); return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithRestartResetInterval(5*time.Millisecond),
	)

	c.Start()
	require.Eventually(t, func() bool { return starts.Load() == 1 }, 2*time.Second, 10*time.Millisecond)

	// A clean start is never restarted even though a (default) policy now exists.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), starts.Load())

	c.Stop()
	c.Wait()
}

// TestNilStop_ReachesShutdown ensures the no-op Stop default is invoked at
// shutdown for a service that registered a Start but no Stop.
func TestNilStop_ReachesShutdown(t *testing.T) {
	t.Parallel()

	var started atomic.Bool

	c := newQuietController(t)
	c.Register("start-only",
		controls.WithStart(func(_ context.Context) error { started.Store(true); return nil }),
		// No WithStop: defaults to a no-op that must be safe to call at shutdown.
	)

	c.Start()
	require.Eventually(t, started.Load, 2*time.Second, 10*time.Millisecond)

	require.NotPanics(t, func() {
		c.Stop()
		c.Wait()
	})
	assert.True(t, c.IsStopped())
}

// TestSignals_SecondSignalForcesHandlerExit verifies D4: a second signal arriving
// while shutdown is in progress forces the signal-handler goroutine to exit
// rather than block.
func TestSignals_SecondSignalForcesHandlerExit(t *testing.T) {
	t.Parallel()

	sigs := make(chan os.Signal, 2)

	c := controls.NewController(context.Background(), controls.WithLogger(logger.NewNoop()))
	c.SetSignalsChannel(sigs)

	// A stop that lingers so the second signal lands during shutdown.
	c.Register("lingering",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) { time.Sleep(50 * time.Millisecond) }),
	)

	c.Start()

	sigs <- syscall.SIGINT  // initiates graceful stop
	sigs <- syscall.SIGTERM // second signal forces handler exit

	require.Eventually(t, func() bool {
		return c.GetState() == controls.Stopped
	}, 5*time.Second, 10*time.Millisecond)

	c.Wait()
}

// TestValidError_ExemptsExpectedTerminalErrors verifies D7/D2: an error matched by
// ValidErrorFunc does not count toward the restart total nor get forwarded as a
// failure.
func TestValidError_ExemptsExpectedTerminalErrors(t *testing.T) {
	t.Parallel()

	var starts atomic.Int32

	errs := make(chan error, 16)
	var forwarded atomic.Int32

	c := newQuietController(t, controls.WithValidError(func(err error) bool {
		return errors.Is(err, http.ErrServerClosed)
	}))
	c.SetErrorsChannel(errs)

	c.Register("http-like",
		controls.WithStart(func(_ context.Context) error {
			starts.Add(1)
			return http.ErrServerClosed
		}),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithRestartPolicy(controls.RestartPolicy{
			MaxRestarts:    3,
			InitialBackoff: 5 * time.Millisecond,
		}),
	)

	go func() {
		for range errs {
			forwarded.Add(1)
		}
	}()

	c.Start()

	// http.ErrServerClosed is "valid" — must not restart.
	time.Sleep(150 * time.Millisecond)

	c.Stop()
	c.Wait()
	close(errs)

	assert.Equal(t, int32(1), starts.Load(), "valid terminal error must not trigger a restart")
	assert.Equal(t, int32(0), forwarded.Load(), "valid terminal error must not be forwarded as a failure")
}
