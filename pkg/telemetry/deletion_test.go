package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestEmailDeletionRequestor_ConstructsSafeMailto(t *testing.T) {
	t.Parallel()

	r := NewEmailDeletionRequestor("privacy@example.com", "mytool").(*EmailDeletionRequestor)

	// Capture the URL passed to the opener instead of launching a mail client.
	var capturedURL string
	r.openURL = func(_ context.Context, rawURL string) error {
		capturedURL = rawURL
		return nil
	}

	if err := r.RequestDeletion(context.Background(), "abc-machine-123"); err != nil {
		t.Fatalf("RequestDeletion error: %v", err)
	}

	if !strings.HasPrefix(capturedURL, "mailto:privacy@example.com?") {
		t.Fatalf("mailto URL does not start with expected prefix: %q", capturedURL)
	}

	// The subject is fmt-formatted and url.QueryEscape'd. Assert the encoded
	// form is what the mail client will see and that no raw ? or & slipped
	// through outside the expected query-string positions.
	//
	// Structural invariants: exactly one literal '?' (separating the path
	// from the query), at least one '&' (separating subject from body),
	// no '\r' or '\n' anywhere, no control characters anywhere.
	questionMarks := strings.Count(capturedURL, "?")
	if questionMarks != 1 {
		t.Errorf("expected exactly 1 '?', got %d in %q", questionMarks, capturedURL)
	}

	if !strings.Contains(capturedURL, "&body=") {
		t.Errorf("expected &body= in mailto URL: %q", capturedURL)
	}

	for _, r := range capturedURL {
		if r < 0x20 || r == 0x7F {
			t.Fatalf("mailto URL contains control character %#x: %q", r, capturedURL)
		}
	}

	// The machine ID appears URL-encoded inside the body parameter.
	if !strings.Contains(capturedURL, "abc-machine-123") {
		t.Errorf("expected machine ID in mailto URL: %q", capturedURL)
	}
}

// TestEmailDeletionRequestor_CannotInjectHeaders asserts that attacker-
// controlled subject/body content containing common mailto header
// injection sequences (additional ?&=, CR, LF) is properly url.QueryEscape'd
// in the generated URL so the mail client sees exactly the intended
// Subject and Body, with no extra headers like Cc or Bcc.
//
// This is the caller-contract test required by the URL scheme validation
// spec (2026-04-02-url-scheme-validation.md). We drive the injection via
// the toolName field (which flows into the subject), since the other two
// inputs (address, machineID) are caller-supplied and under the caller's
// control in this constructor.
func TestEmailDeletionRequestor_CannotInjectHeaders(t *testing.T) {
	t.Parallel()

	injected := "InjectedTool&cc=attacker@evil.com&bcc=also@evil.com\r\nX-Header: evil"
	r := NewEmailDeletionRequestor("privacy@example.com", injected).(*EmailDeletionRequestor)

	var capturedURL string
	r.openURL = func(_ context.Context, rawURL string) error {
		capturedURL = rawURL
		return nil
	}

	if err := r.RequestDeletion(context.Background(), "id"); err != nil {
		t.Fatalf("RequestDeletion error: %v", err)
	}

	// The injected '&cc=', '&bcc=', '\r\n', 'X-Header:' sequences must
	// all be url.QueryEscape'd so the mail client sees them as literal
	// subject text, not as separate query parameters or headers.
	prohibited := []string{
		"&cc=attacker",
		"&bcc=also",
		"\r\n",
		"\r",
		"\n",
	}
	for _, p := range prohibited {
		if strings.Contains(capturedURL, p) {
			t.Errorf("captured URL contains un-escaped %q: %q", p, capturedURL)
		}
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
