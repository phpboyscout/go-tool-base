package controls_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func newTestController(t *testing.T) *controls.Controller {
	t.Helper()

	c := controls.NewController(context.Background(),
		controls.WithoutSignals(),
		controls.WithLogger(logger.NewNoop()),
	)
	t.Cleanup(func() {
		c.Stop()
		c.Wait()
	})

	return c
}

func TestRegisterHealthCheck_Success(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	err := c.RegisterHealthCheck(controls.HealthCheck{
		Name: "db",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
	})

	assert.NoError(t, err)
}

func TestRegisterHealthCheck_DuplicateName(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	check := controls.HealthCheck{
		Name: "db",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
	}

	require.NoError(t, c.RegisterHealthCheck(check))

	err := c.RegisterHealthCheck(check)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestRegisterHealthCheck_AfterStart(t *testing.T) {
	t.Parallel()

	c := newTestController(t)
	c.Start()

	err := c.RegisterHealthCheck(controls.HealthCheck{
		Name: "db",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "after start")
}

func TestSyncCheck_RunsOnRequest(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int64

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "sync-check",
		Check: func(_ context.Context) controls.CheckResult {
			callCount.Add(1)
			return controls.CheckResult{Status: controls.CheckHealthy, Message: "ok"}
		},
		Type: controls.CheckTypeReadiness,
	}))

	c.Start()

	// Each call to Readiness should invoke the sync check
	c.Readiness()
	c.Readiness()
	c.Readiness()

	assert.Equal(t, int64(3), callCount.Load())
}

func TestAsyncCheck_CachesResult(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int64

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "async-check",
		Check: func(_ context.Context) controls.CheckResult {
			callCount.Add(1)
			return controls.CheckResult{Status: controls.CheckHealthy, Message: "cached"}
		},
		Interval: 1 * time.Second,
		Type:     controls.CheckTypeReadiness,
	}))

	c.Start()

	// Wait for initial async run
	require.Eventually(t, func() bool {
		return callCount.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	// Multiple readiness calls should not trigger additional check runs
	initialCount := callCount.Load()
	c.Readiness()
	c.Readiness()
	c.Readiness()

	assert.Equal(t, initialCount, callCount.Load(), "async check should use cached result")
}

func TestAsyncCheck_RefreshesOnInterval(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int64

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "refresh-check",
		Check: func(_ context.Context) controls.CheckResult {
			callCount.Add(1)
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
		Interval: 50 * time.Millisecond,
		Type:     controls.CheckTypeReadiness,
	}))

	c.Start()

	// Should be called multiple times within a reasonable window
	require.Eventually(t, func() bool {
		return callCount.Load() >= 3
	}, 2*time.Second, 10*time.Millisecond)
}

func TestCheck_Timeout(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "timeout-check",
		Check: func(ctx context.Context) controls.CheckResult {
			<-ctx.Done()
			return controls.CheckResult{Status: controls.CheckUnhealthy, Message: "timed out"}
		},
		Timeout: 50 * time.Millisecond,
		Type:    controls.CheckTypeReadiness,
	}))

	c.Start()

	// Sync check should complete within timeout
	report := c.Readiness()
	found := false
	for _, s := range report.Services {
		if s.Name == "timeout-check" {
			found = true
			assert.Equal(t, "ERROR", s.Status)
		}
	}
	assert.True(t, found, "timeout check should appear in readiness report")
}

func TestCheck_Healthy(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "healthy-check",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy, Message: "all good"}
		},
		Type: controls.CheckTypeReadiness,
	}))

	c.Start()

	report := c.Readiness()
	assert.True(t, report.OverallHealthy)

	found := false
	for _, s := range report.Services {
		if s.Name == "healthy-check" {
			found = true
			assert.Equal(t, "OK", s.Status)
		}
	}
	assert.True(t, found)
}

func TestCheck_Degraded(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "degraded-check",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckDegraded, Message: "pool at 90%"}
		},
		Type: controls.CheckTypeReadiness,
	}))

	c.Start()

	report := c.Readiness()
	assert.True(t, report.OverallHealthy, "degraded should not set OverallHealthy to false")

	found := false
	for _, s := range report.Services {
		if s.Name == "degraded-check" {
			found = true
			assert.Equal(t, "DEGRADED", s.Status)
			assert.Equal(t, "pool at 90%", s.Error)
		}
	}
	assert.True(t, found)
}

