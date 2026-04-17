package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/browser"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	// deletionTimeout is the timeout for HTTP deletion requests.
	deletionTimeout = 10 * time.Second
)

// DeletionRequestor sends a GDPR data deletion request for a given machine ID.
// Implementations should be best-effort — deletion cannot be guaranteed for all
// backend types.
type DeletionRequestor interface {
	RequestDeletion(ctx context.Context, machineID string) error
}

// --- HTTP deletion requestor ---

// HTTPDeletionRequestor sends a deletion request via HTTP POST.
type HTTPDeletionRequestor struct {
	endpoint string
	client   *http.Client
	log      logger.Logger
}

// NewHTTPDeletionRequestor creates a requestor that POSTs a JSON deletion
// request to the given endpoint.
func NewHTTPDeletionRequestor(endpoint string, log logger.Logger) DeletionRequestor {
	return &HTTPDeletionRequestor{
		endpoint: endpoint,
		client:   gtbhttp.NewClient(gtbhttp.WithTimeout(deletionTimeout)),
		log:      log,
	}
}

func (h *HTTPDeletionRequestor) RequestDeletion(ctx context.Context, machineID string) error {
	body, err := json.Marshal(map[string]string{"machine_id": machineID})
	if err != nil {
		return errors.Wrap(err, "marshalling deletion request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "creating deletion request")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "sending deletion request")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= httpErrorThreshold {
		h.log.Debug("deletion endpoint returned non-success status",
			"status", resp.StatusCode, "endpoint", h.endpoint)

		return errors.Newf("deletion request returned status %d", resp.StatusCode)
	}

	return nil
}

// --- Email deletion requestor ---

// EmailDeletionRequestor composes a deletion request email by opening
// the user's default mail client via a mailto: URL.
//
// Callers constructing mailto: URLs from user-influenced data must
// url.QueryEscape every parameter value. See the implementation of
// RequestDeletion for the canonical pattern.
type EmailDeletionRequestor struct {
	address  string
	toolName string

	// openURL is the function used to invoke the OS URL opener.
	// Defaults to browser.OpenURL. Tests (using the internal test
	// package) override this field to capture the generated mailto:
	// URL without launching a real mail client.
	openURL func(ctx context.Context, rawURL string) error
}

// NewEmailDeletionRequestor creates a requestor that opens a pre-filled
// mailto: link for the user to send a deletion request.
func NewEmailDeletionRequestor(address, toolName string) DeletionRequestor {
	return &EmailDeletionRequestor{
		address:  address,
		toolName: toolName,
		openURL: func(ctx context.Context, rawURL string) error {
			return browser.OpenURL(ctx, rawURL)
		},
	}
}

func (e *EmailDeletionRequestor) RequestDeletion(ctx context.Context, machineID string) error {
	subject := fmt.Sprintf("Data Deletion Request — %s", e.toolName)
	body := fmt.Sprintf("Please delete all telemetry data associated with machine ID: %s", machineID)
	mailto := fmt.Sprintf("mailto:%s?subject=%s&body=%s",
		e.address,
		url.QueryEscape(subject),
		url.QueryEscape(body))

	return e.openURL(ctx, mailto)
}

// --- Event deletion requestor ---

// EventDeletionRequestor sends a data.deletion_request event through the
// existing telemetry backend. This is the universal fallback — works with
// any backend type.
type EventDeletionRequestor struct {
	backend Backend
}

// NewEventDeletionRequestor creates a requestor that emits a deletion request
// event through the provided backend.
func NewEventDeletionRequestor(backend Backend) DeletionRequestor {
	return &EventDeletionRequestor{backend: backend}
}

func (e *EventDeletionRequestor) RequestDeletion(ctx context.Context, machineID string) error {
	event := Event{
		Timestamp: time.Now().UTC(),
		Type:      EventDeletionRequest,
		Name:      "deletion_request",
		MachineID: machineID,
	}

	return e.backend.Send(ctx, []Event{event})
}
