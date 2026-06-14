package docs

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureServeURL starts Serve with the given options on a random port,
// captures the structured "url" attribute it logs, then cancels and waits
// for a clean shutdown. The returned string is the URL the server reported
// binding to.
//
// These tests swap the process-global slog default, so they must not run in
// parallel with each other.
func captureServeURL(t *testing.T, opts ...ServeOption) string {
	t.Helper()

	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)

	handler := slog.NewTextHandler(syncWriter{mu: &mu, buf: &buf}, nil)

	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithCancel(context.Background())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- Serve(ctx, fstest.MapFS{"index.html": {Data: []byte("ok")}}, 0, opts...)
	}()

	// Poll for the log line rather than sleeping a fixed duration.
	var url string

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		out := buf.String()
		idx := strings.Index(out, "url=")
		if idx == -1 {
			return false
		}

		rest := out[idx+len("url="):]
		url = strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0])

		return url != ""
	}, 2*time.Second, 5*time.Millisecond, "server did not log a bind url")

	cancel()

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}

	return url
}

type syncWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

// TestServe_DefaultBindsLoopback proves the default bind interface is
// 127.0.0.1 (loopback) and that the logged URL reports the real bound
// address — not a hard-coded "localhost".
func TestServe_DefaultBindsLoopback(t *testing.T) {
	url := captureServeURL(t)

	assert.Contains(t, url, "127.0.0.1",
		"default bind must be loopback and the log must report it")
	assert.NotContains(t, url, "localhost",
		"log line must print the real bind address, not a hard-coded localhost")
	assert.NotContains(t, url, "0.0.0.0",
		"default bind must not be all-interfaces")
}

// TestServe_WithHostWidensBind proves the WithHost option widens the bind
// address (here to all interfaces) and that the log reports the widened
// address rather than the loopback default.
func TestServe_WithHostWidensBind(t *testing.T) {
	url := captureServeURL(t, WithHost("0.0.0.0"))

	// Binding 0.0.0.0 resolves to a non-loopback (often dual-stack "[::]")
	// listener; the precise spelling is OS-dependent. The invariant is that
	// the bind widened off loopback and the log reports the real address.
	assert.NotContains(t, url, "127.0.0.1",
		"WithHost must widen the bind address off loopback")
}

// TestServe_WithHostEmptyFallsBackToLoopback proves an empty host argument
// does not blank the bind address — it falls back to the loopback default.
func TestServe_WithHostEmptyFallsBackToLoopback(t *testing.T) {
	url := captureServeURL(t, WithHost(""))

	assert.Contains(t, url, "127.0.0.1",
		"empty host must fall back to the loopback default")
}