func TestCheck_Unhealthy(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "unhealthy-check",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckUnhealthy, Message: "db down"}
		},
		Type: controls.CheckTypeReadiness,
	}))

	c.Start()

	report := c.Readiness()
	assert.False(t, report.OverallHealthy)

	found := false
	for _, s := range report.Services {
		if s.Name == "unhealthy-check" {
			found = true
			assert.Equal(t, "ERROR", s.Status)
			assert.Equal(t, "db down", s.Error)
		}
	}
	assert.True(t, found)
}

func TestCheckType_Readiness(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "readiness-only",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
		Type: controls.CheckTypeReadiness,
	}))

	c.Start()

	// Should appear in readiness and status
	readiness := c.Readiness()
	status := c.Status()
	liveness := c.Liveness()

	assert.True(t, hasCheck(readiness, "readiness-only"), "should appear in readiness")
	assert.True(t, hasCheck(status, "readiness-only"), "should appear in status")
	assert.False(t, hasCheck(liveness, "readiness-only"), "should NOT appear in liveness")
}

func TestCheckType_Liveness(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "liveness-only",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
		Type: controls.CheckTypeLiveness,
	}))

	c.Start()

	readiness := c.Readiness()
	status := c.Status()
	liveness := c.Liveness()

	assert.False(t, hasCheck(readiness, "liveness-only"), "should NOT appear in readiness")
	assert.True(t, hasCheck(status, "liveness-only"), "should appear in status")
	assert.True(t, hasCheck(liveness, "liveness-only"), "should appear in liveness")
}

func TestCheckType_Both(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "both-check",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
		Type: controls.CheckTypeBoth,
	}))

	c.Start()

	assert.True(t, hasCheck(c.Readiness(), "both-check"), "should appear in readiness")
	assert.True(t, hasCheck(c.Status(), "both-check"), "should appear in status")
	assert.True(t, hasCheck(c.Liveness(), "both-check"), "should appear in liveness")
}

func TestAsyncCheck_StopsOnShutdown(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int64

	c := controls.NewController(context.Background(),
		controls.WithoutSignals(),
		controls.WithLogger(logger.NewNoop()),
	)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "shutdown-check",
		Check: func(_ context.Context) controls.CheckResult {
			callCount.Add(1)
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
		Interval: 50 * time.Millisecond,
		Type:     controls.CheckTypeReadiness,
	}))

	c.Start()

	// Wait for at least one run
	require.Eventually(t, func() bool {
		return callCount.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	c.Stop()
	c.Wait()

	// Record count after shutdown, wait, and verify it doesn't increase
	countAfterStop := callCount.Load()
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, countAfterStop, callCount.Load(), "async check should stop after shutdown")
}

func TestGetCheckResult(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "result-check",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckDegraded, Message: "warming up"}
		},
		Type: controls.CheckTypeReadiness,
	}))

	c.Start()

	// Trigger a sync check by calling Readiness
	c.Readiness()

	result, ok := c.GetCheckResult("result-check")
	assert.True(t, ok)
	assert.Equal(t, controls.CheckDegraded, result.Status)
	assert.Equal(t, "warming up", result.Message)
	assert.False(t, result.Timestamp.IsZero())
}

func TestGetCheckResult_Unknown(t *testing.T) {
	t.Parallel()

	c := newTestController(t)
	c.Start()

	_, ok := c.GetCheckResult("nonexistent")
	assert.False(t, ok)
}

func TestHealthReport_MixedServicesAndChecks(t *testing.T) {
	t.Parallel()

	c := newTestController(t)

	// Register a service
	c.Register("my-service",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithStatus(func() error { return nil }),
	)

	// Register a health check
	require.NoError(t, c.RegisterHealthCheck(controls.HealthCheck{
		Name: "my-db-check",
		Check: func(_ context.Context) controls.CheckResult {
			return controls.CheckResult{Status: controls.CheckHealthy}
		},
		Type: controls.CheckTypeReadiness,
	}))

	c.Start()

	report := c.Readiness()
	assert.True(t, report.OverallHealthy)

	serviceNames := make(map[string]bool)
	for _, s := range report.Services {
		serviceNames[s.Name] = true
	}

	assert.True(t, serviceNames["my-service"], "service should appear in report")
	assert.True(t, serviceNames["my-db-check"], "health check should appear in report")
}

func hasCheck(report controls.HealthReport, name string) bool {
	for _, s := range report.Services {
		if s.Name == name {
			return true
		}
	}
	return false
}
