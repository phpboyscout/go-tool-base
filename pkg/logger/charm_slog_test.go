package logger_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestNewCharmHandler_EmitsThroughCharm(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := logger.NewCharmHandler(&buf)
	require.NotNil(t, handler)

	slog.New(handler).Info("hello", "key", "value")

	out := buf.String()
	assert.Contains(t, out, "hello")
	assert.Contains(t, out, "key")
	assert.Contains(t, out, "value")
}

func TestNewCharmSlog_ReturnsUsableLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.NewCharmSlog(&buf)
	require.NotNil(t, log)

	log.Warn("warned")
	assert.Contains(t, buf.String(), "warned")
}

func TestNewCharmSlog_AppliesConfigOptions(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := logger.Config{Level: "debug", Format: "json", Timestamp: false}
	log := logger.NewCharmSlog(&buf, cfg.CharmOptions()...)

	log.Debug("debugged", "n", 1)

	out := buf.String()
	assert.Contains(t, out, "debugged")
	assert.Contains(t, out, `"n":1`) // json formatter honoured
}

func TestNewLevelGate_ControlsLevelAtRuntime(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)

	// Build the base handler permissively so the gate is authoritative.
	base := logger.NewCharmHandler(&buf, logger.WithLevel(logger.DebugLevel))
	log := slog.New(logger.NewLevelGate(base, level))

	log.Debug("suppressed")
	assert.NotContains(t, buf.String(), "suppressed", "debug must be filtered at info level")

	level.Set(slog.LevelDebug)
	log.Debug("now-visible")
	assert.Contains(t, buf.String(), "now-visible", "debug must pass after lowering the gate")
}

func TestCaptureHandler_RecordsRecords(t *testing.T) {
	t.Parallel()

	capture := logger.NewCaptureHandler()
	log := slog.New(capture)

	log.Info("first", "a", 1)
	log.Error("second")

	assert.Equal(t, []string{"first", "second"}, capture.Messages())
	assert.True(t, capture.Contains("seco"))
	assert.Len(t, capture.Records(), 2)
}

func TestCaptureHandler_SharesStoreAcrossDerivedHandlers(t *testing.T) {
	t.Parallel()

	capture := logger.NewCaptureHandler()
	base := slog.New(capture)
	derived := base.With("request_id", "abc").WithGroup("http")

	base.Info("base-msg")
	derived.Info("derived-msg")

	// Both loggers land records in the one shared store.
	assert.ElementsMatch(t, []string{"base-msg", "derived-msg"}, capture.Messages())

	// The WithAttrs attribute is captured on the derived record.
	var found bool
	for _, record := range capture.Records() {
		if record.Message != "derived-msg" {
			continue
		}
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "request_id" && attr.Value.String() == "abc" {
				found = true
			}
			return true
		})
	}
	assert.True(t, found, "derived record must carry the WithAttrs attribute")
}

func TestCaptureHandler_RespectsLevel(t *testing.T) {
	t.Parallel()

	capture := logger.NewCaptureHandler()
	assert.True(t, capture.Enabled(context.Background(), slog.LevelDebug))

	log := slog.New(capture)
	log.Log(context.Background(), slog.LevelDebug, "dbg")
	assert.True(t, capture.Contains("dbg"))
}
