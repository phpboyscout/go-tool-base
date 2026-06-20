package openapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openapi"
)

// serve registers the spec on a fresh mux with the given options and replays a
// request through httptest.ResponseRecorder — no real network, no wall-clock.
func serve(t *testing.T, method, target string, spec []byte, opts ...openapi.Option) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	require.NoError(t, openapi.Register(mux, spec, opts...))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))

	return rec
}

func TestWithSpecPath(t *testing.T) {
	t.Parallel()

	spec := []byte("openapi: 3.0.3\n")
	rec := serve(t, http.MethodGet, "/api/spec.yaml", spec, openapi.WithSpecPath("/api/spec.yaml"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Equal(t, spec, rec.Body.Bytes())
}

func TestWithSpecPath_ReflectedInDocsIndex(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/docs/", []byte("openapi: 3.0.3\n"),
		openapi.WithSpecPath("/api/spec.yaml"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `apiDescriptionUrl="/api/spec.yaml"`)
}

func TestWithDocsPath(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/reference/", []byte("openapi: 3.0.3\n"),
		openapi.WithDocsPath("/reference/"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "<elements-api")
	// The custom docs path is woven into the asset references.
	assert.Contains(t, body, `href="/reference/styles.min.css"`)
	assert.Contains(t, body, `src="/reference/web-components.min.js"`)
}

func TestWithDocsPath_ServesEmbeddedAssetSubtree(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/reference/styles.min.css", []byte("openapi: 3.0.3\n"),
		openapi.WithDocsPath("/reference/"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Body.Bytes())
}

func TestRegister_AllOptionsCombined(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/v1/docs/", []byte("openapi: 3.0.3\n"),
		openapi.WithSpecPath("/v1/openapi.yaml"),
		openapi.WithDocsPath("/v1/docs/"),
		openapi.WithTitle("Combined API"))

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Combined API")
	assert.Contains(t, body, `apiDescriptionUrl="/v1/openapi.yaml"`)
	assert.Contains(t, body, `src="/v1/docs/web-components.min.js"`)
}

func TestRegister_NilSpecServesEmptyBody(t *testing.T) {
	t.Parallel()

	// A nil/empty spec must still register cleanly and serve an empty body —
	// no panic, correct content type, 200.
	rec := serve(t, http.MethodGet, "/openapi.yaml", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Empty(t, rec.Body.Bytes())
}

func TestRegister_SpecRejectsNonGET(t *testing.T) {
	t.Parallel()

	// The spec route is registered as "GET /openapi.yaml"; a POST must not
	// match it and falls through to the mux's 405 handling.
	rec := serve(t, http.MethodPost, "/openapi.yaml", []byte("openapi: 3.0.3\n"))

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestRegister_DocsExactPathRejectsNonGET(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodDelete, "/docs/", []byte("openapi: 3.0.3\n"))

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestRegister_UnknownPathNotFound(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/nope", []byte("openapi: 3.0.3\n"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRegister_MissingEmbeddedAssetNotFound(t *testing.T) {
	t.Parallel()

	// A request under the docs subtree for a file that is not in the embedded
	// FS exercises the FileServer 404 path.
	rec := serve(t, http.MethodGet, "/docs/does-not-exist.js", []byte("openapi: 3.0.3\n"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRegister_WithoutSecurityHeaders_SpecAndAssets(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/openapi.yaml", "/docs/", "/docs/web-components.min.js"} {
		rec := serve(t, http.MethodGet, path, []byte("openapi: 3.0.3\n"),
			openapi.WithoutSecurityHeaders())

		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Empty(t, rec.Header().Get("X-Content-Type-Options"), path)
		assert.Empty(t, rec.Header().Get("X-Frame-Options"), path)
		assert.Empty(t, rec.Header().Get("Content-Security-Policy"), path)
	}
}
