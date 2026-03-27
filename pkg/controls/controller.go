package controls

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	errors "github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// ErrShutdown is the cause attached to the controller context when a graceful
// shutdown is initiated. Callers can distinguish a controlled stop from an
// upstream cancellation via context.Cause(ctx) == controls.ErrShutdown.
var ErrShutdown = errors.New("controller shutdown")

const DefaultShutdownTimeout = 5 * time.Second

type Controller struct {
	ctx             context.Context
	cancel          context.CancelCauseFunc
	logger          logger.Logger
	messages        chan Message
	health          chan HealthMessage
	errs            chan error
	signals         chan os.Signal
	wg              *sync.WaitGroup
	shutdownTimeout time.Duration
	state           State
	stateMutex      sync.Mutex
	services        Services
	healthChecks    map[string]*healthCheckEntry
}

func (c *Controller) GetContext() context.Context {
	return c.ctx
}

func (c *Controller) Messages() chan Message {
	return c.messages
}

func (c *Controller) SetMessageChannel(messages chan Message) {
	c.messages = messages
}

func (c *Controller) Health() chan HealthMessage {
	return c.health
}

func (c *Controller) SetHealthChannel(health chan HealthMessage) {
	c.health = health
}

func (c *Controller) Signals() chan os.Signal {
	return c.signals
}

func (c *Controller) SetSignalsChannel(signals chan os.Signal) {
	c.signals = signals
}

func (c *Controller) Errors() chan error {
	return c.errs
}

func (c *Controller) SetErrorsChannel(errs chan error) {
	c.errs = errs
}

func (c *Controller) WaitGroup() *sync.WaitGroup {
	return c.wg
}

func (c *Controller) SetWaitGroup(wg *sync.WaitGroup) {
	c.wg = wg
}

func (c *Controller) SetShutdownTimeout(d time.Duration) {
	c.shutdownTimeout = d
}

func (c *Controller) SetState(state State) {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()

	c.state = state
}

func (c *Controller) GetState() State {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()

	return c.state
}

func (c *Controller) SetLogger(l logger.Logger) {
	c.logger = l
}

func (c *Controller) GetLogger() logger.Logger {
	return c.logger
}

func (c *Controller) IsRunning() bool {
	return c.GetState() == Running
}

func (c *Controller) IsStopped() bool {
	return c.GetState() == Stopped
}

func (c *Controller) IsStopping() bool {
	return c.GetState() == Stopping
}

func (c *Controller) Register(id string, opts ...ServiceOption) {
	s := Service{
		Name: id,
	}

	for _, opt := range opts {
		opt(&s)
	}

	c.services.add(s)
	c.logger.Debug("Registered service", "service_name", id)
}

// RegisterHealthCheck adds a standalone health check to the controller.
// Must be called before Start(). The check name must be unique across
// both services and health checks.
func (c *Controller) RegisterHealthCheck(check HealthCheck) error {
	if c.GetState() != Unknown {
		return errors.New("cannot register health check after start")
	}

	if _, exists := c.healthChecks[check.Name]; exists {
		return errors.Newf("duplicate health check name: %q", check.Name)
	}

	c.healthChecks[check.Name] = &healthCheckEntry{check: check}
	c.logger.Debug("Registered health check", "name", check.Name)

	return nil
}

// GetCheckResult returns the latest result for a named health check.
func (c *Controller) GetCheckResult(name string) (CheckResult, bool) {
	entry, ok := c.healthChecks[name]
	if !ok {
		return CheckResult{}, false
	}

	r := entry.lastResult.Load()
	if r == nil {
		return CheckResult{}, false
	}

	return *r, true
}

// compareAndSetState atomically checks if the current state matches expected,
// and if so, sets it to next. Returns true if the transition occurred.
func (c *Controller) compareAndSetState(expected, next State) bool {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()

	if c.state != expected {
		return false
	}

	c.state = next

	return true
}

