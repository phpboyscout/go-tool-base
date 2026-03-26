package controls

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// CheckStatus represents the health state of a check.
type CheckStatus int

const (
	// CheckHealthy indicates the check passed.
	CheckHealthy CheckStatus = iota
	// CheckDegraded indicates the check passed but with warnings.
	// Maps to OverallHealthy: true with Status: "DEGRADED".
	CheckDegraded
	// CheckUnhealthy indicates the check failed.
	// Maps to OverallHealthy: false with Status: "ERROR".
	CheckUnhealthy
)

// CheckResult represents the outcome of a health check.
type CheckResult struct {
	// Status is the health status.
	Status CheckStatus
	// Message provides human-readable detail about the check result.
	Message string
	// Timestamp is when this result was produced.
	Timestamp time.Time
}

// CheckType determines which health endpoint(s) a check contributes to.
type CheckType int

const (
	// CheckTypeReadiness contributes to the readiness endpoint.
	CheckTypeReadiness CheckType = iota
	// CheckTypeLiveness contributes to the liveness endpoint.
	CheckTypeLiveness
	// CheckTypeBoth contributes to both liveness and readiness endpoints.
	CheckTypeBoth
)

// HealthCheck defines a named health check function.
type HealthCheck struct {
	// Name is the unique identifier for this check.
	Name string
	// Check is the function that performs the health check.
	// It receives a context with the check's timeout applied.
	Check func(ctx context.Context) CheckResult
	// Timeout is the maximum duration for a single check execution.
	// Default: 5s.
	Timeout time.Duration
	// Interval is the polling interval for async checks.
	// Zero means synchronous (run on every health request).
	Interval time.Duration
	// Type determines which health endpoints this check feeds into.
	// Default: CheckTypeReadiness.
	Type CheckType
}

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

// RestartPolicy defines how a service should be restarted on failure.
type RestartPolicy struct {
	MaxRestarts            int
	InitialBackoff         time.Duration
	MaxBackoff             time.Duration
	HealthFailureThreshold int
	HealthCheckInterval    time.Duration
}

func WithRestartPolicy(policy RestartPolicy) ServiceOption {
	return func(s *Service) {
		s.RestartPolicy = &policy
	}
}

type ServiceInfo struct {
	Name         string
	RestartCount int
	LastStarted  time.Time
	LastStopped  time.Time
	Error        error
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
	IsRunning() bool
	IsStopped() bool
	IsStopping() bool
	Register(id string, opts ...ServiceOption)
}

// HealthReporter provides read access to service health, liveness, and readiness
// reports, and to per-service runtime information. Handlers and transports that
// only need to query health should depend on this interface rather than the full
// Controllable.
type HealthReporter interface {
	Status() HealthReport
	Liveness() HealthReport
	Readiness() HealthReport
	GetServiceInfo(name string) (ServiceInfo, bool)
}

// HealthCheckReporter extends HealthReporter with check-specific queries.
type HealthCheckReporter interface {
	HealthReporter
	// GetCheckResult returns the latest result for a named health check.
	GetCheckResult(name string) (CheckResult, bool)
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
// Prefer using the narrower interfaces (Runner, HealthReporter, Configurable, etc.) where possible.
type Controllable interface {
	Runner
	HealthReporter
	StateAccessor
	Configurable
	ChannelProvider
}
