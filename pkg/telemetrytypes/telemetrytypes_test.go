package telemetrytypes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetrytypes"
)

// TestEventTypeWireValues locks the on-the-wire string values: they are
// serialised into telemetry payloads and consumed by backends, so a change
// here is a breaking change to every ingest pipeline.
func TestEventTypeWireValues(t *testing.T) {
	t.Parallel()

	cases := map[telemetrytypes.EventType]string{
		telemetrytypes.EventCommandInvocation: "command.invocation",
		telemetrytypes.EventCommandError:      "command.error",
		telemetrytypes.EventFeatureUsed:       "feature.used",
		telemetrytypes.EventUpdateCheck:       "update.check",
		telemetrytypes.EventUpdateApplied:     "update.applied",
		telemetrytypes.EventDeletionRequest:   "data.deletion_request",
	}

	for got, want := range cases {
		assert.Equal(t, want, string(got))
	}
}

// TestDeliveryModeWireValues locks the delivery-mode string values, which are
// persisted in tool config and read back by the collector.
func TestDeliveryModeWireValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "at_least_once", string(telemetrytypes.DeliveryAtLeastOnce))
	assert.Equal(t, "at_most_once", string(telemetrytypes.DeliveryAtMostOnce))
}
