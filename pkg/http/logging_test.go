package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func findEntry(entries []logger.Entry, msg string) (logger.Entry, bool) {
	for _, e := range entries {
		if strings.Contains(e.Message, msg) {
			return e, true
		}
	}

	return logger.Entry{}, false
}

func keyvalMap(keyvals []any) map[string]any {
	m := make(map[string]any)
	for i := 0; i+1 < len(keyvals); i += 2 {
		if k, ok := keyvals[i].(string); ok {
			m[k] = keyvals[i+1]
		}
	}

	return m
}

func TestLoggingMiddleware_DefaultStructuredFields(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "hello")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("User-Agent", "test-agent/1.0")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, 1, buf.Len())

	entry, found := findEntry(buf.Entries(), "request completed")
	require.True(t, found, "should log 'request completed'")
	assert.Equal(t, logger.InfoLevel, entry.Level)

	kv := keyvalMap(entry.Keyvals)
	assert.Equal(t, "GET", kv["method"])
	assert.Equal(t, "/api/data", kv["path"])
	assert.Equal(t, http.StatusOK, kv["status"])
	assert.Equal(t, 5, kv["bytes"])
	assert.Contains(t, kv, "latency")
	assert.Equal(t, "10.0.0.1", kv["client_ip"])
	assert.Equal(t, "test-agent/1.0", kv["user_agent"])
}

func TestLoggingMiddleware_5xxLogsAtErrorLevel(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fail", nil))

	require.Equal(t, 1, buf.Len())

	entry := buf.Entries()[0]
	assert.Equal(t, logger.ErrorLevel, entry.Level)
}

func TestLoggingMiddleware_WithLogLevel(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithLogLevel(logger.DebugLevel))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, 1, buf.Len())
	assert.Equal(t, logger.DebugLevel, buf.Entries()[0].Level)
}

func TestLoggingMiddleware_WithPathFilter(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithPathFilter("/healthz", "/livez"))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Filtered path — should not log
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, 0, buf.Len())

	// Non-filtered path — should log
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/data", nil))
	assert.Equal(t, 1, buf.Len())
}

func TestLoggingMiddleware_WithoutLatency(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithoutLatency())

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.NotContains(t, kv, "latency")
}

func TestLoggingMiddleware_WithoutUserAgent(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithoutUserAgent())

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "should-not-appear")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.NotContains(t, kv, "user_agent")
}

func TestLoggingMiddleware_WithHeaderFields(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithHeaderFields("x-request-id", "x-custom"))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	req.Header.Set("X-Custom", "custom-val")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.Equal(t, "abc-123", kv["x-request-id"])
	assert.Equal(t, "custom-val", kv["x-custom"])
}

func TestLoggingMiddleware_ClientIP_XForwardedFor(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	req.Header.Set("X-Forwarded-For", "10.20.30.40, 10.20.30.41")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.Equal(t, "10.20.30.40", kv["client_ip"])
}

func TestLoggingMiddleware_ClientIP_XRealIP(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	req.Header.Set("X-Real-IP", "10.20.30.50")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.Equal(t, "10.20.30.50", kv["client_ip"])
}

func TestLoggingMiddleware_DefaultStatusCode(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf)

	// Handler that writes body without explicit WriteHeader — defaults to 200
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "ok")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.Equal(t, http.StatusOK, kv["status"])
}

// --- Format tests ---

func TestLoggingMiddleware_FormatCommon(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithFormat(FormatCommon))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "hello")
	}))

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.RemoteAddr = "10.0.0.1:12345"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, 1, buf.Len())

	msg := buf.Entries()[0].Message
	// CLF: <ip> - - [<timestamp>] "<method> <path> <proto>" <status> <bytes>
	assert.Contains(t, msg, "10.0.0.1")
	assert.Contains(t, msg, `"GET /page HTTP/1.1"`)
	assert.Contains(t, msg, "200")
	assert.Contains(t, msg, "5")
	assert.Contains(t, msg, " - - [")
}

func TestLoggingMiddleware_FormatCombined(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithFormat(FormatCombined))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("Referer", "https://example.com")
	req.Header.Set("User-Agent", "test-agent/2.0")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	msg := buf.Entries()[0].Message
	assert.Contains(t, msg, `"https://example.com"`)
	assert.Contains(t, msg, `"test-agent/2.0"`)
}

func TestLoggingMiddleware_FormatCombined_WithoutUserAgent(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithFormat(FormatCombined), WithoutUserAgent())

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("User-Agent", "should-not-appear")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	msg := buf.Entries()[0].Message
	assert.NotContains(t, msg, "should-not-appear")
	// Should have "-" in the user-agent position
	assert.True(t, strings.HasSuffix(strings.TrimSpace(msg), `"-"`))
}

func TestLoggingMiddleware_FormatJSON(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithFormat(FormatJSON))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, "created")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/items", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("User-Agent", "json-test/1.0")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, 1, buf.Len())

	msg := buf.Entries()[0].Message
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(msg), &parsed))

	assert.Equal(t, "POST", parsed["method"])
	assert.Equal(t, "/api/items", parsed["path"])
	assert.InDelta(t, float64(http.StatusCreated), parsed["status"], 0)
	assert.InDelta(t, float64(7), parsed["bytes"], 0)
	assert.Equal(t, "10.0.0.1", parsed["client_ip"])
	assert.Equal(t, "json-test/1.0", parsed["user_agent"])
	assert.Contains(t, parsed, "latency")
	assert.Contains(t, parsed, "timestamp")
}

func TestLoggingMiddleware_FormatJSON_WithoutLatency(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithFormat(FormatJSON), WithoutLatency())

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	msg := buf.Entries()[0].Message
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(msg), &parsed))

	assert.NotContains(t, parsed, "latency")
}

func TestLoggingMiddleware_FormatJSON_WithHeaderFields(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithFormat(FormatJSON), WithHeaderFields("x-request-id"))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "req-456")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	msg := buf.Entries()[0].Message
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(msg), &parsed))

	assert.Equal(t, "req-456", parsed["x-request-id"])
}

func TestLoggingMiddleware_FormatCommon_PathFilter(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	mw := LoggingMiddleware(buf, WithFormat(FormatCommon), WithPathFilter("/healthz"))

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, 0, buf.Len())
}
