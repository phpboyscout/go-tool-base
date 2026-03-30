package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestNoopBackend(t *testing.T) {
	t.Parallel()

	b := NewNoopBackend()

	if err := b.Send(context.Background(), []Event{{Name: "test"}}); err != nil {
		t.Errorf("noop send: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("noop close: %v", err)
	}
}

func TestStdoutBackend(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	b := NewStdoutBackend(&buf)

	events := []Event{
		{
			Timestamp: time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC),
			Type:      EventCommandInvocation,
			Name:      "generate",
			ToolName:  "mytool",
		},
	}

	if err := b.Send(context.Background(), events); err != nil {
		t.Fatalf("stdout send: %v", err)
	}

	var parsed []Event
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 event, got %d", len(parsed))
	}

	if parsed[0].Name != "generate" {
		t.Errorf("name = %q, want %q", parsed[0].Name, "generate")
	}
}

func TestFileBackend(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "telemetry.log")
	b := NewFileBackend(path)

	events := []Event{
		{Type: EventCommandInvocation, Name: "first"},
		{Type: EventCommandError, Name: "second"},
	}

	if err := b.Send(context.Background(), events); err != nil {
		t.Fatalf("file send: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var e1, e2 Event

	if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
		t.Fatalf("unmarshal line 0: %v", err)
	}

	if err := json.Unmarshal([]byte(lines[1]), &e2); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}

	if e1.Name != "first" {
		t.Errorf("first event name = %q", e1.Name)
	}

	if e2.Name != "second" {
		t.Errorf("second event name = %q", e2.Name)
	}
}

func TestHTTPBackend_Success(t *testing.T) {
	t.Parallel()

	var received []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b := NewHTTPBackend(srv.URL, logger.NewNoop())

	events := []Event{{Type: EventCommandInvocation, Name: "test"}}
	if err := b.Send(context.Background(), events); err != nil {
		t.Fatalf("http send: %v", err)
	}

	var parsed []Event
	if err := json.Unmarshal(received, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(parsed) != 1 || parsed[0].Name != "test" {
		t.Errorf("unexpected payload: %s", received)
	}
}

func TestHTTPBackend_Non2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	b := NewHTTPBackend(srv.URL, logger.NewNoop())

	// Should not return an error — non-2xx is logged at debug, not propagated
	err := b.Send(context.Background(), []Event{{Name: "test"}})
	if err != nil {
		t.Errorf("expected nil error for non-2xx, got %v", err)
	}
}

func TestHTTPBackend_NetworkError(t *testing.T) {
	t.Parallel()

	// Closed server — connection refused
	b := NewHTTPBackend("http://127.0.0.1:1", logger.NewNoop())

	err := b.Send(context.Background(), []Event{{Name: "test"}})
	if err != nil {
		t.Errorf("expected nil error for network failure, got %v", err)
	}
}
