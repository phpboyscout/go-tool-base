package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetrytypes"
)

// capturingHandler records every slog record it is given, for asserting on
// emitted log lines in tests.
type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.records = append(h.records, r.Clone())

	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// warnRecords returns the WARN-level records captured so far.
func (h *capturingHandler) warnRecords() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()

	var out []slog.Record

	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			out = append(out, r)
		}
	}

	return out
}

func TestCollector_SpillOnCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)
	c.maxBuffer = 3

	c.Track(props.EventCommandInvocation, "a", nil)
	c.Track(props.EventCommandInvocation, "b", nil)
	c.Track(props.EventCommandInvocation, "c", nil) // triggers spill

	files, _ := filepath.Glob(filepath.Join(dir, spillPattern))
	if len(files) != 1 {
		t.Fatalf("expected 1 spill file, got %d", len(files))
	}

	// Buffer should be empty after spill
	c.mu.Lock()
	bufLen := len(c.buffer)
	c.mu.Unlock()

	if bufLen != 0 {
		t.Errorf("expected empty buffer after spill, got %d", bufLen)
	}

	// Spill file should contain valid events
	data, _ := os.ReadFile(files[0])

	var events []Event
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatalf("invalid spill JSON: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events in spill, got %d", len(events))
	}
}

func TestCollector_FlushReadsSpillFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)
	c.maxBuffer = 2

	// Create a spill by filling the buffer
	c.Track(props.EventCommandInvocation, "spilled-a", nil)
	c.Track(props.EventCommandInvocation, "spilled-b", nil) // triggers spill

	// Add one more to current buffer
	c.Track(props.EventCommandInvocation, "buffered", nil)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	// Should have sent spill file events + buffered events
	if spy.sendCount != 2 { // one send for spill, one for buffer
		t.Errorf("expected 2 sends, got %d", spy.sendCount)
	}

	total := len(spy.lastEvents)
	if total != 3 {
		t.Errorf("expected 3 total events, got %d", total)
	}

	// Spill file should be cleaned up
	files, _ := filepath.Glob(filepath.Join(dir, spillPattern))
	if len(files) != 0 {
		t.Errorf("expected spill files cleaned up, got %d", len(files))
	}
}

func TestCollector_SpillPrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create maxSpillFiles + 5 fake spill files
	for i := range maxSpillFiles + 5 {
		name := filepath.Join(dir, "telemetry-spill-"+string(rune('a'+i))+".json")
		_ = os.WriteFile(name, []byte("[]"), 0o600)
	}

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)
	c.maxBuffer = 1

	// This will trigger spill which calls pruneSpillFiles
	c.Track(props.EventCommandInvocation, "trigger", nil)

	files, _ := filepath.Glob(filepath.Join(dir, spillPattern))

	// Should have pruned down to maxSpillFiles (the prune + the new one)
	if len(files) > maxSpillFiles+1 {
		t.Errorf("expected at most %d spill files after prune, got %d", maxSpillFiles+1, len(files))
	}
}

// The prune discards spill files that were never sent, so under
// DeliveryAtLeastOnce it is a bounded breach of the guarantee. It must be
// operator-visible: each prune logs at WARN with the number of files discarded.
func TestCollector_SpillPruneWarns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Accumulate exactly maxSpillFiles spill files so the next spill prunes.
	for i := range maxSpillFiles {
		name := filepath.Join(dir, "telemetry-spill-"+string(rune('a'+i))+".json")
		if err := os.WriteFile(name, []byte("[]"), 0o600); err != nil {
			t.Fatalf("seed spill file: %v", err)
		}
	}

	handler := &capturingHandler{}
	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, slog.New(handler), dir, props.DeliveryAtLeastOnce, false)
	c.maxBuffer = 1

	// Triggers a spill, which prunes the oldest file.
	c.Track(props.EventCommandInvocation, "trigger", nil)

	warns := handler.warnRecords()
	if len(warns) == 0 {
		t.Fatalf("expected a WARN log recording the spill prune, got none")
	}

	var (
		found     bool
		discarded int64
	)

	for _, r := range warns {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "discarded" {
				found = true
				discarded = a.Value.Int64()
			}

			return true
		})

		if found {
			break
		}
	}

	if !found {
		t.Fatalf("prune WARN log must record the number of discarded files")
	}

	if discarded < 1 {
		t.Errorf("expected at least 1 discarded file recorded, got %d", discarded)
	}
}

func TestCollector_DeliveryAtLeastOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write a spill file manually
	events := []Event{{Type: telemetrytypes.EventCommandInvocation, Name: "spilled"}}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	spillFile := filepath.Join(dir, "telemetry-spill-100.json")

	if err := os.WriteFile(spillFile, data, 0o600); err != nil {
		t.Fatalf("write spill: %v", err)
	}

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	// File should be deleted after successful send
	if _, err := os.Stat(spillFile); !os.IsNotExist(err) {
		t.Error("at-least-once: spill file should be deleted after successful send")
	}
}

func TestCollector_DeliveryAtMostOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	events := []Event{{Type: telemetrytypes.EventCommandInvocation, Name: "spilled"}}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	spillFile := filepath.Join(dir, "telemetry-spill-100.json")

	if err := os.WriteFile(spillFile, data, 0o600); err != nil {
		t.Fatalf("write spill: %v", err)
	}

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtMostOnce, false)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	// File should be deleted before send (at-most-once)
	if _, err := os.Stat(spillFile); !os.IsNotExist(err) {
		t.Error("at-most-once: spill file should be deleted")
	}

	// Events should still have been sent
	if spy.sendCount != 1 {
		t.Errorf("expected 1 send, got %d", spy.sendCount)
	}
}
