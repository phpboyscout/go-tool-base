package controls_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phpboyscout/gtb/pkg/controls"

	"github.com/stretchr/testify/assert"
)

type StateCounters struct {
	Started  atomic.Int64
	Stopped  atomic.Int64
	Statused atomic.Int64
}

// syncBuffer is a thread-safe bytes.Buffer for use with slog in tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func getNewController(ctx context.Context) (*controls.Controller, *StateCounters, *syncBuffer) {
	cntrs := &StateCounters{}
	startFunc := func(_ context.Context) error { cntrs.Started.Add(1); return nil }
	stopFunc := func(_ context.Context) { cntrs.Stopped.Add(1) }
	statusFunc := func() { cntrs.Statused.Add(1); time.Sleep(500 * time.Microsecond) }

	buf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(buf, nil))

	c := controls.NewController(ctx, controls.WithLogger(logger))
	c.Register("test",
		controls.WithStart(startFunc),
		controls.WithStop(stopFunc),
		controls.WithStatus(statusFunc),
	)

	return c, cntrs, buf
}

func TestController_Controls(t *testing.T) {
	t.Run("stopping", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		assert.Equal(t, controls.Unknown, c.GetState())
		c.Start()

		assert.True(t, c.IsRunning())

		c.Stop()
		assert.Eventually(t, func() bool {
			return cntrs.Stopped.Load() == int64(1)
		}, 1*time.Second, 10*time.Millisecond)
		assert.True(t, c.IsStopped())
	})

	t.Run("status", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		c.Start()

		assert.True(t, c.IsRunning())
		c.Messages() <- controls.Status
		assert.Eventually(t, func() bool {
			return cntrs.Statused.Load() == int64(1)
		}, 1*time.Second, 10*time.Millisecond)
		assert.True(t, c.IsRunning())
	})

	t.Run("multiple status calls", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		c.Start()

		assert.True(t, c.IsRunning())
		for i := 1; i <= 3; i++ {
			c.Messages() <- controls.Status
			expected := int64(i)
			assert.Eventually(t, func() bool {
				return cntrs.Statused.Load() == expected
			}, 1*time.Second, 10*time.Millisecond)
		}
		assert.True(t, c.IsRunning())
	})

	t.Run("stop running controller", func(t *testing.T) {
		c, cntrs, _ := getNewController(context.Background())
		c.Start()

		assert.True(t, c.IsRunning())
		c.Messages() <- controls.Stop

		assert.Eventually(t, func() bool {
			return cntrs.Stopped.Load() == int64(1)
		}, 1*time.Second, 10*time.Millisecond)
		assert.True(t, c.IsStopped())
	})

}

func TestController_StartError(t *testing.T) {
	c, _, output := getNewController(context.Background())
	c.Register("test",
		controls.WithStart(func(_ context.Context) error {
			return fmt.Errorf("test error")
		}),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithStatus(func() {}),
	)

	c.Start()

	assert.Eventually(t, func() bool {
		return strings.Contains(output.String(), "test error")
	}, 1*time.Second, 10*time.Millisecond)
}

func TestController_WaitGroup(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	wg := &sync.WaitGroup{}
	c.SetWaitGroup(wg)
	wg2 := c.WaitGroup()
	assert.Equal(t, wg, wg2)
}

func TestController_SetState(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	c.SetState(controls.Running)
	assert.True(t, c.IsRunning())

	c.SetState(controls.Stopping)
	assert.True(t, c.IsStopping())

	c.SetState(controls.Stopped)
	assert.True(t, c.IsStopped())
}

func TestController_Errors(t *testing.T) {
	c, _, output := getNewController(context.Background())
	errs := make(chan error)
	c.SetErrorsChannel(errs)

	c.Start()
	c.Errors() <- fmt.Errorf("test error") //nolint:goerr113

	assert.Eventually(t, func() bool {
		return strings.Contains(output.String(), "test error")
	}, 1*time.Second, 10*time.Millisecond)
}

func TestController_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, _, _ := getNewController(ctx)
	errs := make(chan error)
	c.SetErrorsChannel(errs)

	c.Start()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	c.Wait()
	assert.True(t, c.IsStopped())

}

func TestController_SetMessageChannels(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	msgs := make(chan controls.Message)
	c.SetMessageChannel(msgs)
	assert.Equal(t, msgs, c.Messages())
}

func TestController_Health(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	health := make(chan controls.HealthMessage)
	c.SetHealthChannel(health)

	go func(t *testing.T, health chan controls.HealthMessage) {
		h := <-health
		assert.Equal(t, "testHost", h.Host)
		assert.Equal(t, 1, h.Port)
		assert.Equal(t, 2, h.Status)
		assert.Equal(t, "testMessage", h.Message)
	}(t, health)

	c.Health() <- controls.HealthMessage{
		Host:    "testHost",
		Port:    1,
		Status:  2,
		Message: "testMessage",
	}
}

func TestStop_ConcurrentCalls(t *testing.T) {
	c, cntrs, _ := getNewController(context.Background())
	c.Start()
	assert.True(t, c.IsRunning())

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Stop()
		}()
	}
	wg.Wait()

	assert.Eventually(t, func() bool {
		return c.IsStopped()
	}, 2*time.Second, 10*time.Millisecond)

	// Stop function of the service should have been called exactly once
	assert.Equal(t, int64(1), cntrs.Stopped.Load(), "stop should execute exactly once")
}

func TestStop_AlreadyStopped(t *testing.T) {
	c, _, _ := getNewController(context.Background())
	c.Start()
	assert.True(t, c.IsRunning())

	c.Stop()
	assert.Eventually(t, func() bool {
		return c.IsStopped()
	}, 2*time.Second, 10*time.Millisecond)

	// Calling Stop again should be a no-op (not panic or block)
	c.Stop()
	assert.True(t, c.IsStopped())
}
