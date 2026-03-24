package controls

import (
	"context"
	"fmt"
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

	adding := len(c.services.services)
	c.wg.Add(adding)
	c.services.start(c.ctx, c.errs)
	c.SetState(Running)
}

func (c *Controller) Wait() {
	c.wg.Wait()
}

// Stop initiates a graceful shutdown. Duplicate calls while already
// stopping or stopped are safely ignored.
func (c *Controller) Stop() {
	if !c.compareAndSetState(Running, Stopping) {
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
			c.logger.Warn(fmt.Sprintf("Received signal: %s", sig))
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

					c.logger.Debug(fmt.Sprintf("Stopping due to context cancellation: %v", c.GetContext().Err()))

					if !c.IsStopping() && !c.IsStopped() {
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
			c.handleStopMessage()
		case Status:
			c.services.status()
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

	ctx, cancel := context.WithTimeout(c.ctx, c.shutdownTimeout)
	defer cancel()

	stopping := 0 - c.services.stop(ctx)
	c.wg.Add(stopping)
	c.SetState(Stopped)
	c.logger.Info("Stopped")
}

// Compile-time interface satisfaction checks.
var (
	_ Runner          = (*Controller)(nil)
	_ StateAccessor   = (*Controller)(nil)
	_ Configurable    = (*Controller)(nil)
	_ ChannelProvider = (*Controller)(nil)
	_ Controllable    = (*Controller)(nil)
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
		c.SetLogger(l)
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
	}

	c.SetSignalsChannel(make(chan os.Signal, 1))
	signal.Notify(c.Signals(), syscall.SIGINT, syscall.SIGTERM)

	for _, opt := range opts {
		opt(c)
	}

	return c
}
