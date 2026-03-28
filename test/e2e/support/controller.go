// Package support provides shared test harness helpers for E2E/BDD tests.
package support

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	pollInterval    = 10 * time.Millisecond
	cleanupTimeout  = 5 * time.Second
	listenAddress   = "127.0.0.1:0"
	defaultOptsCap  = 1
	serviceOptsCap  = 3
)

var errWaitTimeout = errors.New("timed out waiting for state")

// StateCounters tracks service lifecycle invocations atomically.
type StateCounters struct {
	Started  atomic.Int64
	Stopped  atomic.Int64
	Statused atomic.Int64
}

// SyncBuffer is a thread-safe bytes.Buffer for capturing log output.
type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// ControllerWorld holds per-scenario state for controls BDD tests.
type ControllerWorld struct {
	Controller *controls.Controller
	Ctx        context.Context
	Cancel     context.CancelFunc
	Counters   map[string]*StateCounters
	LogBuf     *SyncBuffer
	Logger     logger.Logger
	HTTPPort   int
	GRPCPort   int

	// Health check tracking
	CheckCounts map[string]*atomic.Int64

	// Channels for coordination
	RequestStarted  chan struct{}
	RequestFinished chan struct{}
	ClientResult    chan error
	StartupDelay    chan struct{}
}

// NewControllerWorld creates a fresh scenario world.
func NewControllerWorld() *ControllerWorld {
	buf := &SyncBuffer{}
	l := logger.NewCharm(buf, logger.WithLevel(logger.DebugLevel))

	ctx, cancel := context.WithCancel(context.Background())

	return &ControllerWorld{
		Ctx:         ctx,
		Cancel:      cancel,
		Counters:    make(map[string]*StateCounters),
		CheckCounts: make(map[string]*atomic.Int64),
		LogBuf:      buf,
		Logger:      l,
	}
}

// EnsureController creates the controller if it doesn't exist yet.
func (w *ControllerWorld) EnsureController(opts ...controls.ControllerOpt) {
	if w.Controller != nil {
		return
	}

	defaults := make([]controls.ControllerOpt, defaultOptsCap, defaultOptsCap+len(opts))
	defaults[0] = controls.WithLogger(w.Logger)
	opts = append(defaults, opts...)
	w.Controller = controls.NewController(w.Ctx, opts...)
}

// RegisterService registers a service with atomic counters for tracking.
func (w *ControllerWorld) RegisterService(name string, extraOpts ...controls.ServiceOption) *StateCounters {
	cntrs := &StateCounters{}
	w.Counters[name] = cntrs

	opts := make([]controls.ServiceOption, serviceOptsCap, serviceOptsCap+len(extraOpts))
	opts[0] = controls.WithStart(func(_ context.Context) error {
		cntrs.Started.Add(1)

		return nil
	})
	opts[1] = controls.WithStop(func(_ context.Context) { cntrs.Stopped.Add(1) })
	opts[2] = controls.WithStatus(func() error {
		cntrs.Statused.Add(1)

		return nil
	})
	opts = append(opts, extraOpts...)
	w.Controller.Register(name, opts...)

	return cntrs
}

// FreePort obtains a free TCP port.
func FreePort() (int, error) {
	var lc net.ListenConfig

	l, err := lc.Listen(context.Background(), "tcp", listenAddress)
	if err != nil {
		return 0, fmt.Errorf("failed to obtain free port: %w", err)
	}

	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	return port, nil
}

// WaitForState polls until the controller reaches the desired state or timeout.
func (w *ControllerWorld) WaitForState(state controls.State, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(pollInterval)

	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return errors.Wrapf(errWaitTimeout, "wanted %q, current %q", state, w.Controller.GetState())
		case <-ticker.C:
			if w.Controller.GetState() == state {
				return nil
			}
		}
	}
}

// Cleanup stops the controller if still running.
func (w *ControllerWorld) Cleanup() {
	if w.Controller != nil && !w.Controller.IsStopped() {
		w.Controller.Stop()
		_ = w.WaitForState(controls.Stopped, cleanupTimeout)
	}

	w.Cancel()
}
