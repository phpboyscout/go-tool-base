package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	readTimeout           = 5 * time.Second
	writeTimeout          = 10 * time.Second
	idleTimeout           = 120 * time.Second
	defaultMaxHeaderBytes = 1 << 20 // 1MB default
	// DefaultMaxRequestBodyBytes caps the size of each request body
	// accepted by the management HTTP server. Closes M-1 from
	// docs/development/reports/security-audit-2026-04-17.md.
	DefaultMaxRequestBodyBytes int64 = 1 << 20 // 1 MiB
)

// HealthHandler returns an http.HandlerFunc that responds with the controller's health report.
func HealthHandler(controller controls.HealthReporter) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		report := controller.Status()

		w.Header().Set("Content-Type", "application/json")

		if !report.OverallHealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_ = json.NewEncoder(w).Encode(report)
	}
}

// LivenessHandler returns an http.HandlerFunc that responds with the controller's liveness report.
func LivenessHandler(controller controls.HealthReporter) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		report := controller.Liveness()

		w.Header().Set("Content-Type", "application/json")

		if !report.OverallHealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_ = json.NewEncoder(w).Encode(report)
	}
}

// ReadinessHandler returns an http.HandlerFunc that responds with the controller's readiness report.
func ReadinessHandler(controller controls.HealthReporter) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		report := controller.Readiness()

		w.Header().Set("Content-Type", "application/json")

		if !report.OverallHealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_ = json.NewEncoder(w).Encode(report)
	}
}

// NewServer returns a new preconfigured http.Server.
func NewServer(ctx context.Context, cfg config.Containable, handler http.Handler) (*http.Server, error) {
	port := cfg.GetInt("server.http.port")
	if port == 0 {
		port = cfg.GetInt("server.port")
	}

	maxHeaderBytes := cfg.GetInt("server.http.max_header_bytes")
	if maxHeaderBytes == 0 {
		maxHeaderBytes = defaultMaxHeaderBytes
	}

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		Handler:        handler,
		ReadTimeout:    readTimeout,
		WriteTimeout:   writeTimeout,
		IdleTimeout:    idleTimeout,
		MaxHeaderBytes: maxHeaderBytes,
		TLSConfig:      DefaultTLSConfig(),
	}

	return srv, nil
}

// Start returns a curried function suitable for use with the controls package.
func Start(cfg config.Containable, logger logger.Logger, srv *http.Server) controls.StartFunc {
	tlsEnabled, cert, key := ResolveTLSConfig(cfg, "server.http.tls")

	return func(ctx context.Context) error {
		var lc net.ListenConfig

		ln, err := lc.Listen(ctx, "tcp", srv.Addr)
		if err != nil {
			return errors.Wrap(err, "failed to listen")
		}

		go func() {
			var err error

			if tlsEnabled {
				logger.Info("starting http server", "tls", true, "addr", srv.Addr)
				err = srv.ServeTLS(ln, cert, key)
			} else {
				logger.Info("starting http server", "tls", false, "addr", srv.Addr)
				err = srv.Serve(ln)
			}

			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("HTTP server failed", "error", err)
			}
		}()

		return nil
	}
}

// Stop returns a curried function suitable for use with the controls package.
func Stop(logger logger.Logger, srv *http.Server) controls.StopFunc {
	return func(ctx context.Context) {
		logger.Info("stopping http server", "addr", srv.Addr)

		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("server shutdown failed", "error", err)
		}
	}
}

// Status returns a curried function suitable for use with the controls package.
func Status(srv *http.Server) controls.StatusFunc {
	return func() error {
		if srv == nil {
			return errors.New("http server is nil")
		}

		return nil
	}
}

// RegisterOption configures optional behaviour for HTTP server registration.
type RegisterOption func(*registerConfig)

type registerConfig struct {
	chain               *Chain
	maxRequestBodyBytes int64
}

// WithMiddleware sets the middleware chain applied to the handler before
// it is passed to the HTTP server. Health endpoints (/healthz, /livez,
// /readyz) are mounted outside the chain and are never affected by middleware.
func WithMiddleware(chain Chain) RegisterOption {
	return func(c *registerConfig) {
		c.chain = &chain
	}
}

// WithMaxRequestBodyBytes overrides the DefaultMaxRequestBodyBytes cap
// applied to every request body. Set to a negative value to disable the
// cap entirely (not recommended).
func WithMaxRequestBodyBytes(n int64) RegisterOption {
	return func(c *registerConfig) {
		c.maxRequestBodyBytes = n
	}
}

// MaxBytesMiddleware wraps a handler so every request body is bounded by
// http.MaxBytesReader. A request that exceeds the limit is terminated
// with HTTP 413 (via the default ResponseWriter behaviour) when the
// handler attempts to read past the boundary.
//
// Callers that need per-route limits should wrap the handler directly
// rather than registering at server level.
func MaxBytesMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxBytes > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Register creates a new HTTP server and registers it with the controller under the given id.
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, handler http.Handler, opts ...RegisterOption) (*http.Server, error) {
	rc := registerConfig{
		maxRequestBodyBytes: DefaultMaxRequestBodyBytes,
	}
	for _, o := range opts {
		o(&rc)
	}

	// Apply middleware chain to the handler, not the health endpoints.
	if rc.chain != nil {
		handler = rc.chain.Then(handler)
	}

	bodyLimit := MaxBytesMiddleware(rc.maxRequestBodyBytes)

	mux := http.NewServeMux()
	mux.Handle("/healthz", bodyLimit(HealthHandler(controller)))
	mux.Handle("/livez", bodyLimit(LivenessHandler(controller)))
	mux.Handle("/readyz", bodyLimit(ReadinessHandler(controller)))
	mux.Handle("/", bodyLimit(handler))

	srv, err := NewServer(ctx, cfg, mux)
	if err != nil {
		return nil, err
	}

	controller.Register(id,
		controls.WithStart(Start(cfg, logger, srv)),
		controls.WithStop(Stop(logger, srv)),
		controls.WithStatus(Status(srv)),
	)

	return srv, nil
}
