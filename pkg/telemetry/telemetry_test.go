package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetrytypes"
)

func TestCollector_Disabled(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: false}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "test", nil)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	if spy.sendCount > 0 {
		t.Error("expected no sends when disabled")
	}
}

func TestCollector_Track(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	meta := map[string]string{"env": "test"}
	c := NewCollector(Config{Enabled: true}, spy, "mytool", "2.0.0", meta, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "generate", map[string]string{"flag": "verbose"})

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	if spy.sendCount != 1 {
		t.Fatalf("expected 1 send, got %d", spy.sendCount)
	}

	if len(spy.lastEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(spy.lastEvents))
	}

	e := spy.lastEvents[0]

	if e.Type != telemetrytypes.EventCommandInvocation {
		t.Errorf("type = %q, want %q", e.Type, telemetrytypes.EventCommandInvocation)
	}

	if e.Name != "generate" {
		t.Errorf("name = %q, want %q", e.Name, "generate")
	}

	if e.ToolName != "mytool" {
		t.Errorf("tool_name = %q, want %q", e.ToolName, "mytool")
	}

	if e.Version != "2.0.0" {
		t.Errorf("version = %q, want %q", e.Version, "2.0.0")
	}

	if e.Metadata["env"] != "test" {
		t.Errorf("metadata[env] = %q, want %q", e.Metadata["env"], "test")
	}

	if e.Metadata["flag"] != "verbose" {
		t.Errorf("metadata[flag] = %q, want %q", e.Metadata["flag"], "verbose")
	}
}

func TestCollector_TrackCommand(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "mytool", "2.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.TrackCommand("build", 1234, 2, map[string]string{"flag": "v"})

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	if len(spy.lastEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(spy.lastEvents))
	}

	e := spy.lastEvents[0]
	if e.Type != telemetrytypes.EventCommandInvocation {
		t.Errorf("type = %q, want %q", e.Type, telemetrytypes.EventCommandInvocation)
	}

	if e.Name != "build" {
		t.Errorf("name = %q, want build", e.Name)
	}

	if e.DurationMs != 1234 {
		t.Errorf("duration = %d, want 1234", e.DurationMs)
	}

	if e.ExitCode != 2 {
		t.Errorf("exit_code = %d, want 2", e.ExitCode)
	}

	// Common envelope stamped by record().
	if e.ToolName != "mytool" || e.Version != "2.0.0" {
		t.Errorf("envelope not stamped: tool=%q version=%q", e.ToolName, e.Version)
	}

	if e.Metadata["flag"] != "v" {
		t.Errorf("metadata[flag] = %q, want v", e.Metadata["flag"])
	}
}

func TestCollector_RedactsMetadataValues(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	// A credential supplied via collector metadata must be scrubbed at ingest,
	// just like args/errMsg are — metadata values were shipped verbatim.
	meta := map[string]string{"base_url": "https://user:" + "s3cret@api.example.co/v1"}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", meta, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "op", map[string]string{"token_arg": "token=" + "glpat-abcdef1234567890abcdef"})

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	e := spy.lastEvents[0]
	if strings.Contains(e.Metadata["base_url"], "s3cret") {
		t.Errorf("collector metadata not redacted: %q", e.Metadata["base_url"])
	}

	if strings.Contains(e.Metadata["token_arg"], "glpat-abcdef1234567890abcdef") {
		t.Errorf("extra metadata not redacted: %q", e.Metadata["token_arg"])
	}
}

func TestCollector_FlushEmpty(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	if spy.sendCount != 0 {
		t.Errorf("expected 0 sends for empty flush, got %d", spy.sendCount)
	}
}

func TestCollector_FlushError(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{sendErr: errBackend}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "test", nil)

	err := c.Flush(context.Background())
	if err == nil {
		t.Error("expected error from flush")
	}
}

