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

	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
)

const (
	readHeaderTimeout = 3 * time.Second

	// defaultServeHost is the loopback interface the documentation server
	// binds to unless widened via [WithHost]. Binding loopback by default
	// keeps the docs server off the local network — the static site is only
	// reachable from the machine running the command.
	defaultServeHost = "127.0.0.1"
)

// serveConfig holds the resolved configuration for the documentation server.
type serveConfig struct {
	host string
}

// ServeOption configures the documentation server started by [Serve].
type ServeOption func(*serveConfig)

// WithHost overrides the interface the documentation server binds to. The
// default is loopback (127.0.0.1). Pass "0.0.0.0" (or "::") to expose the
// server on all interfaces — only do this on a trusted network. An empty
// host falls back to the loopback default.
func WithHost(host string) ServeOption {
	return func(c *serveConfig) {
		if host != "" {
			c.host = host
		}
	}
}

// Serve starts a documentation server on the given port serving the provided
// filesystem. By default it binds the loopback interface (127.0.0.1); pass
// [WithHost] to widen the bind address.
func Serve(ctx context.Context, fsys fs.FS, port int, opts ...ServeOption) error {
	cfg := serveConfig{host: defaultServeHost}
	for _, opt := range opts {
		opt(&cfg)
	}

	addr := net.JoinHostPort(cfg.host, fmt.Sprintf("%d", port))

	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to start listener")
	}

	// Report the address the listener actually bound to (including the
	// resolved port when the caller passed 0) rather than a hard-coded
	// "localhost", which would mislead when the bind host is widened.
	url := fmt.Sprintf("http://%s", listener.Addr().String())

	slog.Info("Documentation server starting", "url", url)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(fsys)))

	// Apply conservative security headers to the built-in docs surface
	// (nosniff, frame-ancestors 'none', referrer-policy). HSTS stays off:
	// the server is plain HTTP on loopback by default.
	handler := gtbhttp.SecurityHeadersMiddleware()(mux)

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// Handle context cancellation
	go func() {
		<-ctx.Done()

		_ = server.Close()
	}()

	return server.Serve(listener)
}
