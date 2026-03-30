package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestHTTPDeletionRequestor_Success(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := NewHTTPDeletionRequestor(srv.URL, logger.NewNoop())

	if err := r.RequestDeletion(context.Background(), "abc123"); err != nil {
		t.Fatalf("deletion error: %v", err)
	}

	if receivedBody["machine_id"] != "abc123" {
		t.Errorf("machine_id = %q, want %q", receivedBody["machine_id"], "abc123")
	}
}

func TestHTTPDeletionRequestor_Non2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	r := NewHTTPDeletionRequestor(srv.URL, logger.NewNoop())

	err := r.RequestDeletion(context.Background(), "abc123")
	if err == nil {
		t.Error("expected error for non-2xx deletion response")
	}
}

func TestEventDeletionRequestor(t *testing.T) {
	t.Parallel()

	spy := &spyBackend{}
	r := NewEventDeletionRequestor(spy)

	if err := r.RequestDeletion(context.Background(), "machine-xyz"); err != nil {
		t.Fatalf("deletion error: %v", err)
	}

	if spy.sendCount != 1 {
		t.Fatalf("expected 1 send, got %d", spy.sendCount)
	}

	e := spy.lastEvents[0]

	if e.Type != EventDeletionRequest {
		t.Errorf("type = %q, want %q", e.Type, EventDeletionRequest)
	}

	if e.MachineID != "machine-xyz" {
		t.Errorf("machine_id = %q, want %q", e.MachineID, "machine-xyz")
	}

	if e.Name != "deletion_request" {
		t.Errorf("name = %q, want %q", e.Name, "deletion_request")
	}
}
