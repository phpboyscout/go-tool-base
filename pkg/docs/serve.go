package docs

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	readHeaderTimeout = 3 * time.Second
)

// --- Documentation Server ---

// Serve starts a documentation server on the given port serving the provided filesystem.
func Serve(ctx context.Context, fsys fs.FS, port int) error {
	addr := fmt.Sprintf(":%d", port)

	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to start listener")
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://localhost:%d", actualPort)

	slog.Info("Documentation server starting", "url", url)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(fsys)))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// Handle context cancellation
	go func() {
		<-ctx.Done()

		_ = server.Close()
	}()

	return server.Serve(listener)
}
