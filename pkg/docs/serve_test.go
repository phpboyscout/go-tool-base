package docs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServe_InvalidPort(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{}
	ctx := context.Background()

	// Port 99999 is out of valid range — Listen should fail immediately
	err := Serve(ctx, fsys, 99999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start listener")
}

func TestServe_StartsAndShutsDownCleanly(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<h1>Hello</h1>")},
	}

	ctx, cancel := context.WithCancel(context.Background())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- Serve(ctx, fsys, 0)
	}()

	// Give the server time to start
	time.Sleep(30 * time.Millisecond)

	// Cancel the context to trigger shutdown
	cancel()

	select {
	case err := <-doneCh:
		// http.ErrServerClosed is the expected clean-shutdown signal
		assert.True(t, err == nil || err == http.ErrServerClosed,
			"unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

// TestServeStatic verifies the HTTP handler logic using httptest (no real TCP).
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

// TestServeWithRealRequest verifies the handler wiring: FileServer on "/" serves files.
func TestServeWithRealRequest(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"page.html": {Data: []byte("<html>test content</html>")},
	}

	handler := http.FileServer(http.FS(fsys))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	resp, err := http.Get(fmt.Sprintf("%s/page.html", server.URL))
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "test content")
}