// Start launches all registered services. Duplicate calls while already
// running are no-ops.
func (c *Controller) Start() {
	go c.controls()

	// +1 for the controller lifecycle itself — this is only decremented
	// when handleStopMessage completes, ensuring Wait() blocks until the
	// full shutdown sequence (stop all services, set state) has finished.
	c.wg.Add(1 + len(c.services.services))

	// Set state before launching services so the signal handler
	// (running concurrently via controls()) can transition to Stopping if
	// an interrupt arrives while services are still initialising.
	c.SetState(Running)
	c.logger.Debug("Controller set to running state")

	c.services.start(c.ctx, c.wg, c.errs)
	c.startAsyncHealthChecks()
	c.logger.Debug("All services should now be running")
}

func (c *Controller) Wait() {
	c.wg.Wait()
}

// Stop initiates a graceful shutdown. Duplicate calls while already
// stopping or stopped are safely ignored.
func (c *Controller) Stop() {
	if !c.compareAndSetState(Running, Stopping) {
		c.logger.Warn("Stop called, but not in expected state, unable to continue", "current_state", c.GetState())

		return
	}

	c.messages <- Stop
}

// Controls sets the handlers for different control operations.
func (c *Controller) controls() {
	c.startSignalHandler()
	c.startErrorAndContextHandler()
	c.processControlMessages()
}

func (c *Controller) startSignalHandler() {
	// handle signals
	if c.signals != nil {
		go func() {
			sig := <-c.Signals()
			c.logger.Warn("received signal", "signal", sig)
			c.Stop()
		}()
	}
}

func (c *Controller) startErrorAndContextHandler() {
	// handle errors and context cancellation
	go func() {
		ctxCancelled := false

		for {
			select {
			case err, ok := <-c.Errors():
				if !ok {
					return // channel closed, controller stopped
				}

				if !errors.Is(err, context.Canceled) {
					c.logger.Error("control error", "error", err)
				}
			case <-c.GetContext().Done():
				if !ctxCancelled {
					ctxCancelled = true

					if !c.IsStopping() && !c.IsStopped() {
						c.logger.Debug("stopping due to context cancellation", "error", c.GetContext().Err())
						c.Stop()
					}
				}
			}
		}
	}()
}

func (c *Controller) processControlMessages() {
	// handle the control message cases
	for {
		msg := <-c.Messages()
		switch msg {
		case Stop:
			c.logger.Debug("received Stop message")
			c.handleStopMessage()
		case Status:
			c.logger.Debug("received Status message")
			_ = c.services.status()
		}
	}
}

func (c *Controller) handleStopMessage() {
	// If still Running, transition to Stopping first (handles direct channel sends).
	// If Stop() already transitioned us, this CAS is a harmless no-op.
	c.compareAndSetState(Running, Stopping)

	if c.GetState() != Stopping {
		return
	}

	c.logger.Warn("Stopping Services")

	// Cancel the controller context so all StartFuncs blocking on
	// ctx.Done() are unblocked before the shutdown timeout fires.
	c.cancel(ErrShutdown)

	// Derive the shutdown timeout from a fresh background context.
	// c.ctx is already cancelled above, so using it as a parent would
	// produce a context that is dead on arrival — causing http.Server.Shutdown
	// to fail immediately instead of draining in-flight connections.
	ctx, cancel := context.WithTimeout(context.Background(), c.shutdownTimeout)
	defer cancel()

	c.services.stop(ctx)
	c.SetState(Stopped)
	c.logger.Info("Stopped")
	c.wg.Done()
}

// startAsyncHealthChecks launches background goroutines for health checks
// that have a non-zero Interval.
func (c *Controller) startAsyncHealthChecks() {
	for _, entry := range c.healthChecks {
		if entry.check.Interval > 0 {
			c.startAsyncCheck(entry)
		}
	}
}

func (c *Controller) startAsyncCheck(entry *healthCheckEntry) {
	ctx, cancel := context.WithCancel(c.ctx)
	entry.cancel = cancel

	c.wg.Add(1)

	go func() {
		defer c.wg.Done()

		// Run immediately on start
		entry.runCheck(ctx)

		ticker := time.NewTicker(entry.check.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				entry.runCheck(ctx)
			}
		}
	}()
}

