package controls

import (
	"context"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	defaultInitialBackoff = 1 * time.Second
	defaultMaxBackoff     = 30 * time.Second
	defaultHealthInterval = 10 * time.Second
	backoffMultiplier     = 2.0

	// DefaultRestartResetInterval is the duration a service must run healthily
	// before its consecutive-failure restart counter resets to zero.
	DefaultRestartResetInterval = 30 * time.Second
)

// runOutcome classifies the result of a single service run for the supervisor.
type runOutcome int

const (
	// outcomeCleanStart means Start returned nil — the service either completed
	// cleanly or, more commonly, serves in a background goroutine. It is NOT an
	// exit and is never restarted; such services are supervised via health checks.
	outcomeCleanStart runOutcome = iota
	// outcomeCancelled means the run ended because the controller context was
	// cancelled (graceful shutdown). It never triggers a restart.
	outcomeCancelled
	// outcomeError means Start returned a non-nil, non-valid error while the
	// context was still live. Only this outcome may trigger a restart.
	outcomeError
)

// noopStop is the default StopFunc used when a service registers none.
func noopStop(context.Context) {}

// noopStart is the default StartFunc used when a service registers none.
func noopStart(context.Context) error { return nil }

// Services manages the collection of registered services and their lifecycle.
type Services struct {
	mu         sync.Mutex
	services   []Service
	info       sync.Map // map[string]ServiceInfo
	validError ValidErrorFunc
}

func (q *Services) add(s Service) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// D5: default missing lifecycle funcs to no-ops so the supervisor and the
	// shutdown sequence never dereference a nil func.
	if s.Start == nil {
		s.Start = noopStart
	}

	if s.Stop == nil {
		s.Stop = noopStop
	}

	q.services = append(q.services, s)
	q.info.Store(s.Name, ServiceInfo{Name: s.Name})
}

// classifyRun determines the outcome of a single Start invocation. The validError
// predicate (if set) exempts expected terminal errors (e.g. http.ErrServerClosed)
// from being treated as failures.
func (q *Services) classifyRun(ctx context.Context, err error) runOutcome {
	if err == nil {
		return outcomeCleanStart
	}

	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return outcomeCancelled
	}

	if q.validError != nil && q.validError(err) {
		return outcomeCancelled
	}

	return outcomeError
}

// monitorHealth supervises a background-serving service via its Status probe. It
// returns true if it ended because the health-failure threshold was breached (a
// restart-worthy condition), or false if it ended because the context was
// cancelled or there is nothing to monitor (in which case the service is supervised
// purely by its clean start and should simply wait for shutdown).
func (q *Services) monitorHealth(ctx context.Context, srv Service, updateInfo func(func(*ServiceInfo))) bool {
	if srv.RestartPolicy.HealthFailureThreshold <= 0 || srv.Status == nil {
		// Nothing to monitor: a clean start is not an exit, so block until the
		// controller shuts down rather than falling through to a restart.
		<-ctx.Done()

		return false
	}

	healthInterval := srv.RestartPolicy.HealthCheckInterval
	if healthInterval == 0 {
		healthInterval = defaultHealthInterval
	}

	healthFailures := 0

	for {
		select {
		case <-time.After(healthInterval):
			if err := srv.Status(); err != nil {
				healthFailures++
				if healthFailures >= srv.RestartPolicy.HealthFailureThreshold {
					srv.Stop(ctx)
					updateInfo(func(i *ServiceInfo) {
						i.Error = errors.Wrap(err, "health check failed")
					})

					return true
				}
			} else {
				healthFailures = 0 // Reset on success
			}
		case <-ctx.Done():
			return false
		}
	}
}

// sendErr forwards err on errs unless shutdown has already completed (D9). Once
// handleStopMessage closes done, the error/context handler has exited and there
// is no receiver, so an unguarded send would block the supervisor goroutine
// forever. The guard makes every forward provably non-blocking.
func sendErr(done <-chan struct{}, errs chan error, err error) {
	select {
	case errs <- err:
	case <-done:
	}
}

func (q *Services) start(ctx context.Context, wg *sync.WaitGroup, errChan chan error, done <-chan struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, s := range q.services {
		go q.supervise(ctx, s, errChan, wg, done)
	}
}

