package posthog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

func newTestServer(t *testing.T) (*httptest.Server, *[]byte) {
	t.Helper()

	var received []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(srv.Close)

	return srv, &received
}

func testEvent() telemetry.Event {
	return telemetry.Event{
		Timestamp: time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC),
		Type:      telemetry.EventCommandInvocation,
		Name:      "generate",
		MachineID: "abc123",
		ToolName:  "mytool",
		Version:   "1.2.3",
		OS:        "linux",
		Arch:      "amd64",
		Metadata:  map[string]string{"env": "dev"},
	}
}

func TestBackend_Send_Payload(t *testing.T) {
	t.Parallel()

	srv, received := newTestServer(t)
	b := NewBackend("phc_test123", logger.NewNoop(), WithEndpoint(srv.URL))

	if err := b.Send(context.Background(), []telemetry.Event{testEvent()}); err != nil {
		t.Fatalf("send: %v", err)
	}

	var payload struct {
		ProjectToken string         `json:"api_key"`
		Batch        []posthogEvent `json:"batch"`
	}
	if err := json.Unmarshal(*received, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if payload.ProjectToken != "phc_test123" {
		t.Errorf("api_key = %q, want %q", payload.ProjectToken, "phc_test123")
	}

	if len(payload.Batch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(payload.Batch))
	}
}

func TestBackend_Send_EventMapping(t *testing.T) {
	t.Parallel()

	srv, received := newTestServer(t)
	b := NewBackend("key", logger.NewNoop(), WithEndpoint(srv.URL))

	if err := b.Send(context.Background(), []telemetry.Event{testEvent()}); err != nil {
		t.Fatalf("send: %v", err)
	}

	var payload struct {
		ProjectToken string         `json:"api_key"`
		Batch        []posthogEvent `json:"batch"`
	}
	if err := json.Unmarshal(*received, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	e := payload.Batch[0]

	if e.Event != "command.invocation" {
		t.Errorf("event = %q", e.Event)
	}

	if e.DistinctID != "abc123" {
		t.Errorf("distinct_id = %q", e.DistinctID)
	}

	if e.Properties["event_name"] != "generate" {
		t.Errorf("properties.event_name = %q", e.Properties["event_name"])
	}

	if e.Properties["tool_name"] != "mytool" {
		t.Errorf("properties.tool_name = %q", e.Properties["tool_name"])
	}

	if e.Properties["tool_version"] != "1.2.3" {
		t.Errorf("properties.tool_version = %q", e.Properties["tool_version"])
	}

	if e.Properties["env"] != "dev" {
		t.Errorf("properties.env = %q (metadata merge)", e.Properties["env"])
	}
}

func TestBackend_Send_OsProperty(t *testing.T) {
	t.Parallel()

	srv, received := newTestServer(t)
	b := NewBackend("key", logger.NewNoop(), WithEndpoint(srv.URL))

	if err := b.Send(context.Background(), []telemetry.Event{{OS: "darwin"}}); err != nil {
		t.Fatalf("send: %v", err)
	}

	var payload struct {
		ProjectToken string         `json:"api_key"`
		Batch        []posthogEvent `json:"batch"`
	}
	if err := json.Unmarshal(*received, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if payload.Batch[0].Properties["$os"] != "darwin" {
		t.Errorf("$os = %q, want darwin", payload.Batch[0].Properties["$os"])
	}
}

func TestBackend_Instances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		instance Instance
		contains string
	}{
		{InstanceUS, "us.i.posthog.com"},
		{InstanceEU, "eu.i.posthog.com"},
	}

	for _, tt := range tests {
		ep, ok := instanceEndpoints[tt.instance]
		if !ok {
			t.Errorf("instance %q not in instanceEndpoints", tt.instance)

			continue
		}

		if !contains(ep, tt.contains) {
			t.Errorf("instance %q endpoint %q does not contain %q", tt.instance, ep, tt.contains)
		}
	}
}

func TestBackend_InvalidInstance(t *testing.T) {
	t.Parallel()

	b := NewBackend("key", logger.NewNoop(), WithInstance("invalid")).(*backend)

	if !contains(b.endpoint, "us.i.posthog.com") {
		t.Errorf("invalid instance should fall back to US, got %q", b.endpoint)
	}
}

func TestBackend_CustomEndpoint(t *testing.T) {
	t.Parallel()

	b := NewBackend("key", logger.NewNoop(),
		WithInstance(InstanceEU),
		WithEndpoint("https://posthog.internal.example.com/capture/"),
	).(*backend)

	if b.endpoint != "https://posthog.internal.example.com/capture/" {
		t.Errorf("custom endpoint should override instance, got %q", b.endpoint)
	}
}

func TestBackend_Non2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	b := NewBackend("key", logger.NewNoop(), WithEndpoint(srv.URL))

	err := b.Send(context.Background(), []telemetry.Event{{Name: "test"}})
	if err != nil {
		t.Errorf("expected nil for non-2xx, got %v", err)
	}
}

func TestBackend_NetworkError(t *testing.T) {
	t.Parallel()

	b := NewBackend("key", logger.NewNoop(), WithEndpoint("http://127.0.0.1:1"))

	err := b.Send(context.Background(), []telemetry.Event{{Name: "test"}})
	if err != nil {
		t.Errorf("expected nil for network error, got %v", err)
	}
}

func TestBackend_Close(t *testing.T) {
	t.Parallel()

	b := NewBackend("key", logger.NewNoop())

	if err := b.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && indexof(s, substr) >= 0
}

func indexof(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}
