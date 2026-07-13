package controls

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	errors "github.com/cockroachdb/errors"
)

// ErrShutdown is the cause attached to the controller context when a graceful
// shutdown is initiated. Callers can distinguish a controlled stop from an
// upstream cancellation via context.Cause(ctx) == controls.ErrShutdown.
var ErrShutdown = errors.New("controller shutdown")

// DefaultShutdownTimeout is the time allowed for graceful shutdown before
// services are force-stopped.
const DefaultShutdownTimeout = 5 * time.Second

// Controller orchestrates the lifecycle of registered services: startup
// ordering, health monitoring, graceful shutdown, and signal handling.
type Controller struct {
	ctx             context.Context
	cancel          context.CancelCauseFunc
	logger          *slog.Logger
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
	validError      ValidErrorFunc
	// shutdownComplete is closed once handleStopMessage has finished the full
	// shutdown sequence. The error/context and signal handler goroutines watch
	// it as their exit condition so they terminate rather than spin or leak.
	shutdownComplete chan struct{}
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
	// Detach any prior OS-signal registration so a swapped-out channel does not
	// keep receiving SIGINT/SIGTERM (D6). signal.Stop is a no-op for channels
	// that were never passed to signal.Notify.
	if c.signals != nil {
		signal.Stop(c.signals)
	}

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

func (c *Controller) SetLogger(l *slog.Logger) {
	c.logger = l
}

func (c *Controller) GetLogger() *slog.Logger {
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

	// Registering after Start has already transitioned the controller away from
	// the initial Unknown state cannot supervise the service: Start has already
	// snapshotted the service count and launched the supervisor goroutines, so a
	// late registration is never started, monitored, or stopped. Mirror
	// RegisterHealthCheck's guard by surfacing the problem — but as a WARNING,
	// since Register has no error return and the supervisor spec defaults missing
	// funcs to no-ops. The service is still added (behaviour unchanged) so any
	// status query still reflects it; it simply is not supervised.
	if c.GetState() != Unknown {
		c.logger.Warn(
			"Register called after Start; service will not be supervised",
			"service_name", id,
			"current_state", c.GetState(),
		)
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

// Start launches all registered services. It is idempotent: a second call while
// already running (or stopping/stopped) returns early without double-starting
// services or double-counting the wait group (D3).
func (c *Controller) Start() {
	// CAS Unknown -> Running. Only the first caller proceeds; this also sets the
	// Running state before launching services so the signal handler (running
	// concurrently via controls()) can transition to Stopping if an interrupt
	// arrives while services are still initialising.
	if !c.compareAndSetState(Unknown, Running) {
		c.logger.Warn("Start called, but controller is not in the Unknown state; ignoring", "current_state", c.GetState())

		return
	}

	c.logger.Debug("Controller set to running state")

	// Propagate the valid-error predicate to the supervisor before any service
	// run can be classified.
	c.services.validError = c.validError

	// Snapshot the service count under the services mutex so the wait-group add
	// matches exactly the goroutines services.start will spawn.
	c.services.mu.Lock()
	serviceCount := len(c.services.services)
	c.services.mu.Unlock()

	// +1 for the controller lifecycle itself — this is only decremented
	// when handleStopMessage completes, ensuring Wait() blocks until the
	// full shutdown sequence (stop all services, set state) has finished.
	c.wg.Add(1 + serviceCount)

	// Wire up services and async health checks BEFORE launching the control
	// goroutines (D8). controls() starts the signal handler and message
	// processor, either of which can drive a shutdown that reads each async
	// check's CancelFunc via cancelHealthChecks. Recording those CancelFuncs
	// (startAsyncCheck writes entry.cancel) must therefore happen-before the
	// goroutines that read them start, or the two accesses race when a shutdown
	// lands mid-startup.
	c.services.start(c.ctx, c.wg, c.errs, c.shutdownComplete)
	c.startAsyncHealthChecks()

	go c.controls()

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
	// Handle OS signals. Only runs when a signal channel is configured.
	if c.signals == nil {
		return
	}

	go func() {
		select {
		case sig := <-c.Signals():
			c.logger.Warn("received signal", "signal", sig)
			c.Stop()
		case <-c.shutdownComplete:
			// Shutdown was driven by some other path (context cancel, direct
			// Stop). Exit rather than leak waiting for a signal that will never
			// come.
			return
		}

		// First signal initiated a graceful stop. Wait for shutdown to complete,
		// but allow a second signal to force an immediate exit of this goroutine
		// (the caller can then escalate to os.Exit if the shutdown is wedged).
		select {
		case sig := <-c.Signals():
			c.logger.Warn("received second signal, forcing handler exit", "signal", sig)
		case <-c.shutdownComplete:
		}
	}()
}

func (c *Controller) startErrorAndContextHandler() {
	// Handle errors and context cancellation. Exits once shutdown is complete so
	// it neither leaks nor busy-spins on a permanently-ready ctx.Done() case.
	go func() {
		// Local copy of the done channel; set to nil after first receipt so the
		// select case is disabled and stops firing on every iteration (the
		// busy-spin fix, D4).
		done := c.GetContext().Done()

		for {
			select {
			case err, ok := <-c.Errors():
				if !ok {
					return // channel closed, controller stopped
				}

				if !errors.Is(err, context.Canceled) {
					c.logger.Error("control error", "error", err)
				}
			case <-done:
				done = nil // disable this case; ctx.Done() is now permanently ready

				if !c.IsStopping() && !c.IsStopped() {
					c.logger.Debug("stopping due to context cancellation", "error", c.GetContext().Err())
					c.Stop()
				}
			case <-c.shutdownComplete:
				// Real exit condition: shutdown sequence finished. Drain any
				// buffered errors without blocking, then return.
				c.drainErrors()

				return
			}
		}
	}()
}

// drainErrors empties the error channel without blocking, logging any non-cancel
// errors. Used at handler shutdown so a buffered error is not silently dropped.
func (c *Controller) drainErrors() {
	for {
		select {
		case err, ok := <-c.Errors():
			if !ok {
				return
			}

			if err != nil && !errors.Is(err, context.Canceled) {
				c.logger.Error("control error", "error", err)
			}
		default:
			return
		}
	}
}

func (c *Controller) processControlMessages() {
	// Handle control messages until shutdown completes, then exit so the
	// goroutine terminates rather than blocking forever on the channel.
	for {
		select {
		case msg := <-c.Messages():
			switch msg {
			case Stop:
				c.logger.Debug("received Stop message")
				c.handleStopMessage()
			case Status:
				c.logger.Debug("received Status message")
				_ = c.services.status()
			}
		case <-c.shutdownComplete:
			return
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

	// Detach OS-signal handling at shutdown so a late signal does not land on a
	// channel no one is reading (D6).
	if c.signals != nil {
		signal.Stop(c.signals)
	}

	// Cancel the controller context so all StartFuncs blocking on
	// ctx.Done() are unblocked before the shutdown timeout fires.
	c.cancel(ErrShutdown)

	// Derive the shutdown timeout from a fresh background context.
	// c.ctx is already cancelled above, so using it as a parent would
	// produce a context that is dead on arrival — causing http.Server.Shutdown
	// to fail immediately instead of draining in-flight connections.
	ctx, cancel := context.WithTimeout(context.Background(), c.shutdownTimeout)
	defer cancel()

	// Cancel each async health-check context explicitly. Cancelling c.ctx above
	// already propagates to these derived contexts, but the per-check CancelFunc
	// must still be invoked to release its resources (the Go contract for every
	// context.WithCancel); skipping it leaks the cancellation goroutine until the
	// parent is collected.
	c.cancelHealthChecks()

	c.services.stop(ctx)
	c.SetState(Stopped)
	c.logger.Info("Stopped")

	// Signal the handler goroutines (error/context, signal, message processor)
	// that the shutdown sequence is complete so they terminate.
	close(c.shutdownComplete)

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

// cancelHealthChecks invokes each async health check's CancelFunc, releasing the
// per-check context derived in startAsyncCheck. It is best-effort and nil-safe:
// sync checks (and any entry whose async goroutine was never launched) have a nil
// cancel and are skipped.
func (c *Controller) cancelHealthChecks() {
	for _, entry := range c.healthChecks {
		if entry.cancel != nil {
			entry.cancel()
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
// matching the given filter function. When failClosed is true (readiness gating),
// an async check whose first run has not yet produced a cached result is reported
// as not-ready rather than defaulting to OK (D7).
func (c *Controller) healthCheckStatuses(filter func(CheckType) bool, failClosed bool) ([]ServiceStatus, bool) {
	var statuses []ServiceStatus

	allHealthy := true

	for _, entry := range c.healthChecks {
		if !filter(entry.check.Type) {
			continue
		}

		r := entry.result(c.ctx)
		s, healthy := toServiceStatus(entry.check.Name, r, failClosed)
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
	// Status includes all check types. Not a readiness gate, so do not fail closed.
	checks, healthy := c.healthCheckStatuses(func(_ CheckType) bool { return true }, false)
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
	}, false)
	report.Services = append(report.Services, checks...)

	if !healthy {
		report.OverallHealthy = false
	}

	return report
}

// Readiness returns an aggregate readiness report for all registered services and health checks.
func (c *Controller) Readiness() HealthReport {
	report := c.services.readiness()
	// Readiness gates traffic: fail closed when an async check has not yet run.
	checks, healthy := c.healthCheckStatuses(func(ct CheckType) bool {
		return ct == CheckTypeReadiness || ct == CheckTypeBoth
	}, true)
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
func WithLogger(l *slog.Logger) ControllerOpt {
	return func(c Configurable) {
		c.SetLogger(l.With("component", "controller"))
	}
}

// WithValidError registers a predicate that identifies expected terminal errors
// (e.g. http.ErrServerClosed, context.Canceled). The restart supervisor treats a
// matching error as a graceful end-of-run rather than a failure, so it neither
// counts toward the restart total nor is forwarded on the error channel (D7).
func WithValidError(fn ValidErrorFunc) ControllerOpt {
	return func(c Configurable) {
		if vs, ok := c.(validErrorSetter); ok {
			vs.setValidError(fn)
		}
	}
}

// validErrorSetter is satisfied by *Controller and lets WithValidError set the
// predicate through the Configurable option surface without widening the public
// Configurable interface.
type validErrorSetter interface {
	setValidError(fn ValidErrorFunc)
}

func (c *Controller) setValidError(fn ValidErrorFunc) {
	c.validError = fn
}

// NewController creates a Controller with the given context and options.
// By default, it registers SIGINT and SIGTERM handlers for graceful shutdown.
// Use WithoutSignals to disable signal handling (useful in tests).
func NewController(ctx context.Context, opts ...ControllerOpt) *Controller {
	ctx, cancel := context.WithCancelCause(ctx)

	c := &Controller{
		ctx:              ctx,
		cancel:           cancel,
		logger:           slog.New(slog.DiscardHandler),
		messages:         make(chan Message),
		health:           make(chan HealthMessage),
		errs:             make(chan error),
		signals:          make(chan os.Signal, 1),
		wg:               &sync.WaitGroup{},
		shutdownTimeout:  DefaultShutdownTimeout,
		state:            Unknown,
		services:         Services{},
		healthChecks:     make(map[string]*healthCheckEntry),
		shutdownComplete: make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Register OS-signal handling only after options are applied, and only if a
	// signal channel survives (WithoutSignals sets it to nil). This prevents an
	// orphaned signal.Notify registration that would swallow SIGINT/SIGTERM (D6).
	if c.signals != nil {
		signal.Notify(c.signals, syscall.SIGINT, syscall.SIGTERM)
	}

	return c
}