func (q *Services) supervise(ctx context.Context, srv Service, errs chan error, wg *sync.WaitGroup, done <-chan struct{}) {
	started := false

	markStarted := func() {
		if !started {
			wg.Done()

			started = true
		}
	}
	defer markStarted() // ensure wg is decremented if we exit early

	updateInfo := func(update func(*ServiceInfo)) {
		if v, ok := q.info.Load(srv.Name); ok {
			info := v.(ServiceInfo)
			update(&info)
			q.info.Store(srv.Name, info)
		}
	}

	if srv.RestartPolicy == nil {
		q.runOnce(ctx, srv, errs, updateInfo, done)

		return
	}

	q.runWithRestartPolicy(ctx, srv, errs, markStarted, updateInfo, done)
}

func (q *Services) runOnce(ctx context.Context, srv Service, errs chan error, updateInfo func(func(*ServiceInfo)), done <-chan struct{}) {
	updateInfo(func(i *ServiceInfo) { i.LastStarted = time.Now() })

	err := srv.Start(ctx)

	updateInfo(func(i *ServiceInfo) {
		i.LastStopped = time.Now()
		i.Error = err
	})

	// Only forward genuine errors; a clean start, a cancellation, or a valid
	// terminal error (e.g. http.ErrServerClosed) is not a failure.
	if q.classifyRun(ctx, err) == outcomeError {
		sendErr(done, errs, err)
	}
}

func calculateNextBackoff(current, max time.Duration) time.Duration {
	next := time.Duration(float64(current) * backoffMultiplier)
	if next > max || next < 0 {
		return max
	}

	return next
}

// restartTimings holds the resolved backoff/reset parameters for a restart loop.
type restartTimings struct {
	backoff       time.Duration
	maxBackoff    time.Duration
	resetInterval time.Duration
}

func resolveRestartTimings(p *RestartPolicy) restartTimings {
	t := restartTimings{
		backoff:       p.InitialBackoff,
		maxBackoff:    p.MaxBackoff,
		resetInterval: p.RestartResetInterval,
	}

	if t.backoff == 0 {
		t.backoff = defaultInitialBackoff
	}

	if t.maxBackoff == 0 {
		t.maxBackoff = defaultMaxBackoff
	}

	if t.resetInterval == 0 {
		t.resetInterval = DefaultRestartResetInterval
	}

	return t
}

// runOnceWithRestart performs a single supervised run. It returns the Start error
// and whether the restart loop should keep going (false means terminate: graceful
// shutdown or a clean start with no exit).
func (q *Services) runOnceWithRestart(ctx context.Context, srv Service, markStarted func(), updateInfo func(func(*ServiceInfo))) (error, bool) {
	updateInfo(func(i *ServiceInfo) { i.LastStarted = time.Now() })

	err := srv.Start(ctx)

	updateInfo(func(i *ServiceInfo) {
		i.LastStopped = time.Now()
		i.Error = err
	})

	switch q.classifyRun(ctx, err) {
	case outcomeCancelled:
		// Graceful shutdown (or an expected terminal error). Never restart.
		return err, false
	case outcomeCleanStart:
		// Start returned nil: the service serves in the background. Mark it
		// started and supervise it via its health check. monitorHealth blocks
		// until the context is cancelled (shutdown) or the health threshold is
		// breached. A clean start that is not health-failed is never an exit.
		markStarted()

		return err, q.monitorHealth(ctx, srv, updateInfo)
	default: // outcomeError
		return err, true
	}
}

func (q *Services) runWithRestartPolicy(ctx context.Context, srv Service, errs chan error, markStarted func(), updateInfo func(func(*ServiceInfo)), done <-chan struct{}) {
	restarts := 0
	timings := resolveRestartTimings(srv.RestartPolicy)

	for {
		runStarted := time.Now()

		err, keepGoing := q.runOnceWithRestart(ctx, srv, markStarted, updateInfo)
		if !keepGoing {
			return
		}

		// The run ended in a failure (Start error or health breach). Reset the
		// consecutive-failure counter if it ran healthily for long enough.
		if time.Since(runStarted) >= timings.resetInterval {
			restarts = 0
		}

		// Check if we've exhausted restarts.
		if srv.RestartPolicy.MaxRestarts > 0 && restarts >= srv.RestartPolicy.MaxRestarts {
			finalErr := errors.New("max restarts exceeded")
			if err != nil {
				finalErr = errors.Wrap(err, "max restarts exceeded")
			}

			updateInfo(func(i *ServiceInfo) { i.Error = finalErr })

			sendErr(done, errs, finalErr)

			return
		}

		restarts++

		updateInfo(func(i *ServiceInfo) { i.RestartCount = restarts })

		// Never send nil on errs (errors.Wrap(nil,...) returns nil). A health
		// failure stores its error via monitorHealth/updateInfo; only forward a
		// non-nil Start error here.
		if err != nil {
			sendErr(done, errs, err)
		}

		// Wait for backoff or cancellation.
		select {
		case <-time.After(timings.backoff):
			timings.backoff = calculateNextBackoff(timings.backoff, timings.maxBackoff)

			continue
		case <-ctx.Done():
			return
		}
	}
}

