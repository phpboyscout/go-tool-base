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
)

// HealthHandler returns an http.HandlerFunc that responds with the controller's health report.
func HealthHandler(controller controls.Controllable) http.HandlerFunc {
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
func LivenessHandler(controller controls.Controllable) http.HandlerFunc {
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
func ReadinessHandler(controller controls.Controllable) http.HandlerFunc {
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
		TLSConfig:      defaultTLSConfig(),
	}

	return srv, nil
}

// Start returns a curried function suitable for use with the controls package.
func Start(cfg config.Containable, logger logger.Logger, srv *http.Server) controls.StartFunc {
	tlsEnabled := cfg.GetBool("server.tls.enabled")
	cert := cfg.GetString("server.tls.cert")
	key := cfg.GetString("server.tls.key")

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

// Register creates a new HTTP server and registers it with the controller under the given id.
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, handler http.Handler) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", HealthHandler(controller))
	mux.HandleFunc("/livez", LivenessHandler(controller))
	mux.HandleFunc("/readyz", ReadinessHandler(controller))
	mux.Handle("/", handler)

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
