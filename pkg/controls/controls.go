package controls

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	Stop   Message = "stop"
	Status Message = "status"
)

const (
	Unknown  State = "unknown"
	Running  State = "running"
	Stopping State = "stopping"
	Stopped  State = "stopped"
)

type State string
type Message string
type StartFunc func(context.Context) error
type StopFunc func(context.Context)
type StatusFunc func() error
type ProbeFunc func() error
type ValidErrorFunc func(error) bool
type ServiceOption func(*Service)

func WithStart(fn StartFunc) ServiceOption {
	return func(s *Service) {
		s.Start = fn
	}
}

func WithStop(fn StopFunc) ServiceOption {
	return func(s *Service) {
		s.Stop = fn
	}
}

func WithStatus(fn StatusFunc) ServiceOption {
	return func(s *Service) {
		s.Status = fn
	}
}

func WithLiveness(fn ProbeFunc) ServiceOption {
	return func(s *Service) {
		s.Liveness = fn
	}
}

func WithReadiness(fn ProbeFunc) ServiceOption {
	return func(s *Service) {
		s.Readiness = fn
	}
}

type ServiceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "OK", "ERROR"
	Error  string `json:"error,omitempty"`
}

type HealthReport struct {
	OverallHealthy bool            `json:"overall_healthy"`
	Services       []ServiceStatus `json:"services"`
}

type HealthMessage struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// Runner provides service lifecycle operations.
type Runner interface {
	Start()
	Stop()
	Status() HealthReport
	Liveness() HealthReport
	Readiness() HealthReport
	IsRunning() bool
	IsStopped() bool
	IsStopping() bool
	Register(id string, opts ...ServiceOption)
}

// StateAccessor provides read access to controller state and context.
type StateAccessor interface {
	GetState() State
	SetState(state State)
	GetContext() context.Context
	GetLogger() logger.Logger
}

// Configurable provides controller configuration setters.
type Configurable interface {
	SetErrorsChannel(errs chan error)
	SetMessageChannel(control chan Message)
	SetSignalsChannel(sigs chan os.Signal)
	SetHealthChannel(health chan HealthMessage)
	SetWaitGroup(wg *sync.WaitGroup)
	SetShutdownTimeout(d time.Duration)
	SetLogger(l logger.Logger)
}

// ChannelProvider provides access to controller channels.
type ChannelProvider interface {
	Messages() chan Message
	Health() chan HealthMessage
	Errors() chan error
	Signals() chan os.Signal
}

// Controllable is the full controller interface, composed of all role-based interfaces.
// Prefer using the narrower interfaces (Runner, Configurable, etc.) where possible.
type Controllable interface {
	Runner
	StateAccessor
	Configurable
	ChannelProvider
}
