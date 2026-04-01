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

// State represents the lifecycle state of the controller (Created, Starting, Running, Stopped).
type State string

// Message represents a control message sent to the controller (e.g. "stop").
type Message string

// StartFunc is the callback invoked to start a service. It receives a context
// that is cancelled when the controller shuts down.
type StartFunc func(context.Context) error

// StopFunc is the callback invoked to stop a service gracefully. The context
// carries the shutdown timeout.
type StopFunc func(context.Context)

// StatusFunc is the callback invoked to check a service's health.
// Returns nil if healthy, an error otherwise.
type StatusFunc func() error

// ProbeFunc is a health check function for liveness or readiness probes.
type ProbeFunc func() error

// ValidErrorFunc determines whether an error from a service is expected
// (e.g. http.ErrServerClosed) and should not trigger a restart.
type ValidErrorFunc func(error) bool

// ServiceOption is a functional option for configuring a Service.
type ServiceOption func(*Service)

// WithStart sets the service's start function.
func WithStart(fn StartFunc) ServiceOption {
	return func(s *Service) {
		s.Start = fn
	}
}

// WithStop sets the service's graceful shutdown function.
func WithStop(fn StopFunc) ServiceOption {
	return func(s *Service) {
		s.Stop = fn
	}
}

// WithStatus sets the service's health check function.
func WithStatus(fn StatusFunc) ServiceOption {
	return func(s *Service) {
		s.Status = fn
	}
}

// WithLiveness sets a liveness probe for the service.
func WithLiveness(fn ProbeFunc) ServiceOption {
	return func(s *Service) {
		s.Liveness = fn
	}
}

// WithReadiness sets a readiness probe for the service.
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

// WithRestartPolicy configures automatic restart behaviour for a service.
func WithRestartPolicy(policy RestartPolicy) ServiceOption {
	return func(s *Service) {
		s.RestartPolicy = &policy
	}
}

// ServiceInfo holds runtime metadata about a registered service.
type ServiceInfo struct {
	Name         string
	RestartCount int
	LastStarted  time.Time
	LastStopped  time.Time
	Error        error
}

// ServiceStatus is the health status of a single service, used in HealthReport.
type ServiceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "OK", "ERROR"
	Error  string `json:"error,omitempty"`
}

// HealthReport is the aggregate health status across all registered services.
type HealthReport struct {
	OverallHealthy bool            `json:"overall_healthy"`
	Services       []ServiceStatus `json:"services"`
}

// HealthMessage is the JSON response body for HTTP health endpoints.
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
