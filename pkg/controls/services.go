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
)

type Services struct {
	mu       sync.Mutex
	services []Service
	info     sync.Map // map[string]ServiceInfo
}

func (q *Services) add(s Service) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.services = append(q.services, s)
	q.info.Store(s.Name, ServiceInfo{Name: s.Name})
}

func (q *Services) monitorHealth(ctx context.Context, srv Service, updateInfo func(func(*ServiceInfo))) {
	if srv.RestartPolicy.HealthFailureThreshold <= 0 || srv.Status == nil {
		return
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

					return
				}
			} else {
				healthFailures = 0 // Reset on success
			}
		case <-ctx.Done():
			return
		}
	}
}

func (q *Services) start(ctx context.Context, errChan chan error) {
	q.mu.Lock()

	wg := &sync.WaitGroup{}
	for _, s := range q.services {
		wg.Add(1)

		go q.supervise(ctx, s, errChan, wg)
	}

	q.mu.Unlock()
	wg.Wait()
}

func (q *Services) supervise(ctx context.Context, srv Service, errs chan error, wg *sync.WaitGroup) {
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
		q.runOnce(ctx, srv, errs, updateInfo)

		return
	}

	q.runWithRestartPolicy(ctx, srv, errs, markStarted, updateInfo)
}

func (q *Services) runOnce(ctx context.Context, srv Service, errs chan error, updateInfo func(func(*ServiceInfo))) {
	updateInfo(func(i *ServiceInfo) { i.LastStarted = time.Now() })

	err := srv.Start(ctx)

	updateInfo(func(i *ServiceInfo) {
		i.LastStopped = time.Now()
		i.Error = err
	})

	if err != nil {
		errs <- err
	}
}

func calculateNextBackoff(current, max time.Duration) time.Duration {
	next := time.Duration(float64(current) * backoffMultiplier)
	if next > max || next < 0 {
		return max
	}

	return next
}

func (q *Services) runWithRestartPolicy(ctx context.Context, srv Service, errs chan error, markStarted func(), updateInfo func(func(*ServiceInfo))) {
	restarts := 0

	backoff := srv.RestartPolicy.InitialBackoff
	if backoff == 0 {
		backoff = defaultInitialBackoff
	}

	maxBackoff := srv.RestartPolicy.MaxBackoff
	if maxBackoff == 0 {
		maxBackoff = defaultMaxBackoff
	}

	for {
		updateInfo(func(i *ServiceInfo) { i.LastStarted = time.Now() })

		err := srv.Start(ctx)

		updateInfo(func(i *ServiceInfo) {
			i.LastStopped = time.Now()
			i.Error = err
		})

		// Clean exit or successful start
		if err == nil {
			markStarted()
			q.monitorHealth(ctx, srv, updateInfo)
		} else if errors.Is(err, context.Canceled) {
			return
		}

		// Check if we've exhausted restarts
		if srv.RestartPolicy.MaxRestarts > 0 && restarts >= srv.RestartPolicy.MaxRestarts {
			finalErr := errors.Wrap(err, "max restarts exceeded")

			updateInfo(func(i *ServiceInfo) { i.Error = finalErr })

			errs <- finalErr

			return
		}

		restarts++

		updateInfo(func(i *ServiceInfo) { i.RestartCount = restarts })

		errs <- err

		// Wait for backoff or cancellation
		select {
		case <-time.After(backoff):
			backoff = calculateNextBackoff(backoff, maxBackoff)

			continue
		case <-ctx.Done():
			return
		}
	}
}

func (q *Services) stop(ctx context.Context) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, s := range q.services {
		s.Stop(ctx)
	}

	return len(q.services)
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
			if err := s.Status(); err != nil {
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
			err = s.Liveness()
		} else if s.Status != nil {
			err = s.Status()
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
			err = s.Readiness()
		} else if s.Status != nil {
			err = s.Status()
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

type Service struct {
	Name          string
	Start         StartFunc
	Stop          StopFunc
	Status        StatusFunc
	Liveness      ProbeFunc
	Readiness     ProbeFunc
	RestartPolicy *RestartPolicy
}