// stop shuts services down in reverse registration order, one at a time. Each
// StopFunc runs in its own goroutine and is awaited against ctx.Done(): a
// context-ignoring stop is abandoned at the shutdown deadline rather than hanging
// the caller (and Wait()) forever. The abandoned goroutine is left to finish on
// its own. Returns the number of services.
func (q *Services) stop(ctx context.Context) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := len(q.services) - 1; i >= 0; i-- {
		s := q.services[i]

		done := make(chan struct{})

		go func() {
			defer close(done)

			callStop(ctx, s.Stop)
		}()

		select {
		case <-done:
			// Stop completed within the remaining deadline.
		case <-ctx.Done():
			// Deadline reached: abandon this stop and move on to the next service.
			// Remaining stops still get a best-effort attempt, but with the
			// deadline already elapsed their own ctx.Done() fires immediately.
			// The abandoned goroutine is left to finish on its own.
		}
	}

	return len(q.services)
}

// callStop invokes a StopFunc, recovering from a panic so a misbehaving stop
// cannot crash the shutdown sequence. fn is never nil (defaulted at registration).
func callStop(ctx context.Context, fn StopFunc) {
	defer func() {
		_ = recover()
	}()

	fn(ctx)
}

// callProbe calls fn and returns any error it produces. If fn panics, the panic
// value is converted to an error so that a misbehaving StatusFunc or ProbeFunc
// cannot crash the server.
func callProbe(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.Newf("probe panicked: %v", r)
		}
	}()

	return fn()
}

func (q *Services) status() HealthReport {
	q.mu.Lock()
	defer q.mu.Unlock()

	report := HealthReport{
		OverallHealthy: true,
		Services:       make([]ServiceStatus, 0, len(q.services)),
	}

	for _, s := range q.services {
		status := ServiceStatus{
			Name:   s.Name,
			Status: "OK",
		}

		if s.Status != nil {
			if err := callProbe(s.Status); err != nil {
				status.Status = "ERROR"
				status.Error = err.Error()
				report.OverallHealthy = false
			}
		}

		report.Services = append(report.Services, status)
	}

	return report
}

func (q *Services) liveness() HealthReport {
	q.mu.Lock()
	defer q.mu.Unlock()

	report := HealthReport{
		OverallHealthy: true,
		Services:       make([]ServiceStatus, 0, len(q.services)),
	}

	for _, s := range q.services {
		status := ServiceStatus{
			Name:   s.Name,
			Status: "OK",
		}

		var err error
		if s.Liveness != nil {
			err = callProbe(s.Liveness)
		} else if s.Status != nil {
			err = callProbe(s.Status)
		}

		if err != nil {
			status.Status = "ERROR"
			status.Error = err.Error()
			report.OverallHealthy = false
		}

		report.Services = append(report.Services, status)
	}

	return report
}

func (q *Services) readiness() HealthReport {
	q.mu.Lock()
	defer q.mu.Unlock()

	report := HealthReport{
		OverallHealthy: true,
		Services:       make([]ServiceStatus, 0, len(q.services)),
	}

	for _, s := range q.services {
		status := ServiceStatus{
			Name:   s.Name,
			Status: "OK",
		}

		var err error
		if s.Readiness != nil {
			err = callProbe(s.Readiness)
		} else if s.Status != nil {
			err = callProbe(s.Status)
		}

		if err != nil {
			status.Status = "ERROR"
			status.Error = err.Error()
			report.OverallHealthy = false
		}

		report.Services = append(report.Services, status)
	}

	return report
}

// Service represents a managed background service with start/stop lifecycle,
// health probes, and optional restart policy.
type Service struct {
	Name          string
	Start         StartFunc
	Stop          StopFunc
	Status        StatusFunc
	Liveness      ProbeFunc
	Readiness     ProbeFunc
	RestartPolicy *RestartPolicy
}
