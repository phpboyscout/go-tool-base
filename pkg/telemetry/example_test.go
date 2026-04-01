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

func ExampleNewFileStore() {
	// Store snapshots as local JSON files with optional AES-256-GCM encryption.
	// store, err := chat.NewFileStore(afero.NewOsFs(), "~/.mytool/conversations")
	//
	// With encryption (key must be 32 bytes):
	// store, err := chat.NewFileStore(fs, dir, chat.WithEncryption(key))
}

func ExampleNewNoopBackend() {
	// The noop backend silently discards all events.
	// Used when telemetry is disabled.
	backend := telemetry.NewNoopBackend()
	_ = backend.Send(context.Background(), nil)
}
