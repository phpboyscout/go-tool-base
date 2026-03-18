package docs

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"
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
		return err
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://localhost:%d", actualPort)

	fmt.Printf("Documentation server starting at %s\n", url)

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
