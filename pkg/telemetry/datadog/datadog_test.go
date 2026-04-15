package datadog

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

type serverCapture struct {
	body    []byte
	headers http.Header
}

func newTestServer(t *testing.T) (*httptest.Server, *serverCapture) {
	t.Helper()

	capture := &serverCapture{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.body, _ = io.ReadAll(r.Body)
		capture.headers = r.Header
		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(srv.Close)

	return srv, capture
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
		Metadata:  map[string]string{"flag": "verbose"},
	}
}

func TestBackend_Send_Headers(t *testing.T) {

	srv, capture := newTestServer(t)
	regionEndpoints["test-headers"] = srv.URL

	b := NewBackend("test-api-key", logger.NewNoop(), WithRegion("test-headers"))

	if err := b.Send(context.Background(), []telemetry.Event{testEvent()}); err != nil {
		t.Fatalf("send: %v", err)
	}

	if capture.headers.Get("DD-API-KEY") != "test-api-key" {
		t.Errorf("DD-API-KEY = %q, want %q", capture.headers.Get("DD-API-KEY"), "test-api-key")
	}

	if capture.headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capture.headers.Get("Content-Type"))
	}
}

func TestBackend_Send_EventMapping(t *testing.T) {

	srv, capture := newTestServer(t)
	regionEndpoints["test-mapping"] = srv.URL

	b := NewBackend("key", logger.NewNoop(), WithRegion("test-mapping"))

	if err := b.Send(context.Background(), []telemetry.Event{testEvent()}); err != nil {
		t.Fatalf("send: %v", err)
	}

	var entries []datadogEntry
	if err := json.Unmarshal(capture.body, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]

	if e.Message != "command.invocation: generate" {
		t.Errorf("message = %q", e.Message)
	}

	if e.Service != "mytool" {
		t.Errorf("service = %q", e.Service)
	}

	if e.Hostname != "abc123" {
		t.Errorf("hostname = %q", e.Hostname)
	}

	if e.DDSource != "gtb" {
		t.Errorf("ddsource = %q", e.DDSource)
	}

	if e.Metadata["flag"] != "verbose" {
		t.Errorf("metadata[flag] = %q", e.Metadata["flag"])
	}
}

func TestBackend_Send_Tags(t *testing.T) {

	srv, capture := newTestServer(t)
	regionEndpoints["test-tags"] = srv.URL

	b := NewBackend("key", logger.NewNoop(), WithRegion("test-tags"))

	if err := b.Send(context.Background(), []telemetry.Event{testEvent()}); err != nil {
		t.Fatalf("send: %v", err)
	}

	var entries []datadogEntry
	if err := json.Unmarshal(capture.body, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	tags := entries[0].DDTags

	if !contains(tags, "event_type:command.invocation") {
		t.Errorf("ddtags missing event_type: %q", tags)
	}

	if !contains(tags, "tool_version:1.2.3") {
		t.Errorf("ddtags missing tool_version: %q", tags)
	}

	if !contains(tags, "os:linux") {
		t.Errorf("ddtags missing os: %q", tags)
	}

	if !contains(tags, "arch:amd64") {
		t.Errorf("ddtags missing arch: %q", tags)
	}
}

func TestBackend_Regions(t *testing.T) {

	tests := []struct {
		region   Region
		contains string
	}{
		{RegionUS1, "datadoghq.com"},
		{RegionUS3, "us3.datadoghq.com"},
		{RegionUS5, "us5.datadoghq.com"},
		{RegionEU1, "datadoghq.eu"},
		{RegionAP1, "ap1.datadoghq.com"},
		{RegionAP2, "ap2.datadoghq.com"},
		{RegionGOV, "ddog-gov.com"},
	}

	for _, tt := range tests {
		ep, ok := regionEndpoints[tt.region]
		if !ok {
			t.Errorf("region %q not in regionEndpoints", tt.region)

			continue
		}

		if !contains(ep, tt.contains) {
			t.Errorf("region %q endpoint %q does not contain %q", tt.region, ep, tt.contains)
		}
	}
}

func TestBackend_InvalidRegion(t *testing.T) {
	t.Parallel()

	b := NewBackend("key", logger.NewNoop(), WithRegion("invalid")).(*backend)

	if !contains(b.endpoint, "datadoghq.com") {
		t.Errorf("invalid region should fall back to US1, got %q", b.endpoint)
	}
}

func TestBackend_WithSource(t *testing.T) {
	t.Parallel()

	b := NewBackend("key", logger.NewNoop(), WithSource("custom-source")).(*backend)

	if b.source != "custom-source" {
		t.Errorf("source = %q, want %q", b.source, "custom-source")
	}
}

func TestBackend_Non2xx(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	regionEndpoints["test-non2xx"] = srv.URL
	b := NewBackend("key", logger.NewNoop(), WithRegion("test-non2xx"))

	err := b.Send(context.Background(), []telemetry.Event{{Name: "test"}})
	if err != nil {
		t.Errorf("expected nil for non-2xx, got %v", err)
	}
}

func TestBackend_NetworkError(t *testing.T) {

	regionEndpoints["offline"] = "http://127.0.0.1:1"
	b := NewBackend("key", logger.NewNoop(), WithRegion("offline"))

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
