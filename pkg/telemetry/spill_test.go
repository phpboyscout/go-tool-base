package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCollector_SpillOnCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), dir, props.DeliveryAtLeastOnce)
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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), dir, props.DeliveryAtLeastOnce)
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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), dir, props.DeliveryAtLeastOnce)
	c.maxBuffer = 1

	// This will trigger spill which calls pruneSpillFiles
	c.Track(props.EventCommandInvocation, "trigger", nil)

	files, _ := filepath.Glob(filepath.Join(dir, spillPattern))

	// Should have pruned down to maxSpillFiles (the prune + the new one)
	if len(files) > maxSpillFiles+1 {
		t.Errorf("expected at most %d spill files after prune, got %d", maxSpillFiles+1, len(files))
	}
}

func TestCollector_DeliveryAtLeastOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write a spill file manually
	events := []Event{{Type: EventCommandInvocation, Name: "spilled"}}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	spillFile := filepath.Join(dir, "telemetry-spill-100.json")

	if err := os.WriteFile(spillFile, data, 0o600); err != nil {
		t.Fatalf("write spill: %v", err)
	}

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), dir, props.DeliveryAtLeastOnce)

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

	events := []Event{{Type: EventCommandInvocation, Name: "spilled"}}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	spillFile := filepath.Join(dir, "telemetry-spill-100.json")

	if err := os.WriteFile(spillFile, data, 0o600); err != nil {
		t.Fatalf("write spill: %v", err)
	}

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), dir, props.DeliveryAtMostOnce)

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
