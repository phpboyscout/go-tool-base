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

func (q *Services) status() {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, s := range q.services {
		s.Status()
	}
}

type Service struct {
	Name   string
	Start  StartFunc
	Stop   StopFunc
	Status StatusFunc
}