// healthCheckStatuses collects ServiceStatus entries from health checks
// matching the given filter function.
func (c *Controller) healthCheckStatuses(filter func(CheckType) bool) ([]ServiceStatus, bool) {
	var statuses []ServiceStatus

	allHealthy := true

	for _, entry := range c.healthChecks {
		if !filter(entry.check.Type) {
			continue
		}

		r := entry.result(c.ctx)
		s, healthy := toServiceStatus(entry.check.Name, r)
		statuses = append(statuses, s)

		if !healthy {
			allHealthy = false
		}
	}

	return statuses, allHealthy
}

// Status returns an aggregate health report for all registered services and health checks.
func (c *Controller) Status() HealthReport {
	report := c.services.status()
	// Status includes all check types.
	checks, healthy := c.healthCheckStatuses(func(_ CheckType) bool { return true })
	report.Services = append(report.Services, checks...)

	if !healthy {
		report.OverallHealthy = false
	}

	return report
}

// Liveness returns an aggregate liveness report for all registered services and health checks.
func (c *Controller) Liveness() HealthReport {
	report := c.services.liveness()
	checks, healthy := c.healthCheckStatuses(func(ct CheckType) bool {
		return ct == CheckTypeLiveness || ct == CheckTypeBoth
	})
	report.Services = append(report.Services, checks...)

	if !healthy {
		report.OverallHealthy = false
	}

	return report
}

// Readiness returns an aggregate readiness report for all registered services and health checks.
func (c *Controller) Readiness() HealthReport {
	report := c.services.readiness()
	checks, healthy := c.healthCheckStatuses(func(ct CheckType) bool {
		return ct == CheckTypeReadiness || ct == CheckTypeBoth
	})
	report.Services = append(report.Services, checks...)

	if !healthy {
		report.OverallHealthy = false
	}

	return report
}

// GetServiceInfo returns the runtime information and statistics for a specific service.
func (c *Controller) GetServiceInfo(name string) (ServiceInfo, bool) {
	if v, ok := c.services.info.Load(name); ok {
		return v.(ServiceInfo), true
	}

	return ServiceInfo{}, false
}

// Compile-time interface satisfaction checks.
var (
	_ Runner              = (*Controller)(nil)
	_ StateAccessor       = (*Controller)(nil)
	_ Configurable        = (*Controller)(nil)
	_ ChannelProvider     = (*Controller)(nil)
	_ Controllable        = (*Controller)(nil)
	_ HealthCheckReporter = (*Controller)(nil)
)

// ControllerOpt is a functional option for configuring a Controller.
type ControllerOpt func(Configurable)

// WithoutSignals disables OS signal handling.
func WithoutSignals() ControllerOpt {
	return func(c Configurable) {
		c.SetSignalsChannel(nil)
	}
}

// WithShutdownTimeout sets the graceful shutdown timeout.
func WithShutdownTimeout(d time.Duration) ControllerOpt {
	return func(c Configurable) {
		c.SetShutdownTimeout(d)
	}
}

// WithLogger sets the controller logger.
func WithLogger(l logger.Logger) ControllerOpt {
	return func(c Configurable) {
		c.SetLogger(l.WithPrefix("[Controller] "))
	}
}

func NewController(ctx context.Context, opts ...ControllerOpt) *Controller {
	ctx, cancel := context.WithCancelCause(ctx)

	c := &Controller{
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger.NewCharm(os.Stdout),
		messages:        make(chan Message),
		health:          make(chan HealthMessage),
		errs:            make(chan error),
		wg:              &sync.WaitGroup{},
		shutdownTimeout: DefaultShutdownTimeout,
		state:           Unknown,
		services:        Services{},
		healthChecks:    make(map[string]*healthCheckEntry),
	}

	c.SetSignalsChannel(make(chan os.Signal, 1))
	signal.Notify(c.Signals(), syscall.SIGINT, syscall.SIGTERM)

	for _, opt := range opts {
		opt(c)
	}

	return c
}
