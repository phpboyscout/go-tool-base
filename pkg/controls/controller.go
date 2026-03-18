package controls

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const DefaultShutdownTimeout = 5 * time.Second

type Controller struct {
	ctx             context.Context
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

func (c *Controller) SetLogger(logger *slog.Logger) {
	c.logger = logger
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

	c.services.add(s)
}

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

// Stop configured server.
func (c *Controller) Stop() {
	c.SetState(Stopping)

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
			case err := <-c.Errors():
				c.logger.Error(err.Error())
			case <-c.GetContext().Done():
				if !ctxCancelled {
					ctxCancelled = true

					c.logger.Warn("Context cancelled")
					c.Stop()
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
	if c.IsRunning() {
		c.logger.Warn("Stopping Services")
		c.SetState(Stopping)
	}

	if c.IsStopping() {
		ctx, cancel := context.WithTimeout(context.Background(), c.shutdownTimeout)
		defer cancel()

		stopping := 0 - c.services.stop(ctx)
		c.wg.Add(stopping)
		c.SetState(Stopped)
		c.logger.Info("Stopped")
	}
}

type ControllerOpt func(Controllable)

func WithoutSignals() ControllerOpt {
	return func(c Controllable) {
		c.SetSignalsChannel(nil)
	}
}

func WithShutdownTimeout(d time.Duration) ControllerOpt {
	return func(c Controllable) {
		c.SetShutdownTimeout(d)
	}
}

// Global Options.
func WithLogger(logger *slog.Logger) ControllerOpt {
	return func(c Controllable) {
		c.SetLogger(logger)
	}
}

func NewController(ctx context.Context, opts ...ControllerOpt) *Controller {
	c := &Controller{
		ctx:             ctx,
		logger:          slog.New(slog.NewTextHandler(os.Stdout, nil)),
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
