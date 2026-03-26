package controls_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type StateCounters struct {
	Started  atomic.Int64
	Stopped  atomic.Int64
	Statused atomic.Int64
}

// syncBuffer is a thread-safe bytes.Buffer for use with slog in tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func getNewController(ctx context.Context) (*controls.Controller, *StateCounters, *syncBuffer) {
	cntrs := &StateCounters{}
	startFunc := func(_ context.Context) error { cntrs.Started.Add(1); return nil }
	stopFunc := func(_ context.Context) { cntrs.Stopped.Add(1) }
	statusFunc := func() error { cntrs.Statused.Add(1); time.Sleep(500 * time.Microsecond); return nil }

	buf := &syncBuffer{}
	l := logger.NewCharm(buf)

	c := controls.NewController(ctx, controls.WithLogger(l))
	c.Register("test",
		controls.WithStart(startFunc),
		controls.WithStop(stopFunc),
		controls.WithStatus(statusFunc),
	)

	return c, cntrs, buf
}

func TestController_Controls(t *testing.T) {
	t.Run("stopping", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		assert.Equal(t, controls.Unknown, c.GetState())
		c.Start()

		assert.True(t, c.IsRunning())

		c.Stop()
		assert.Eventually(t, func() bool {
			return cntrs.Stopped.Load() == int64(1)
		}, 1*time.Second, 10*time.Millisecond)
		assert.True(t, c.IsStopped())
	})

	t.Run("status", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		c.Start()

		assert.True(t, c.IsRunning())
		c.Messages() <- controls.Status
		assert.Eventually(t, func() bool {
			return cntrs.Statused.Load() == int64(1)
		}, 1*time.Second, 10*time.Millisecond)
		assert.True(t, c.IsRunning())
	})

	t.Run("multiple status calls", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		c.Start()

		assert.True(t, c.IsRunning())
		for i := 1; i <= 3; i++ {
			c.Messages() <- controls.Status
			expected := int64(i)
			assert.Eventually(t, func() bool {
				return cntrs.Statused.Load() == expected
			}, 1*time.Second, 10*time.Millisecond)
		}
		assert.True(t, c.IsRunning())
	})

	t.Run("stop running controller", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		c.Start()

		assert.True(t, c.IsRunning())
		c.Messages() <- controls.Stop

		assert.Eventually(t, func() bool {
			return cntrs.Stopped.Load() == int64(1)
		}, 1*time.Second, 10*time.Millisecond)
		assert.True(t, c.IsStopped())
	})

}

func TestController_StartError(t *testing.T) {
	c, _, output := getNewController(context.Background())
	c.Register("test",
		controls.WithStart(func(_ context.Context) error {
			return fmt.Errorf("test error")
		}),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithStatus(func() error { return nil }),
	)

	c.Start()

	assert.Eventually(t, func() bool {
		return strings.Contains(output.String(), "test error")
	}, 1*time.Second, 10*time.Millisecond)
}

func TestController_WaitGroup(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	wg := &sync.WaitGroup{}
	c.SetWaitGroup(wg)
	wg2 := c.WaitGroup()
	assert.Equal(t, wg, wg2)
}

func TestController_SetState(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	c.SetState(controls.Running)
	assert.True(t, c.IsRunning())

	c.SetState(controls.Stopping)
	assert.True(t, c.IsStopping())

	c.SetState(controls.Stopped)
	assert.True(t, c.IsStopped())
}

func TestController_Errors(t *testing.T) {
	c, _, output := getNewController(context.Background())
	errs := make(chan error)
	c.SetErrorsChannel(errs)

	c.Start()
	c.Errors() <- fmt.Errorf("test error") //nolint:goerr113

	assert.Eventually(t, func() bool {
		return strings.Contains(output.String(), "test error")
	}, 1*time.Second, 10*time.Millisecond)
}

func TestController_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, _, _ := getNewController(ctx)
	errs := make(chan error)
	c.SetErrorsChannel(errs)

	c.Start()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	c.Wait()
	assert.True(t, c.IsStopped())

}

func TestController_SetMessageChannels(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	msgs := make(chan controls.Message)
	c.SetMessageChannel(msgs)
	assert.Equal(t, msgs, c.Messages())
}

