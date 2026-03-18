package docs

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestServeStatic(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<h1>Welcome</h1>")},
		"about.html": {Data: []byte("<h1>About</h1>")},
	}

	t.Run("Serve index", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		handler := http.FileServer(http.FS(fsys))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "Welcome")
	})

	t.Run("Serve missing file", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/missing.html", nil)
		rr := httptest.NewRecorder()

		handler := http.FileServer(http.FS(fsys))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}
