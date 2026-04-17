package telemetry

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// spyLogger captures log calls so tests can assert on them. All calls
// are serialised through mu so the test can read fields safely even
// if the code under test logs from multiple goroutines.
type spyLogger struct {
	logger.Logger

	mu    sync.Mutex
	warns []warnEntry
}

type warnEntry struct {
	msg     string
	keyvals []any
}

func (s *spyLogger) Warn(msg string, keyvals ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.warns = append(s.warns, warnEntry{msg: msg, keyvals: keyvals})
}

// newSpyLogger wraps a noop logger with Warn interception. Other
// logger methods delegate to noop so unexercised paths do not panic.
func newSpyLogger() *spyLogger {
	return &spyLogger{Logger: logger.NewNoop()}
}

func TestWithOTelHeaders_WarnsOnSensitiveKeys(t *testing.T) {
	t.Parallel()

	// An HTTPS endpoint means the exporter is constructed lazily —
	// otlploghttp.New accepts the config and does not dial until an
	// event is actually shipped.
	const endpoint = "https://otel-collector.example.internal/"

	spy := newSpyLogger()

	_, err := NewOTelBackend(context.Background(), endpoint,
		WithOTelHeaders(map[string]string{
			"Authorization": "Bearer sk-abc123def456ghi789",
			"X-API-Key":     "deadbeefcafebabe",
			"Content-Type":  "application/json",
		}),
		WithOTelLogger(spy),
	)
	require.NoError(t, err)

	spy.mu.Lock()
	defer spy.mu.Unlock()

	// Exactly two warnings — one per sensitive-looking key.
	require.Len(t, spy.warns, 2, "expected a WARN per sensitive header")

	msgs := []string{spy.warns[0].msg, spy.warns[1].msg}
	combined := strings.Join(msgs, "\n")

	assert.Contains(t, combined, "Authorization",
		"warning should name the sensitive header key")
	assert.Contains(t, combined, "X-API-Key",
		"warning should name the sensitive header key")
	assert.Contains(t, combined, "TLS",
		"warning should mention the TLS / middleware remediation")

	// Critically: the header VALUES must never appear in the warning.
	// Callers routinely put tokens in these fields, so the warning
	// text itself must not echo them.
	assert.NotContains(t, combined, "Bearer sk-abc123def456ghi789",
		"warning must not echo header value (Authorization)")
	assert.NotContains(t, combined, "deadbeefcafebabe",
		"warning must not echo header value (X-API-Key)")
}

func TestWithOTelHeaders_NoWarnForPlainHeaders(t *testing.T) {
	t.Parallel()

	spy := newSpyLogger()

	_, err := NewOTelBackend(context.Background(), "https://otel-collector.example.internal/",
		WithOTelHeaders(map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   "gtb/1.0",
		}),
		WithOTelLogger(spy),
	)
	require.NoError(t, err)

	spy.mu.Lock()
	defer spy.mu.Unlock()

	assert.Empty(t, spy.warns, "plain headers should not trigger advisories")
}
