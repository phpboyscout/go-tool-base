package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serveWithSecurity(t *testing.T, opts ...SecurityHeadersOption) *httptest.ResponseRecorder {
	t.Helper()

	mw := SecurityHeadersMiddleware(opts...)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	return rec
}

func TestSecurityHeadersMiddleware_Defaults(t *testing.T) {
	t.Parallel()

	rec := serveWithSecurity(t)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "frame-ancestors 'none'", rec.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))

	// HSTS must be OFF by default — it is only meaningful over TLS.
	assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
}

func TestSecurityHeadersMiddleware_ConfigurableValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opts   []SecurityHeadersOption
		header string
		want   string
	}{
		{
			name:   "custom referrer policy",
			opts:   []SecurityHeadersOption{WithReferrerPolicy("strict-origin-when-cross-origin")},
			header: "Referrer-Policy",
			want:   "strict-origin-when-cross-origin",
		},
		{
			name:   "custom frame options",
			opts:   []SecurityHeadersOption{WithFrameOptions("SAMEORIGIN")},
			header: "X-Frame-Options",
			want:   "SAMEORIGIN",
		},
		{
			name:   "custom content type options",
			opts:   []SecurityHeadersOption{WithContentTypeOptions("nosniff")},
			header: "X-Content-Type-Options",
			want:   "nosniff",
		},
		{
			name:   "full CSP replaces default",
			opts:   []SecurityHeadersOption{WithContentSecurityPolicy("default-src 'self'; frame-ancestors 'none'")},
			header: "Content-Security-Policy",
			want:   "default-src 'self'; frame-ancestors 'none'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serveWithSecurity(t, tt.opts...)
			assert.Equal(t, tt.want, rec.Header().Get(tt.header))
		})
	}
}

func TestSecurityHeadersMiddleware_EmptyValuesOmitHeader(t *testing.T) {
	t.Parallel()

	rec := serveWithSecurity(t,
		WithReferrerPolicy(""),
		WithFrameOptions(""),
		WithContentTypeOptions(""),
	)

	assert.Empty(t, rec.Header().Get("Referrer-Policy"))
	assert.Empty(t, rec.Header().Get("X-Frame-Options"))
	assert.Empty(t, rec.Header().Get("X-Content-Type-Options"))
	// CSP frame-ancestors control is never dropped: empty CSP falls back.
	assert.Equal(t, "frame-ancestors 'none'", rec.Header().Get("Content-Security-Policy"))
}

func TestSecurityHeadersMiddleware_EmptyCSPFallsBack(t *testing.T) {
	t.Parallel()

	rec := serveWithSecurity(t, WithContentSecurityPolicy(""))
	assert.Equal(t, "frame-ancestors 'none'", rec.Header().Get("Content-Security-Policy"))
}

func TestSecurityHeadersMiddleware_HSTS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		maxAge    time.Duration
		subdomain bool
		preload   bool
		want      string
	}{
		{
			name:   "disabled when zero",
			maxAge: 0,
			want:   "",
		},
		{
			name:   "disabled when negative",
			maxAge: -time.Hour,
			want:   "",
		},
		{
			name:   "max-age only",
			maxAge: 24 * time.Hour,
			want:   "max-age=86400",
		},
		{
			name:      "with subdomains",
			maxAge:    time.Hour,
			subdomain: true,
			want:      "max-age=3600; includeSubDomains",
		},
		{
			name:      "with subdomains and preload",
			maxAge:    time.Hour,
			subdomain: true,
			preload:   true,
			want:      "max-age=3600; includeSubDomains; preload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serveWithSecurity(t, WithHSTS(tt.maxAge, tt.subdomain, tt.preload))
			assert.Equal(t, tt.want, rec.Header().Get("Strict-Transport-Security"))
		})
	}
}

func TestSecurityHeadersMiddleware_SetBeforeHandlerWrites(t *testing.T) {
	t.Parallel()

	// A handler that writes a body (and an implicit 200) should still emit
	// the security headers — they are set before next.ServeHTTP runs.
	mw := SecurityHeadersMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("body without explicit WriteHeader"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}
