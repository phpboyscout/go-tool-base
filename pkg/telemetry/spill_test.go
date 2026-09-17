package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

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

func TestSplitSpillData_ChunksOversizedBuffer(t *testing.T) {
	t.Parallel()

	// Five ~300KiB events ≈ 1.5MiB > 1MiB cap → must split into >1 chunk.
	events := []Event{
		bigEvent("a"), bigEvent("b"), bigEvent("c"), bigEvent("d"), bigEvent("e"),
	}

	full, err := json.Marshal(events)
	require.NoError(t, err)
	require.Greater(t, len(full), maxSpillFileSize)

	chunks := splitSpillData(events, full)
	require.Greater(t, len(chunks), 1, "oversized buffer must split into multiple chunks")

	// Every chunk must round-trip; together they must recover all events.
	var recovered int

	for _, ch := range chunks {
		var batch []Event
		require.NoError(t, json.Unmarshal(ch, &batch))

		recovered += len(batch)
	}

	if recovered != len(events) {
		t.Errorf("recovered %d events across chunks, want %d", recovered, len(events))
	}
}

// TestSplitSpillData_SingleOversizedEvent drives the flushBatch single-event
// branch: one event already exceeds the cap and must be emitted as a lone chunk.
func TestSplitSpillData_SingleOversizedEvent(t *testing.T) {
	t.Parallel()

	giant := Event{
		Type:     telemetrytypes.EventCommandInvocation,
		Name:     "giant",
		Metadata: map[string]string{"blob": strings.Repeat("y", maxSpillFileSize*2)},
	}
	events := []Event{giant}

	full, err := json.Marshal(events)
	require.NoError(t, err)
	require.Greater(t, len(full), maxSpillFileSize)

	chunks := splitSpillData(events, full)
	require.NotEmpty(t, chunks)

	var batch []Event
	require.NoError(t, json.Unmarshal(chunks[0], &batch))
	require.Len(t, batch, 1)
}

// TestSpillToDisk_SplitsAcrossFiles drives the spillToDisk → splitSpillData →
// multi-file write path end to end through record(), then verifies all events
// are recoverable across the produced spill files.
func TestSpillToDisk_SplitsAcrossFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	c.maxBuffer = len(names)

	// Eight big events (~2.4MiB) fill the buffer and trigger a spill whose
	// serialised form exceeds the per-file cap, so multiple files are written.
	// record() replaces evt.Metadata with the merged extra map, so the large
	// payload is supplied via the extra argument (spaces keep it un-redacted).
	blob := map[string]string{"blob": strings.Repeat("x y ", 75*1024)}
	for _, n := range names {
		c.record(Event{Type: telemetrytypes.EventCommandInvocation, Name: n}, blob)
	}

	files, _ := filepath.Glob(filepath.Join(dir, spillPattern))
	require.GreaterOrEqual(t, len(files), 2, "expected the oversized spill to span multiple files")

	var total int

	for _, f := range files {
		data, readErr := os.ReadFile(f)
		require.NoError(t, readErr)

		var evts []Event
		require.NoError(t, json.Unmarshal(data, &evts))

		total += len(evts)
	}

	if total != len(names) {
		t.Errorf("recovered %d events across spill files, want %d", total, len(names))
	}
}

func TestFlushSpillFiles_RemovesCorruptFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A spill file with invalid JSON must be removed, not retried forever.
	bad := filepath.Join(dir, "telemetry-spill-1.json")
	require.NoError(t, os.WriteFile(bad, []byte("{not json"), 0o600))

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)

	require.NoError(t, c.flushSpillFiles(context.Background()))

	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Error("corrupt spill file should have been removed")
	}

	spy.mu.Lock()
	sent := spy.sendCount
	spy.mu.Unlock()

	if sent != 0 {
		t.Errorf("corrupt file should not produce a send, got %d", sent)
	}
}

// TestFlushSpillFiles_SendErrorRetainsFile proves an at-least-once spill file is
// kept when the backend send fails, so a later flush retries it.
func TestFlushSpillFiles_SendErrorRetainsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	events := []Event{{Type: telemetrytypes.EventCommandInvocation, Name: "spilled"}}
	data, err := json.Marshal(events)
	require.NoError(t, err)

	good := filepath.Join(dir, "telemetry-spill-1.json")
	require.NoError(t, os.WriteFile(good, data, 0o600))

	spy := &spyBackend{sendErr: errBackend}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)

	require.NoError(t, c.flushSpillFiles(context.Background()))

	if _, statErr := os.Stat(good); statErr != nil {
		t.Error("at-least-once spill file must be retained when the send fails")
	}
}

// TestDeleteSpillFiles_NoDataDir covers the early return when no data dir is set.
func TestDeleteSpillFiles_NoDataDir(t *testing.T) {
	t.Parallel()

	c := NewCollector(Config{Enabled: true}, &spyBackend{}, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	if err := c.deleteSpillFiles(); err != nil {
		t.Errorf("deleteSpillFiles with no data dir = %v, want nil", err)
	}
}

// TestRemoveSpillFile_MissingIsNoError proves removing a non-existent spill file
// is tolerated (os.ErrNotExist), exercising the not-exist branch.
func TestRemoveSpillFile_MissingIsNoError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := NewCollector(Config{Enabled: true}, &spyBackend{}, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)

	c.removeSpillFile(filepath.Join(dir, "telemetry-spill-does-not-exist.json"))
}

// bigEvent returns an event whose JSON encoding is large enough that a handful
// of them exceed maxSpillFileSize, forcing splitSpillData to chunk. The blob
// contains spaces so redact.String's long-opaque-token collapse does not shrink
// it when the event flows through record() → mergeMetadata().
func bigEvent(name string) Event {
	return Event{
		Type:     telemetrytypes.EventCommandInvocation,
		Name:     name,
		Metadata: map[string]string{"blob": strings.Repeat("x y ", 75*1024)},
	}
}
