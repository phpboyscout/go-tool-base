package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"

	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	// filePermissions is the permission mode for telemetry data files.
	filePermissions = 0o600
	// httpTimeout is the timeout for telemetry HTTP requests.
	httpTimeout = 5 * time.Second
	// httpErrorThreshold is the HTTP status code threshold for error logging.
	httpErrorThreshold = 400
)

// Backend is the interface for telemetry data sinks.
// Implementations must be safe for concurrent use.
// Send must be non-blocking or short-timeout to avoid impacting CLI performance.
type Backend interface {
	Send(ctx context.Context, events []Event) error
	Close() error
}

// --- Noop backend ---

type noopBackend struct{}

// NewNoopBackend returns a backend that silently discards all events.
func NewNoopBackend() Backend                                  { return &noopBackend{} }
func (n *noopBackend) Send(_ context.Context, _ []Event) error { return nil }
func (n *noopBackend) Close() error                            { return nil }

// --- Stdout backend (debugging) ---

type stdoutBackend struct{ w io.Writer }

// NewStdoutBackend returns a backend that writes events as pretty-printed JSON.
func NewStdoutBackend(w io.Writer) Backend { return &stdoutBackend{w: w} }

func (s *stdoutBackend) Send(_ context.Context, events []Event) error {
	enc := json.NewEncoder(s.w)
	enc.SetIndent("", "  ")

	return enc.Encode(events)
}

func (s *stdoutBackend) Close() error { return nil }

// --- File backend (local-only mode) ---

type fileBackend struct {
	path string
	mu   sync.Mutex
}

// NewFileBackend returns a backend that appends events as newline-delimited JSON.
func NewFileBackend(path string) Backend { return &fileBackend{path: path} }

func (f *fileBackend) Send(_ context.Context, events []Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	file, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePermissions)
	if err != nil {
		return errors.Wrap(err, "opening telemetry log")
	}

	defer func() { _ = file.Close() }()

	enc := json.NewEncoder(file)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			return errors.Wrap(err, "writing telemetry event")
		}
	}

	return nil
}

func (f *fileBackend) Close() error { return nil }

// --- HTTP backend ---

type httpBackend struct {
	endpoint string
	client   *http.Client
	log      logger.Logger
}

// NewHTTPBackend returns a backend that POSTs events as JSON to the given endpoint.
// Non-2xx responses are logged at debug level via the provided logger.
// Network errors are silently dropped — telemetry must never block the user.
func NewHTTPBackend(endpoint string, log logger.Logger) Backend {
	return &httpBackend{
		endpoint: endpoint,
		client:   gtbhttp.NewClient(gtbhttp.WithTimeout(httpTimeout)),
		log:      log,
	}
}

func (h *httpBackend) Send(ctx context.Context, events []Event) error {
	body, err := json.Marshal(events)
	if err != nil {
		return errors.Wrap(err, "marshalling telemetry events")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "creating telemetry request")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil // silently drop — telemetry must never block the user
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= httpErrorThreshold {
		h.log.Debug("telemetry endpoint returned non-success status",
			"status", resp.StatusCode, "endpoint", h.endpoint)
	}

	return nil
}

func (h *httpBackend) Close() error { return nil }