// TestCollector_FlushFailureSpillsForRetry proves the at-least-once guarantee:
// when the backend's Send surfaces a transport failure, the in-memory batch is
// not lost but spilled to disk so a later flush against a healthy backend
// re-delivers it. This is the behaviour that the previously error-swallowing
// HTTP backend defeated.
func TestCollector_FlushFailureSpillsForRetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spy := &spyBackend{sendErr: errBackend}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "test", nil)

	// First flush fails at the backend; the batch must be spilled, not dropped.
	if err := c.Flush(context.Background()); err == nil {
		t.Fatal("expected error from failing flush")
	}

	files, _ := filepath.Glob(filepath.Join(dir, spillPattern))
	if len(files) == 0 {
		t.Fatal("expected a spill file after a failed at-least-once flush; batch was dropped")
	}

	// Recover the backend and flush again: the spilled batch must be delivered.
	spy.mu.Lock()
	spy.sendErr = nil
	spy.lastEvents = nil
	spy.mu.Unlock()

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("retry flush should succeed: %v", err)
	}

	spy.mu.Lock()
	delivered := len(spy.lastEvents)
	spy.mu.Unlock()

	if delivered == 0 {
		t.Error("expected the spilled batch to be re-delivered on retry")
	}

	remaining, _ := filepath.Glob(filepath.Join(dir, spillPattern))
	if len(remaining) != 0 {
		t.Errorf("spill files should be removed after successful delivery, got %d", len(remaining))
	}
}

func TestCollector_ConcurrentTrack(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	var wg sync.WaitGroup

	for range 100 {
		wg.Add(1)

		go func() {
			defer wg.Done()
			c.Track(props.EventCommandInvocation, "concurrent", nil)
		}()
	}

	wg.Wait()

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	if len(spy.lastEvents) != 100 {
		t.Errorf("expected 100 events, got %d", len(spy.lastEvents))
	}
}

// TestCollector_BackendInfo proves the getter round-trips the setter and that
// the default for an enabled collector (never set) is the empty string.
func TestCollector_BackendInfo(t *testing.T) {
	t.Parallel()

	c := NewCollector(Config{Enabled: true}, &spyBackend{}, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	assert := func(got, want string) {
		t.Helper()

		if got != want {
			t.Errorf("BackendInfo = %q, want %q", got, want)
		}
	}

	assert(c.BackendInfo(), "")

	c.SetBackendInfo("http (https://collector)")
	assert(c.BackendInfo(), "http (https://collector)")

	// A disabled collector reports the noop description.
	d := NewCollector(Config{Enabled: false}, &spyBackend{}, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)
	assert(d.BackendInfo(), "noop (disabled)")
}

// TestCollector_BackendInfoConcurrent must be race-clean under -race: concurrent
// readers and writers exercise the atomic guard on backendInfo, honouring the
// documented concurrency contract.
func TestCollector_BackendInfoConcurrent(t *testing.T) {
	t.Parallel()

	c := NewCollector(Config{Enabled: true}, &spyBackend{}, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(2)

		go func() {
			defer wg.Done()
			c.SetBackendInfo("backend-" + string(rune('a'+i%26)))
		}()

		go func() {
			defer wg.Done()
			_ = c.BackendInfo()
		}()
	}

	wg.Wait()
}

func TestCollector_NoPII(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "test", nil)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	e := spy.lastEvents[0]
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := string(data)

	hostname, _ := os.Hostname()
	if hostname != "" && len(hostname) > 2 {
		if contains(raw, hostname) {
			t.Errorf("event JSON contains raw hostname %q", hostname)
		}
	}
}

func TestCollector_Drop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), dir, props.DeliveryAtLeastOnce, false)

	// Track some events and spill
	c.maxBuffer = 2
	c.Track(props.EventCommandInvocation, "a", nil)
	c.Track(props.EventCommandInvocation, "b", nil)
	c.Track(props.EventCommandInvocation, "c", nil) // triggers spill at 2, then "c" in buffer

	// Verify spill file exists
	files, _ := filepath.Glob(filepath.Join(dir, spillPattern))
	if len(files) == 0 {
		t.Fatal("expected spill file to exist")
	}

	// Drop
	if err := c.Drop(); err != nil {
		t.Fatalf("drop error: %v", err)
	}

	// Buffer should be empty
	c.mu.Lock()
	bufLen := len(c.buffer)
	c.mu.Unlock()

	if bufLen != 0 {
		t.Errorf("expected empty buffer after drop, got %d", bufLen)
	}

	// Spill files should be deleted
	files, _ = filepath.Glob(filepath.Join(dir, spillPattern))
	if len(files) != 0 {
		t.Errorf("expected no spill files after drop, got %d", len(files))
	}
}

func TestCollector_MetadataExtraOverridesBase(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	base := map[string]string{"key": "base"}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", base, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "test", map[string]string{"key": "override"})

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	if spy.lastEvents[0].Metadata["key"] != "override" {
		t.Errorf("extra should override base metadata, got %q", spy.lastEvents[0].Metadata["key"])
	}
}

