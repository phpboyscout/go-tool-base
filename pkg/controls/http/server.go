package http

import (
	"context"
	"crypto/tls"
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
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second
)

// NewServer returns a new preconfigured http.Server.
func NewServer(ctx context.Context, cfg config.Containable, handler http.Handler) (*http.Server, error) {
	port := cfg.GetInt("server.http.port")
	if port == 0 {
		port = cfg.GetInt("server.port")
	}

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{
				tls.CurveP256,
				tls.X25519, // Recommended for TLS 1.3
			},
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			},
		},
	}

	return srv, nil
}

// Start returns a curried function suitable for use with the controls package.
func Start(cfg config.Containable, logger logger.Logger, srv *http.Server) controls.StartFunc {
	tlsEnabled := cfg.GetBool("server.tls.enabled")
	cert := cfg.GetString("server.tls.cert")
	key := cfg.GetString("server.tls.key")

	return func(_ context.Context) error {
		var err error

		if tlsEnabled {
			logger.Info("starting http server", "tls", true, "addr", srv.Addr)

			err = srv.ListenAndServeTLS(cert, key)
		} else {
			logger.Info("starting http server", "tls", false, "addr", srv.Addr)

			err = srv.ListenAndServe()
		}

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.Wrap(err, "HTTP server failed")
		}

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
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, handler http.Handler) error {
	srv, err := NewServer(ctx, cfg, handler)
	if err != nil {
		return err
	}

	controller.Register(id,
		controls.WithStart(Start(cfg, logger, srv)),
		controls.WithStop(Stop(logger, srv)),
		controls.WithStatus(Status(srv)),
	)

	return nil
}
