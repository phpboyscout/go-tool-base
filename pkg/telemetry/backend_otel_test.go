package telemetry

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// recordsAtLevel returns the messages of every captured record emitted at the
// given slog level, preserving order.
func recordsAtLevel(records []slog.Record, level slog.Level) []string {
	var msgs []string

	for _, r := range records {
		if r.Level == level {
			msgs = append(msgs, r.Message)
		}
	}

	return msgs
}

func TestWithOTelHeaders_DebugsOnSensitiveKeys(t *testing.T) {
	t.Parallel()

	// An HTTPS endpoint means the exporter is constructed lazily —
	// otlploghttp.New accepts the config and does not dial until an
	// event is actually shipped.
	const endpoint = "https://otel-collector.example.internal/"

	capture := logger.NewCaptureHandler()

	_, err := NewOTelBackend(context.Background(), endpoint,
		WithOTelHeaders(map[string]string{
			"Authorization": "Bearer sk-abc123def456ghi789",
			"X-API-Key":     "deadbeefcafebabe",
			"Content-Type":  "application/json",
		}),
		WithOTelLogger(slog.New(capture)),
	)
	require.NoError(t, err)

	records := capture.Records()
	warns := recordsAtLevel(records, slog.LevelWarn)
	debugs := recordsAtLevel(records, slog.LevelDebug)

	// Exactly two DEBUG diagnostics — one per sensitive-looking key. The
	// advisory is DEBUG (not WARN) so it doesn't spam every command.
	assert.Empty(t, warns, "sensitive-header advisory must not be WARN")
	require.Len(t, debugs, 2, "expected a DEBUG diagnostic per sensitive header")

	combined := strings.Join(debugs, "\n")

	assert.Contains(t, combined, "Authorization",
		"diagnostic should name the sensitive header key")
	assert.Contains(t, combined, "X-API-Key",
		"diagnostic should name the sensitive header key")
	assert.Contains(t, combined, "TLS",
		"diagnostic should mention the TLS / middleware remediation")

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

	capture := logger.NewCaptureHandler()

	_, err := NewOTelBackend(context.Background(), "https://otel-collector.example.internal/",
		WithOTelHeaders(map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   "gtb/1.0",
		}),
		WithOTelLogger(slog.New(capture)),
	)
	require.NoError(t, err)

	warns := recordsAtLevel(capture.Records(), slog.LevelWarn)
	assert.Empty(t, warns, "plain headers should not trigger advisories")
}
