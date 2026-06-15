package props_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// NoopCollector must satisfy the documented TelemetryCollector invariant: every
// method is callable, never panics, never errors, and Enabled() reports false so
// the telemetry flush is correctly skipped while a noop is in place.
func TestNoopCollector_SatisfiesInterfaceAndIsInert(t *testing.T) {
	t.Parallel()

	var c props.TelemetryCollector = props.NoopCollector{}

	assert.False(t, c.Enabled(), "noop collector must report disabled")
	assert.Equal(t, "noop (disabled)", c.BackendInfo())

	// None of these may panic.
	c.Track(props.EventFeatureUsed, "feature", map[string]string{"k": "v"})
	c.TrackCommand("cmd", 1, 0, nil)
	c.TrackCommandExtended("cmd", []string{"a"}, 1, 0, "err", nil)

	assert.NoError(t, c.Flush(context.Background()))
	assert.NoError(t, c.Close(context.Background()))
	assert.NoError(t, c.Drop())
}
