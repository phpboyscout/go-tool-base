package controls

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCancelHealthChecks_InvokesStoredCancel is a white-box test proving that the
// shutdown teardown calls each entry's stored CancelFunc. The behavioural
// black-box test (TestAsyncCheck_ContextCancelledOnStop) cannot distinguish the
// explicit entry.cancel call from the parent-context cancellation that also
// propagates to the derived context, so this test asserts the wiring directly via
// a spy CancelFunc.
func TestCancelHealthChecks_InvokesStoredCancel(t *testing.T) {
	t.Parallel()

	var called atomic.Int64

	c := &Controller{
		healthChecks: map[string]*healthCheckEntry{
			"spied": {cancel: func() { called.Add(1) }},
			// A sync check (or an async one whose goroutine never launched) has a
			// nil cancel and must be skipped without panicking.
			"nil-cancel": {cancel: nil},
		},
	}

	assert.NotPanics(t, c.cancelHealthChecks)
	assert.Equal(t, int64(1), called.Load(), "stored CancelFunc must be invoked exactly once")
}
