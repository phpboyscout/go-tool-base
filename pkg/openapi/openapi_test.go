package openapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpsec "gitlab.com/phpboyscout/go/transport/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openapi"
)

func TestRegister(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	spec := []byte("openapi: 3.0.3\ninfo:\n  title: Test\n")

	require.NoError(t, openapi.Register(mux, spec, openapi.WithTitle("Test API")))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Spec served verbatim with the yaml content type.
	status, ctype, body := get(t, srv.URL+"/openapi.yaml")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "application/yaml", ctype)
	assert.Equal(t, spec, body)

	// Docs index references the spec and the Stoplight web component.
	status, _, body = get(t, srv.URL+"/docs/")
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "<elements-api")
	assert.Contains(t, string(body), `apiDescriptionUrl="/openapi.yaml"`)
	assert.Contains(t, string(body), "Test API")

	// Embedded Stoplight asset is served from the framework.
	status, _, _ = get(t, srv.URL+"/docs/web-components.min.js")
	assert.Equal(t, http.StatusOK, status)
}

func get(t *testing.T, url string) (int, string, []byte) {
	t.Helper()

	res, err := http.Get(url) //nolint:gosec,noctx // test helper, controlled URL
	require.NoError(t, err)

	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	return res.StatusCode, res.Header.Get("Content-Type"), body
}

func headers(t *testing.T, url string) http.Header {
	t.Helper()

	res, err := http.Get(url) //nolint:gosec,noctx // test helper, controlled URL
	require.NoError(t, err)

	defer func() { _ = res.Body.Close() }()

	_, _ = io.Copy(io.Discard, res.Body)

	return res.Header
}

func TestRegister_SecurityHeadersOnBuiltinHandlers(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	require.NoError(t, openapi.Register(mux, []byte("openapi: 3.0.3\n")))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The docs UI, spec, and embedded assets all carry the conservative
	// security headers by default.
	for _, path := range []string{"/openapi.yaml", "/docs/", "/docs/web-components.min.js"} {
		h := headers(t, srv.URL+path)
		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), path)
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"), path)
		assert.Equal(t, "frame-ancestors 'none'", h.Get("Content-Security-Policy"), path)
		assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"), path)
		// HSTS stays off by default.
		assert.Empty(t, h.Get("Strict-Transport-Security"), path)
	}
}

func TestRegister_SecurityHeaderOptionsCustomised(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	require.NoError(t, openapi.Register(mux, []byte("openapi: 3.0.3\n"),
		openapi.WithSecurityHeaderOptions(httpsec.WithReferrerPolicy("strict-origin-when-cross-origin"))))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := headers(t, srv.URL+"/docs/")
	assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
}

func TestRegister_WithoutSecurityHeaders(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	require.NoError(t, openapi.Register(mux, []byte("openapi: 3.0.3\n"),
		openapi.WithoutSecurityHeaders()))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := headers(t, srv.URL+"/docs/")
	assert.Empty(t, h.Get("X-Content-Type-Options"))
	assert.Empty(t, h.Get("Content-Security-Policy"))
}
