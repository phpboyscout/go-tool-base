package root

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	configMocks "gitlab.com/phpboyscout/go/config/configmock"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
)

// TestBuildTelemetryCollector_NilVersionDoesNotPanic proves a downstream Props
// built without a Version does not panic in PersistentPreRunE; buildTelemetry
// Collector dereferenced props.Version unguarded while sibling code nil-checks.
func TestBuildTelemetryCollector_NilVersionDoesNotPanic(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewNoop(),
		Tool: p.Tool{
			Name:     "tool",
			Features: p.SetFeatures(p.Disable(p.TelemetryCmd)),
		},
		// Version and Config intentionally left nil.
	}

	assert.NotPanics(t, func() {
		_ = buildTelemetryCollector(context.Background(), props)
	})
}

type spyTelemetryBackend struct{ closed bool }

func (s *spyTelemetryBackend) Send(_ context.Context, _ []telemetry.Event) error { return nil }

func (s *spyTelemetryBackend) Close() error {
	s.closed = true

	return nil
}

// TestFlushTelemetry_FlushesEnabledCollectorDespiteConfigKey proves that an
// enabled collector is flushed even when the raw telemetry.enabled config key
// is false — the collector may have been enabled via the TELEMETRY_ENABLED env
// var or the tool-author ForceEnabled path, and those events were being
// dropped because flush gated on the config key instead of the collector.
func TestFlushTelemetry_FlushesEnabledCollectorDespiteConfigKey(t *testing.T) {
	t.Parallel()

	spy := &spyTelemetryBackend{}
	collector := telemetry.NewCollector(telemetry.Config{Enabled: true}, spy,
		"tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", p.DeliveryAtLeastOnce, false)

	cfg := configMocks.NewMockContainable(t)
	cfg.EXPECT().GetBool("telemetry.enabled").Return(false).Maybe()

	props := &p.Props{Logger: logger.NewNoop(), Config: cfg, Collector: collector}

	flushTelemetry(props)

	assert.True(t, spy.closed, "an enabled collector must be flushed even when the config key is false")
}

// TestFlushTelemetry_SkipsDisabledCollector confirms a disabled (no-op)
// collector is not flushed.
func TestFlushTelemetry_SkipsDisabledCollector(t *testing.T) {
	t.Parallel()

	spy := &spyTelemetryBackend{}
	collector := telemetry.NewCollector(telemetry.Config{Enabled: false}, spy,
		"tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", p.DeliveryAtLeastOnce, false)

	props := &p.Props{Logger: logger.NewNoop(), Collector: collector}

	flushTelemetry(props)

	assert.False(t, spy.closed, "a disabled collector should not be flushed")
}
