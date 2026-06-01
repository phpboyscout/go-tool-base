package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestOTelMiddlewareRecordsServerSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	// OTelMiddleware composes in a Chain like any other middleware.
	handler := NewChain(OTelMiddleware("macguffin")).Then(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/v1/macguffins", nil))

	require.NoError(t, tp.ForceFlush(context.Background()))
	assert.Equal(t, http.StatusOK, resp.Code, "middleware must pass the request through")

	spans := rec.Ended()
	require.Len(t, spans, 1, "exactly one server span per request")
	assert.Equal(t, oteltrace.SpanKindServer, spans[0].SpanKind())
}

func TestLoggingMiddlewareCorrelatesWithActiveTrace(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))

	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	buf := logger.NewBuffer()

	// OTelMiddleware must precede the logging middleware so the access log can
	// read the active span from the request context.
	handler := NewChain(
		OTelMiddleware("macguffin"),
		LoggingMiddleware(buf),
	).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/macguffins", nil))

	require.Equal(t, 1, buf.Len())
	kv := keyvalMap(buf.Entries()[0].Keyvals)
	require.Contains(t, kv, "trace_id", "access log must carry the trace id")
	assert.Len(t, kv["trace_id"], 32, "trace id is a 16-byte value in hex")
	assert.Contains(t, kv, "span_id")
}

func TestLoggingMiddlewareOmitsTraceWhenNoSpan(t *testing.T) {
	buf := logger.NewBuffer()

	handler := NewChain(LoggingMiddleware(buf)).Then(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, 1, buf.Len())
	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.NotContains(t, kv, "trace_id", "no span, no correlation fields")
}
