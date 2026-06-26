package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/authn"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func mustVerifier(t *testing.T, key, subject string) authn.Verifier {
	t.Helper()

	v, err := authn.NewAPIKeyVerifier(authn.KeyEntry{Key: key, Subject: subject})
	require.NoError(t, err)

	return v
}

// capturingHandler records the identity it sees and returns 200.
func capturingHandler(seen **authn.Identity) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := IdentityFromContext(r.Context())
		*seen = id
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddleware_ConstructionError(t *testing.T) {
	t.Parallel()

	_, err := AuthMiddleware()
	require.Error(t, err, "no verifier must fail closed")
}

func TestAuthMiddleware_BearerSuccessAndFailure(t *testing.T) {
	t.Parallel()

	mw, err := AuthMiddleware(WithBearerVerifier(mustVerifier(t, "good-token", "alice")))
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	// Success: identity reaches the handler.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, seen)
	assert.Equal(t, "alice", seen.Subject)

	// Failure: 401, WWW-Authenticate, generic body, handler not called.
	seen = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
	assert.JSONEq(t, `{"error":"unauthorized"}`, rec.Body.String())
	assert.Nil(t, seen, "handler must not run on auth failure")

	// A non-Bearer Authorization header is treated as no credential -> 401.
	for _, hdr := range []string{"", "Basic dXNlcjpwYXNz", "Bear"} {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api", nil)
		if hdr != "" {
			req.Header.Set("Authorization", hdr)
		}
		h.ServeHTTP(rec, req)
		assert.Equalf(t, http.StatusUnauthorized, rec.Code, "Authorization %q is not a bearer credential", hdr)
	}
}

func TestAuthMiddleware_APIKeyHeader(t *testing.T) {
	t.Parallel()

	mw, err := AuthMiddleware(WithAPIKeyHeader("X-API-Key", mustVerifier(t, "secret", "ci")))
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("X-API-Key", "secret")
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ci", seen.Subject)

	// Missing credential -> 401.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_AmbiguousCredentialsRejected(t *testing.T) {
	t.Parallel()

	mw, err := AuthMiddleware(
		WithBearerVerifier(mustVerifier(t, "tok", "a")),
		WithAPIKeyHeader("X-API-Key", mustVerifier(t, "key", "b")),
	)
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Authorization", "Bearer tok") // valid bearer
	req.Header.Set("X-API-Key", "key")            // AND valid api-key
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "both schemes present must fail closed")
	assert.Nil(t, seen)
}

func TestAuthMiddleware_AuthorizeDenied(t *testing.T) {
	t.Parallel()

	deny := authn.AuthorizeFunc(func(_ context.Context, _ *authn.Identity) bool { return false })

	mw, err := AuthMiddleware(WithBearerVerifier(mustVerifier(t, "tok", "a")), WithAuthorize(deny))
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"error":"forbidden"}`, rec.Body.String())
	assert.Nil(t, seen, "authorized-denied request must not reach the handler")
}

func TestAuthMiddleware_Skipper(t *testing.T) {
	t.Parallel()

	mw, err := AuthMiddleware(
		WithBearerVerifier(mustVerifier(t, "tok", "a")),
		WithAuthSkipper(func(r *http.Request) bool { return r.URL.Path == "/public" }),
	)
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "skipped path bypasses auth")
}

func TestAuthMiddleware_MTLSFallback(t *testing.T) {
	t.Parallel()

	mw, err := AuthMiddleware(WithMTLSVerifier(authn.NewMTLSVerifier()))
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.TLS = &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{{Subject: pkix.Name{CommonName: "client-a"}}}},
	}
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, seen)
	assert.Equal(t, "client-a", seen.Subject)
	assert.Equal(t, "mtls", seen.Method)

	// No client cert -> 401.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_RedactsCredentialInLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel))

	mw, err := AuthMiddleware(
		WithBearerVerifier(mustVerifier(t, "good", "a")),
		WithAuthLogger(log),
	)
	require.NoError(t, err)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	// A token shaped like a real secret so redact would catch it if logged.
	req.Header.Set("Authorization", "Bearer sk-abcdefghijklmnopqrstuvwxyz0123456789ABCD")
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, buf.String(), "sk-abcdefghijklmnopqrstuvwxyz0123456789ABCD",
		"the presented token must never appear in logs")
}

// reqCookie builds a request cookie for tests. Secure/HttpOnly/SameSite are
// response-only (req.AddCookie serialises just name=value), so they are a no-op
// here — set only to satisfy gosec G124 without a nolint.
func reqCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	}
}

func TestAuthMiddleware_CookieSuccessAndFailure(t *testing.T) {
	t.Parallel()

	mw, err := AuthMiddleware(WithCookieVerifier("session", mustVerifier(t, "good-token", "browser")))
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	// Success: a valid cookie authenticates (the case an <img>/<video> load needs).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.AddCookie(reqCookie("session", "good-token"))
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, seen)
	assert.Equal(t, "browser", seen.Subject)

	// Wrong cookie value -> 401, handler not called.
	seen = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api", nil)
	req.AddCookie(reqCookie("session", "wrong"))
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, seen)

	// Missing / empty cookie -> 401.
	for _, c := range []*http.Cookie{nil, reqCookie("session", ""), reqCookie("other", "good-token")} {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api", nil)
		if c != nil {
			req.AddCookie(c)
		}
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_ExplicitHeaderBeatsAmbientCookie(t *testing.T) {
	t.Parallel()

	// Same verifier for both schemes; the cookie is ambient (sent automatically),
	// so an explicit bearer header must take precedence over it.
	v := mustVerifier(t, "tok", "principal")
	mw, err := AuthMiddleware(WithBearerVerifier(v), WithCookieVerifier("session", v))
	require.NoError(t, err)

	var seen *authn.Identity
	h := mw(capturingHandler(&seen))

	// A bad bearer header with a GOOD cookie: the explicit header wins and fails
	// closed (the ambient cookie does NOT silently rescue a wrong explicit credential).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	req.AddCookie(reqCookie("session", "tok"))
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, seen)

	// No header, just the cookie: authenticates (the browser sub-resource path).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api", nil)
	req.AddCookie(reqCookie("session", "tok"))
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
