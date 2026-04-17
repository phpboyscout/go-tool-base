package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCollector_Disabled(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: false}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

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
	c := NewCollector(Config{Enabled: true}, spy, "mytool", "2.0.0", meta, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

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

	if e.Type != EventCommandInvocation {
		t.Errorf("type = %q, want %q", e.Type, EventCommandInvocation)
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

func TestCollector_FlushEmpty(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

	c.Track(props.EventCommandInvocation, "test", nil)

	err := c.Flush(context.Background())
	if err == nil {
		t.Error("expected error from flush")
	}
}

func TestCollector_ConcurrentTrack(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

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

func TestCollector_NoPII(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), dir, props.DeliveryAtLeastOnce, false)

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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", base, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, true)

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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, true)

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
	c := NewCollector(Config{Enabled: true}, spy, "tool", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce, false)

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

func TestEventTypeSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		propsType     props.EventType
		telemetryType EventType
	}{
		{props.EventCommandInvocation, EventCommandInvocation},
		{props.EventCommandError, EventCommandError},
		{props.EventFeatureUsed, EventFeatureUsed},
		{props.EventUpdateCheck, EventUpdateCheck},
		{props.EventUpdateApplied, EventUpdateApplied},
		{props.EventDeletionRequest, EventDeletionRequest},
	}

	for _, tt := range tests {
		if string(tt.propsType) != string(tt.telemetryType) {
			t.Errorf("props.%s = %q != telemetry.%s = %q",
				tt.propsType, tt.propsType, tt.telemetryType, tt.telemetryType)
		}
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
