package telemetry_test

import (
	"context"
	"os"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

func ExampleNewCollector() {
	cfg := telemetry.Config{Enabled: true}
	backend := telemetry.NewStdoutBackend(os.Stdout)

	c := telemetry.NewCollector(cfg, backend, "mytool", "1.0.0",
		map[string]string{"env": "production"},
		logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "generate", nil)
	c.TrackCommand("build", 1500, 0, map[string]string{"target": "linux"})

	_ = c.Flush(context.Background())
}

func ExampleNewNoopBackend() {
	// The noop backend silently discards all events.
	// Used when telemetry is disabled.
	backend := telemetry.NewNoopBackend()
	_ = backend.Send(context.Background(), nil)
}
