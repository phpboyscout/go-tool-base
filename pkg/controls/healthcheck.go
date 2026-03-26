package controls

import (
	"context"
	"sync/atomic"
	"time"
)

const defaultCheckTimeout = 5 * time.Second

// healthCheckEntry wraps a HealthCheck with its cached result and cancellation.
type healthCheckEntry struct {
	check      HealthCheck
	lastResult atomic.Pointer[CheckResult]
	cancel     context.CancelFunc
}

// runCheck executes the health check with the configured timeout and stores the result.
func (e *healthCheckEntry) runCheck(parentCtx context.Context) {
	timeout := e.check.Timeout
	if timeout == 0 {
		timeout = defaultCheckTimeout
	}

	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	result := e.check.Check(ctx)
	result.Timestamp = time.Now()
	e.lastResult.Store(&result)
}

// result returns the latest check result. For sync checks, it runs the check
// inline. For async checks, it returns the cached result.
func (e *healthCheckEntry) result(parentCtx context.Context) *CheckResult {
	if e.check.Interval > 0 {
		return e.lastResult.Load()
	}

	// Sync check — run inline
	e.runCheck(parentCtx)

	return e.lastResult.Load()
}

// toServiceStatus converts a CheckResult to a ServiceStatus for inclusion in HealthReport.
func toServiceStatus(name string, r *CheckResult) (ServiceStatus, bool) {
	if r == nil {
		return ServiceStatus{
			Name:   name,
			Status: "OK",
		}, true
	}

	s := ServiceStatus{Name: name}

	switch r.Status {
	case CheckHealthy:
		s.Status = "OK"
	case CheckDegraded:
		s.Status = "DEGRADED"
		s.Error = r.Message
	case CheckUnhealthy:
		s.Status = "ERROR"
		s.Error = r.Message
	}

	healthy := r.Status != CheckUnhealthy

	return s, healthy
}