func TestTrackCommandExtended_Enabled(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, true)

	c.TrackCommandExtended("generate", []string{"--name", "myapp"}, 500, 1, "missing template", nil)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	e := spy.lastEvents[0]

	if len(e.Args) != 2 || e.Args[0] != "--name" {
		t.Errorf("args = %v, want [--name myapp]", e.Args)
	}

	if e.Error != "missing template" {
		t.Errorf("error = %q, want %q", e.Error, "missing template")
	}

	if e.DurationMs != 500 {
		t.Errorf("duration = %d, want 500", e.DurationMs)
	}

	if e.ExitCode != 1 {
		t.Errorf("exit_code = %d, want 1", e.ExitCode)
	}
}

func TestTrackCommandExtended_RedactsSensitiveContent(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, true)

	// args carries a credential as a flag value; errMsg quotes a URL
	// with userinfo. Both must be scrubbed before shipping to the
	// backend — closes M-5 from the 2026-04-17 security audit.
	// Inputs are concatenated so gosec does not flag the literals —
	// this is test data for the redactor, not an embedded credential.
	args := []string{"--api-token=sk-" + "abc123def456ghi789jkl012mno345pqr678", "--name", "myapp"}
	errMsg := "failed POST https://user:" + "hunter2@api.example.co/v1: 401"

	c.TrackCommandExtended("generate", args, 500, 1, errMsg, nil)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	e := spy.lastEvents[0]

	joinedArgs := strings.Join(e.Args, " ")
	if strings.Contains(joinedArgs, "sk-abc123def456ghi789jkl012mno345pqr678") {
		t.Errorf("args leaked credential: %v", e.Args)
	}

	if strings.Contains(e.Error, "hunter2") {
		t.Errorf("errMsg leaked password: %q", e.Error)
	}

	if strings.Contains(e.Error, "user:hunter2") {
		t.Errorf("errMsg leaked userinfo: %q", e.Error)
	}

	// Non-sensitive args must survive unchanged.
	if !strings.Contains(joinedArgs, "--name") || !strings.Contains(joinedArgs, "myapp") {
		t.Errorf("non-sensitive args mangled: %v", e.Args)
	}
}

func TestTrackCommandExtended_Disabled(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	c.TrackCommandExtended("generate", []string{"--name", "myapp"}, 500, 1, "missing template", nil)

	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	e := spy.lastEvents[0]

	if len(e.Args) != 0 {
		t.Errorf("args should be empty when extended collection disabled, got %v", e.Args)
	}

	if e.Error != "" {
		t.Errorf("error should be empty when extended collection disabled, got %q", e.Error)
	}

	// Duration and exit code should still be present
	if e.DurationMs != 500 {
		t.Errorf("duration = %d, want 500", e.DurationMs)
	}

	if e.ExitCode != 1 {
		t.Errorf("exit_code = %d, want 1", e.ExitCode)
	}
}

// --- helpers ---

var errBackend = errors.New("backend error")

type spyBackend struct {
	sendCount  int
	lastEvents []Event
	sendErr    error
	mu         sync.Mutex
}

func (s *spyBackend) Send(_ context.Context, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sendCount++
	s.lastEvents = append(s.lastEvents, events...)

	return s.sendErr
}

func (s *spyBackend) Close() error { return nil }

func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) > 0 && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}

// TestCollector_DisabledDoesNotBuffer proves a disabled collector is a true
// no-op: it must not allocate and append events (the noop backend is non-nil,
// so the backend==nil guard did not catch the disabled case).
func TestCollector_DisabledDoesNotBuffer(t *testing.T) {
	t.Parallel()

	c := NewCollector(Config{Enabled: false}, NewNoopBackend(), "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)

	for range 50 {
		c.Track(props.EventFeatureUsed, "evt", nil)
	}

	c.mu.Lock()
	n := len(c.buffer)
	c.mu.Unlock()

	if n != 0 {
		t.Errorf("a disabled collector must not buffer events, got %d buffered", n)
	}
}

// TestCollector_BufferBoundedWhenSpillUnavailable proves the buffer stays
// bounded when spill cannot run (no data dir): the spill path used to early-
// return without clearing the buffer, so it grew without bound.
func TestCollector_BufferBoundedWhenSpillUnavailable(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil,
		logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false) // empty dataDir → spill unavailable
	c.maxBuffer = 10

	for range 100 {
		c.Track(props.EventFeatureUsed, "evt", nil)
	}

	c.mu.Lock()
	n := len(c.buffer)
	c.mu.Unlock()

	if n > c.maxBuffer+1 {
		t.Errorf("buffer must stay bounded when spill is unavailable, got %d (max %d)", n, c.maxBuffer)
	}
}