func TestController_Health(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	health := make(chan controls.HealthMessage)
	c.SetHealthChannel(health)

	go func(t *testing.T, health chan controls.HealthMessage) {
		h := <-health
		assert.Equal(t, "testHost", h.Host)
		assert.Equal(t, 1, h.Port)
		assert.Equal(t, 2, h.Status)
		assert.Equal(t, "testMessage", h.Message)
	}(t, health)

	c.Health() <- controls.HealthMessage{
		Host:    "testHost",
		Port:    1,
		Status:  2,
		Message: "testMessage",
	}
}

func TestStop_ConcurrentCalls(t *testing.T) {
	c, cntrs, _ := getNewController(context.Background())
	c.Start()
	assert.True(t, c.IsRunning())

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Stop()
		}()
	}
	wg.Wait()

	assert.Eventually(t, func() bool {
		return c.IsStopped()
	}, 2*time.Second, 10*time.Millisecond)

	// Stop function of the service should have been called exactly once
	assert.Equal(t, int64(1), cntrs.Stopped.Load(), "stop should execute exactly once")
}

// Compile-time interface satisfaction checks (also exercised at test time).
var (
	_ controls.Runner          = (*controls.Controller)(nil)
	_ controls.StateAccessor   = (*controls.Controller)(nil)
	_ controls.Configurable    = (*controls.Controller)(nil)
	_ controls.ChannelProvider = (*controls.Controller)(nil)
	_ controls.Controllable    = (*controls.Controller)(nil)
)

func TestControllerOpt_WithConfigurable(t *testing.T) {
	// Verify that WithoutSignals works with the Configurable-typed parameter.
	opt := controls.WithoutSignals()
	c := controls.NewController(context.Background(), opt)
	assert.Nil(t, c.Signals())
}

func TestStop_AlreadyStopped(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	c.Start()
	assert.True(t, c.IsRunning())

	c.Stop()
	assert.Eventually(t, func() bool {
		return c.IsStopped()
	}, 2*time.Second, 10*time.Millisecond)

	// Calling Stop again should be a no-op (not panic or block)
	c.Stop()
	assert.True(t, c.IsStopped())
}

func TestController_Status(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := controls.NewController(ctx, controls.WithoutSignals())

	c.Register("healthy-service",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithStatus(func() error { return nil }),
	)

	c.Register("unhealthy-service",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithStatus(func() error { return fmt.Errorf("service failed") }),
	)

	report := c.Status()

	assert.False(t, report.OverallHealthy)
	assert.Len(t, report.Services, 2)

	var healthy, unhealthy bool
	for _, s := range report.Services {
		if s.Name == "healthy-service" {
			assert.Equal(t, "OK", s.Status)
			assert.Empty(t, s.Error)
			healthy = true
		}
		if s.Name == "unhealthy-service" {
			assert.Equal(t, "ERROR", s.Status)
			assert.Equal(t, "service failed", s.Error)
			unhealthy = true
		}
	}
	assert.True(t, healthy)
	assert.True(t, unhealthy)
}

func TestController_Probes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := controls.NewController(ctx, controls.WithoutSignals())

	c.Register("service-1",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithLiveness(func() error { return nil }),
		controls.WithReadiness(func() error { return fmt.Errorf("not ready") }),
	)

	c.Register("service-2",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		// No probes provided, should fall back to Status which defaults to nil (OK)
	)

	// Liveness should be healthy (service-1 is OK, service-2 uses default OK)
	liveReport := c.Liveness()
	assert.True(t, liveReport.OverallHealthy)

	// Readiness should be unhealthy (service-1 reports error)
	readyReport := c.Readiness()
	assert.False(t, readyReport.OverallHealthy)

	var foundS1 bool
	for _, s := range readyReport.Services {
		if s.Name == "service-1" {
			assert.Equal(t, "ERROR", s.Status)
			assert.Equal(t, "not ready", s.Error)
			foundS1 = true
		}
	}
	assert.True(t, foundS1)
}

func TestController_Supervisor_NoPolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := controls.NewController(ctx, controls.WithoutSignals())

	starts := atomic.Int32{}
	c.Register("failing-service",
		controls.WithStart(func(_ context.Context) error {
			starts.Add(1)
			return fmt.Errorf("immediate failure")
		}),
		controls.WithStop(func(_ context.Context) {}),
	)

	c.Start()
	
	// Wait a moment to see if it restarts
	time.Sleep(50 * time.Millisecond)
	c.Stop()
	c.Wait()

	assert.Equal(t, int32(1), starts.Load(), "Service should only start once without a policy")
}

