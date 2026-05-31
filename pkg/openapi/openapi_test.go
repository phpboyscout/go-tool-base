package openapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
