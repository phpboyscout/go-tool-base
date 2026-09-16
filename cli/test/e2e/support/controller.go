// Package support provides shared test harness helpers for E2E/BDD tests.
package support

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"gitlab.com/phpboyscout/go/controls"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	pollInterval   = 10 * time.Millisecond
	cleanupTimeout = 5 * time.Second
	listenAddress  = "127.0.0.1:0"
	defaultOptsCap = 1
)

var errWaitTimeout = errors.New("timed out waiting for state")

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

// ControllerWorld holds per-scenario state for the transport-adapter scenarios.
type ControllerWorld struct {
	Controller *controls.Controller
	Ctx        context.Context
	Cancel     context.CancelFunc
	LogBuf     *SyncBuffer
	Logger     logger.Logger
	HTTPPort   int
	GRPCPort   int

	// RateLimitStatuses collects the HTTP status codes from a burst of requests
	// sent by the rate-limiting scenarios.
	RateLimitStatuses []int
}

// NewControllerWorld creates a fresh scenario world.
func NewControllerWorld() *ControllerWorld {
	buf := &SyncBuffer{}
	l := logger.NewCharm(buf, logger.WithLevel(logger.DebugLevel))

	ctx, cancel := context.WithCancel(context.Background())

	return &ControllerWorld{
		Ctx:    ctx,
		Cancel: cancel,
		LogBuf: buf,
		Logger: l,
	}
}

// EnsureController creates the controller if it doesn't exist yet.
func (w *ControllerWorld) EnsureController(opts ...controls.ControllerOpt) {
	if w.Controller != nil {
		return
	}

	defaults := make([]controls.ControllerOpt, defaultOptsCap, defaultOptsCap+len(opts))
	defaults[0] = controls.WithLogger(logger.ToSlog(w.Logger))
	opts = append(defaults, opts...)
	w.Controller = controls.NewController(w.Ctx, opts...)
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