func TestController_Supervisor_WithPolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := controls.NewController(ctx, controls.WithoutSignals())

	starts := atomic.Int32{}
	c.Register("restarting-service",
		controls.WithStart(func(_ context.Context) error {
			starts.Add(1)
			return fmt.Errorf("transient failure")
		}),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithRestartPolicy(controls.RestartPolicy{
			MaxRestarts:    3,
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
		}),
	)

	c.Start()

	// Wait for the restart policy to exhaust all retries.
	// Under the race detector, backoff sleeps take longer than wall-clock
	// time, so use Eventually rather than a fixed sleep.
	assert.Eventually(t, func() bool {
		return starts.Load() >= 4
	}, 5*time.Second, 10*time.Millisecond, "Service should restart up to MaxRestarts times")

	c.Stop()
	c.Wait()

	// 1 initial start + 3 restarts
	assert.Equal(t, int32(4), starts.Load(), "Service should restart up to MaxRestarts times")
}

func TestController_Supervisor_HealthTriggered(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := controls.NewController(ctx, controls.WithoutSignals())

	starts := atomic.Int32{}
	statusCalls := atomic.Int32{}

	c.Register("health-monitored-service",
		controls.WithStart(func(_ context.Context) error {
			starts.Add(1)
			return nil // starts successfully
		}),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithStatus(func() error {
			calls := statusCalls.Add(1)
			if calls > 1 {
				return fmt.Errorf("unhealthy")
			}
			return nil
		}),
		controls.WithRestartPolicy(controls.RestartPolicy{
			HealthFailureThreshold: 2,
			HealthCheckInterval:    10 * time.Millisecond,
			InitialBackoff:         5 * time.Millisecond,
		}),
	)

	c.Start()

	require.Eventually(t, func() bool {
		return starts.Load() >= 2
	}, 5*time.Second, 10*time.Millisecond, "service should restart due to health failures")

	c.Stop()
	c.Wait()
}

func TestController_ServiceInfo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := controls.NewController(ctx, controls.WithoutSignals())

	c.Register("test-service",
		controls.WithStart(func(_ context.Context) error {
			return fmt.Errorf("initial failure")
		}),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithRestartPolicy(controls.RestartPolicy{
			MaxRestarts:    1,
			InitialBackoff: 5 * time.Millisecond,
		}),
	)

	c.Start()
	time.Sleep(50 * time.Millisecond) // Wait for initial start, failure, backoff, restart, failure
	c.Stop()
	c.Wait()

	info, ok := c.GetServiceInfo("test-service")
	assert.True(t, ok)
	assert.Equal(t, "test-service", info.Name)
	assert.Equal(t, 1, info.RestartCount)
	assert.NotZero(t, info.LastStarted)
	assert.NotZero(t, info.LastStopped)
	assert.ErrorContains(t, info.Error, "max restarts exceeded")

	_, ok = c.GetServiceInfo("non-existent")
	assert.False(t, ok)
}

func TestControllerErrorHandler_ExitsOnClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Use a buffered channel so we can enqueue errors before the goroutine starts.
	errs := make(chan error, 4)

	c := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(logger.NewNoop()))
	c.SetErrorsChannel(errs)

	c.Start()

	// Enqueue a non-fatal error, then close the channel.
	// When the error handler goroutine reads the buffered error it will log it,
	// then the next read returns ok=false and the goroutine exits cleanly.
	errs <- fmt.Errorf("test error")
	close(errs)

	// The controller should stop and Wait should not deadlock.
	c.Stop()
	c.Wait()
}

func TestControllerErrorHandler_ExitsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(logger.NewNoop()))
	c.Start()

	// Cancelling the parent context triggers the ctx.Done() branch in the error
	// handler goroutine, which then calls c.Stop() internally.
	cancel()

	// The controller should reach Stopped state without blocking.
	require.Eventually(t, func() bool {
		return c.GetState() == controls.Stopped
	}, 2*time.Second, 10*time.Millisecond, "controller should reach Stopped state after context cancel")

	c.Wait()
}
