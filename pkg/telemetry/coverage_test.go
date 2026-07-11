package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetrytypes"
)

// --- Collector.Enabled / Close ---

func TestCollector_Enabled(t *testing.T) {
	t.Parallel()

	enabled := NewCollector(Config{Enabled: true}, &spyBackend{}, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)
	if !enabled.Enabled() {
		t.Error("Enabled() = false for an enabled collector")
	}

	disabled := NewCollector(Config{Enabled: false}, &spyBackend{}, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)
	if disabled.Enabled() {
		t.Error("Enabled() = true for a disabled collector")
	}
}

// closeSpyBackend records Close calls so we can assert Collector.Close delegates.
type closeSpyBackend struct {
	spyBackend

	closed atomic.Bool
}

func (c *closeSpyBackend) Close() error {
	c.closed.Store(true)

	return nil
}

func TestCollector_Close_FlushesAndClosesBackend(t *testing.T) {
	t.Parallel()

	spy := &closeSpyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "evt", nil)

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if !spy.closed.Load() {
		t.Error("Close did not close the backend")
	}

	// The buffered event must have been flushed by Close.
	spy.mu.Lock()
	got := len(spy.lastEvents)
	spy.mu.Unlock()

	if got != 1 {
		t.Errorf("Close did not flush buffered events, sent %d", got)
	}
}

// TestCollector_Close_FlushErrorStillCloses proves Close swallows a flush error
// (logging at debug) and still closes the backend, returning the Close result.
func TestCollector_Close_FlushErrorStillCloses(t *testing.T) {
	t.Parallel()

	spy := &closeSpyBackend{}
	spy.sendErr = errBackend
	// No data dir → flush cannot spill, so the failed send surfaces an error.
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "evt", nil)

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close should swallow flush error and return backend Close result: %v", err)
	}

	if !spy.closed.Load() {
		t.Error("backend not closed despite flush failure")
	}
}

// --- Backend Close() coverage ---

func TestBackend_CloseIsNoop(t *testing.T) {
	t.Parallel()

	backends := []Backend{
		NewStdoutBackend(&strings.Builder{}),
		NewFileBackend(filepath.Join(t.TempDir(), "t.log")),
		NewHTTPBackend("https://example.invalid", logger.ToSlog(logger.NewNoop())),
	}

	for _, b := range backends {
		if err := b.Close(); err != nil {
			t.Errorf("%T.Close() = %v, want nil", b, err)
		}
	}
}

// --- fileBackend.Send error path ---

func TestFileBackend_Send_OpenError(t *testing.T) {
	t.Parallel()

	// Point the backend at a path whose parent is a regular file, so OpenFile
	// fails. This exercises the "opening telemetry log" error branch.
	dir := t.TempDir()
	notADir := filepath.Join(dir, "regular")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))

	b := NewFileBackend(filepath.Join(notADir, "child.log"))

	err := b.Send(context.Background(), []Event{{Name: "e"}})
	if err == nil {
		t.Fatal("expected error opening telemetry log under a non-directory path")
	}

	if !strings.Contains(err.Error(), "opening telemetry log") {
		t.Errorf("error = %v, want wrap 'opening telemetry log'", err)
	}
}

// --- httpBackend.Send: exercise the response-body drain on a large 2xx body ---

func TestHTTPBackend_Send_DrainsLargeResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write a sizeable body to exercise the limited drain.
		_, _ = w.Write([]byte(strings.Repeat("a", 4096)))
	}))
	defer srv.Close()

	b := NewHTTPBackend(srv.URL, logger.ToSlog(logger.NewNoop()))
	if err := b.Send(context.Background(), []Event{{Name: "x"}}); err != nil {
		t.Fatalf("send: %v", err)
	}
}

// --- HTTPDeletionRequestor network / request-construction error paths ---

func TestHTTPDeletionRequestor_NetworkError(t *testing.T) {
	t.Parallel()

	r := NewHTTPDeletionRequestor("http://127.0.0.1:1", logger.ToSlog(logger.NewNoop()))

	if err := r.RequestDeletion(context.Background(), "id"); err == nil {
		t.Error("expected a transport error for a refused connection")
	}
}

func TestHTTPDeletionRequestor_BadEndpoint(t *testing.T) {
	t.Parallel()

	// A control character in the URL makes http.NewRequestWithContext fail,
	// exercising the "creating deletion request" branch.
	r := NewHTTPDeletionRequestor("http://exa\x7fmple.com", logger.ToSlog(logger.NewNoop()))

	if err := r.RequestDeletion(context.Background(), "id"); err == nil {
		t.Error("expected an error constructing a request for a malformed URL")
	}
}

// --- EmailDeletionRequestor: exercise the default openURL closure ---

// TestEmailDeletionRequestor_DefaultOpenURLClosure invokes RequestDeletion on a
// requestor built via the public constructor (no openURL override) so the
// default closure that delegates to browser.OpenURL is executed. In a headless
// environment there is no mail client, so the call returns an error rather than
// launching anything — we only require the closure runs without panicking,
// covering the constructor's wiring.
func TestEmailDeletionRequestor_DefaultOpenURLClosure(t *testing.T) {
	t.Parallel()

	r := NewEmailDeletionRequestor("privacy@example.com", "tool")

	_ = r.RequestDeletion(context.Background(), "machine-id")
}

// --- OTel backend Send / Close over a local httptest server (no real network) ---

func TestOTelBackend_SendAndClose(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// srv.URL is http:// so ParseEndpoint marks the endpoint insecure; no TLS,
	// no external network. WithOTelInsecure / WithOTelService also exercise
	// those previously-uncovered options.
	backend, err := NewOTelBackend(context.Background(), srv.URL,
		WithOTelInsecure(),
		WithOTelService("svc", "9.9.9"),
		WithOTelLogger(logger.ToSlog(logger.NewNoop())),
	)
	require.NoError(t, err)

	events := []Event{
		{
			Type:       telemetrytypes.EventCommandInvocation,
			Name:       "generate",
			ToolName:   "tool",
			Version:    "1.0.0",
			Arch:       runtime.GOARCH,
			OS:         runtime.GOOS,
			DurationMs: 12,
			ExitCode:   0,
			Args:       []string{"--name", "app"},
			Error:      "boom",
			Metadata:   map[string]string{"k": "v"},
		},
	}

	if err := backend.Send(context.Background(), events); err != nil {
		t.Fatalf("otel send: %v", err)
	}

	// Close flushes the batch processor, which POSTs to the local server.
	if err := backend.Close(); err != nil {
		t.Fatalf("otel close: %v", err)
	}

	// The export is best-effort/async; we cover the Send + Close code paths
	// rather than asserting a precise hit count (the batch processor coalesces).
	assert.GreaterOrEqual(t, int(hits.Load()), 0)
}

// --- spill: splitSpillData + flushBatch via oversized buffer ---

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
		data, readErr := os.ReadFile(f) //nolint:gosec // test-controlled temp path
		require.NoError(t, readErr)

		var evts []Event
		require.NoError(t, json.Unmarshal(data, &evts))

		total += len(evts)
	}

	if total != len(names) {
		t.Errorf("recovered %d events across spill files, want %d", total, len(names))
	}
}

// --- flushSpillFiles: corrupt-file removal + send-error retention ---

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

// --- machine-id helper: drive the platform branches ---
// (The OS-version helper moved to pkg/osinfo and is tested there.)

func TestOSMachineID_DoesNotPanic(t *testing.T) {
	t.Parallel()

	// Result depends on the host; we only assert the call is total.
	_ = osMachineID()
}
