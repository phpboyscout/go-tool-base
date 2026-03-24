package controls

import (
	"context"
	"sync"
)

type Services struct {
	mu       sync.Mutex
	services []Service
}

func (q *Services) add(s Service) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.services = append(q.services, s)
}

func (q *Services) start(ctx context.Context, errChan chan error) {
	q.mu.Lock()

	wg := &sync.WaitGroup{}
	for _, s := range q.services {
		wg.Add(1)

		go func(fn StartFunc, errs chan error) {
			err := fn(ctx)
			if err != nil {
				errs <- err
			}

			wg.Done()
		}(s.Start, errChan)
	}

	q.mu.Unlock()
	wg.Wait()
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
	Name      string
	Start     StartFunc
	Stop      StopFunc
	Status    StatusFunc
	Liveness  ProbeFunc
	Readiness ProbeFunc
}
