package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
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

// DefaultConfigPrefix is the config prefix an HTTP server reads (port, TLS,
// max_header_bytes) unless overridden with WithConfigPrefix.
const DefaultConfigPrefix = "server.http"

// maxPort is the highest valid TCP port number.
const maxPort = 65535

// serverConfig carries the construction settings consumed by NewServer and
// Start. A zero field means "fall back to config or the built-in default".
type serverConfig struct {
	prefix         string
	port           *int
	maxHeaderBytes int
	readTimeout    time.Duration
	writeTimeout   time.Duration
	idleTimeout    time.Duration
	tlsConfig      *tls.Config
}

// ServerSettings contains the data needed to construct an HTTP server without
// binding the core constructor to any particular config system.
type ServerSettings struct {
	Port           int `mapstructure:"port" yaml:"port" json:"port"`
	MaxHeaderBytes int `mapstructure:"max_header_bytes" yaml:"max_header_bytes" json:"max_header_bytes"`
}

// defaultServerConfig returns the baseline construction settings: the default
// config prefix and the package's built-in timeouts.
func defaultServerConfig() serverConfig {
	return serverConfig{
		prefix:       DefaultConfigPrefix,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
		idleTimeout:  idleTimeout,
	}
}

// ServerOption configures an HTTP server built by NewServer or started by
// Start. ServerOption values are also accepted by Register.
type ServerOption func(*serverConfig)

// WithConfigPrefix sets the config prefix the server reads its port, TLS and
// max_header_bytes from (default "server.http"). Use it to run a second HTTP
// server on its own config block, e.g. "server.admin" for an internal server.
//
// When constructing a server outside Register, pass the SAME prefix to both
// NewServer and Start so the listen port and TLS settings stay consistent.
func WithConfigPrefix(prefix string) ServerOption {
	return func(c *serverConfig) {
		c.prefix = prefix
	}
}

// WithPort sets the listen port explicitly, bypassing config lookup entirely.
// It has the highest precedence: it overrides both <prefix>.port and the
// server.port shared fallback.
func WithPort(port int) ServerOption {
	return func(c *serverConfig) {
		c.port = &port
	}
}

// WithMaxHeaderBytes overrides <prefix>.max_header_bytes and the built-in
// 1 MB default for the constructed server's MaxHeaderBytes.
func WithMaxHeaderBytes(n int) ServerOption {
	return func(c *serverConfig) {
		c.maxHeaderBytes = n
	}
}

// WithReadTimeout overrides the built-in http.Server ReadTimeout.
func WithReadTimeout(d time.Duration) ServerOption {
	return func(c *serverConfig) {
		c.readTimeout = d
	}
}

// WithWriteTimeout overrides the built-in http.Server WriteTimeout.
func WithWriteTimeout(d time.Duration) ServerOption {
	return func(c *serverConfig) {
		c.writeTimeout = d
	}
}

// WithIdleTimeout overrides the built-in http.Server IdleTimeout.
func WithIdleTimeout(d time.Duration) ServerOption {
	return func(c *serverConfig) {
		c.idleTimeout = d
	}
}

// WithServerTLSConfig replaces the default hardened *tls.Config on the
// constructed server. Cert/key resolution for serving still flows through
// Start (from the server's TLS config prefix). It is named distinctly from the
// client-side WithTLSConfig option in this package.
func WithServerTLSConfig(c *tls.Config) ServerOption {
	return func(sc *serverConfig) {
		sc.tlsConfig = c
	}
}

// NewServer returns a new preconfigured http.Server from explicit
// typed settings.
func NewServer(ctx context.Context, settings ServerSettings, handler http.Handler, opts ...ServerOption) (*http.Server, error) {
	sc := defaultServerConfig()
	for _, o := range opts {
		o(&sc)
	}

	return newServer(ctx, settings, handler, sc)
}

func newServer(ctx context.Context, settings ServerSettings, handler http.Handler, sc serverConfig) (*http.Server, error) {
	if sc.prefix == "" {
		return nil, errors.New("http: config prefix must not be empty")
	}

	port, err := resolvePort(settings, sc)
	if err != nil {
		return nil, err
	}

	maxHeaderBytes := sc.maxHeaderBytes
	if maxHeaderBytes == 0 {
		maxHeaderBytes = settings.MaxHeaderBytes
	}

	if maxHeaderBytes == 0 {
		maxHeaderBytes = defaultMaxHeaderBytes
	}

	tlsConfig := sc.tlsConfig
	if tlsConfig == nil {
		tlsConfig = gtbtls.DefaultConfig()
	}

	// Detach the per-request BaseContext from the construction context's
	// cancellation. Cancelling ctx (typically at shutdown) must not propagate
	// into already-accepted requests, which would cancel them mid-drain and
	// defeat Shutdown's graceful-drain guarantee. Values on ctx are preserved.
	baseCtx := context.WithoutCancel(ctx)

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
		BaseContext: func(_ net.Listener) context.Context {
			return baseCtx
		},
		Handler:        handler,
		ReadTimeout:    sc.readTimeout,
		WriteTimeout:   sc.writeTimeout,
		IdleTimeout:    sc.idleTimeout,
		MaxHeaderBytes: maxHeaderBytes,
		TLSConfig:      tlsConfig,
	}

	return srv, nil
}

// resolvePort returns the listen port: an explicit WithPort value if supplied
// (validated), otherwise the typed server settings port. A zero result is left
// permissive — the OS assigns an ephemeral port.
func resolvePort(settings ServerSettings, sc serverConfig) (int, error) {
	if sc.port != nil {
		if *sc.port < 0 || *sc.port > maxPort {
			return 0, errors.Newf("http: invalid port %d (must be 0-%d)", *sc.port, maxPort)
		}

		return *sc.port, nil
	}

	return settings.Port, nil
}

// serveState records the serve goroutine's exit error so Status can report a
// server that has died, rather than only checking that the *http.Server is
// non-nil (which is invariantly true).
type serveState struct {
	mu      sync.Mutex
	exitErr error
}

func (s *serveState) setExit(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.exitErr = err
}

func (s *serveState) status() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exitErr
}

func start(logger logger.Logger, srv *http.Server, tlsPair gtbtls.Pair, state *serveState) controls.StartFunc {
	return func(ctx context.Context) error {
		// Load the TLS material synchronously before serving so a misconfigured
		// or missing certificate fails the start (and the controller) loudly,
		// rather than failing asynchronously inside the serve goroutine while
		// the controller and /healthz report the server as healthy.
		if tlsPair.Enabled {
			tlsCfg, err := tlsPair.ServerConfig()
			if err != nil {
				return errors.Wrap(err, "loading TLS configuration")
			}

			srv.TLSConfig = tlsCfg
		}

		var lc net.ListenConfig

		ln, err := lc.Listen(ctx, "tcp", srv.Addr)
		if err != nil {
			return errors.Wrap(err, "failed to listen")
		}

		// Log the address the listener actually bound to, not srv.Addr: when
		// the configured port is 0 (ephemeral) srv.Addr is ":0", whereas
		// ln.Addr() reports the OS-assigned port.
		boundAddr := ln.Addr().String()

		go func() {
			var err error

			if tlsPair.Enabled {
				logger.Info("starting http server", "tls", true, "addr", boundAddr)

				err = srv.ServeTLS(ln, "", "") // certificate already loaded into srv.TLSConfig
			} else {
				logger.Info("starting http server", "tls", false, "addr", boundAddr)

				err = srv.Serve(ln)
			}

			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("HTTP server failed", "error", err)

				if state != nil {
					state.setExit(err)
				}
			}
		}()

		return nil
	}
}

// StartWithTLSPair returns a curried function suitable for use with the
// controls package from explicit TLS settings.
func StartWithTLSPair(logger logger.Logger, srv *http.Server, tlsPair gtbtls.Pair) controls.StartFunc {
	return start(logger, srv, tlsPair, nil)
}

// Stop returns a curried function suitable for use with the controls package.
// Shutdown is attempted first to drain in-flight requests. If the shutdown
// context expires (or Shutdown otherwise errors) the server is force-closed via
// Close so a hung handler cannot leave the listener and connections open,
// mirroring the gRPC transport's graceful-then-force-stop behaviour.
func Stop(logger logger.Logger, srv *http.Server) controls.StopFunc {
	return func(ctx context.Context) {
		logger.Info("stopping http server", "addr", srv.Addr)

		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("server shutdown failed, forcing close", "error", err)

			if cerr := srv.Close(); cerr != nil {
				logger.Error("server force close failed", "error", cerr)
			}
		}
	}
}

// Status returns a curried function suitable for use with the controls package.
// When state is non-nil it reports the serve goroutine's exit error, so a
// server whose serve loop has died is no longer reported as healthy.
func status(srv *http.Server, state *serveState) controls.StatusFunc {
	return func() error {
		if srv == nil {
			return errors.New("http server is nil")
		}

		if state != nil {
			return state.status()
		}

		return nil
	}
}

// Status returns a curried health function for a manually-wired server. It
// reports an error only when srv is nil; use the controller wiring (Serve) for
// serve-goroutine death detection.
func Status(srv *http.Server) controls.StatusFunc {
	return status(srv, nil)
}

// RegisterOption configures registration-only behaviour for an HTTP server
// (middleware chain, request-body limit). Server construction settings — port,
// prefix, timeouts — are ServerOption values; Register accepts both families.
type RegisterOption func(*registerConfig)

// registerConfig is the superset consumed by Register: it embeds the
// construction settings and adds the registration-only knobs.
type registerConfig struct {
	serverConfig
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

func register(
	ctx context.Context,
	id string,
	controller controls.Controllable,
	logger logger.Logger,
	handler http.Handler,
	settings ServerSettings,
	tlsPair gtbtls.Pair,
	rc registerConfig,
) (*http.Server, error) {
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

	srv, err := newServer(ctx, settings, mux, rc.serverConfig)
	if err != nil {
		return nil, err
	}

	state := &serveState{}
	controller.Register(id,
		controls.WithStart(start(logger, srv, tlsPair, state)),
		controls.WithStop(Stop(logger, srv)),
		controls.WithStatus(status(srv, state)),
	)

	return srv, nil
}

// Register creates a new HTTP server from explicit typed settings
// and registers it with the controller under the given id.
func Register(
	ctx context.Context,
	id string,
	controller controls.Controllable,
	logger logger.Logger,
	handler http.Handler,
	settings ServerSettings,
	tlsPair gtbtls.Pair,
	opts ...any,
) (*http.Server, error) {
	rc := registerConfig{
		serverConfig:        defaultServerConfig(),
		maxRequestBodyBytes: DefaultMaxRequestBodyBytes,
	}

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&rc.serverConfig)
		case RegisterOption:
			v(&rc)
		}
	}

	return register(ctx, id, controller, logger, handler, settings, tlsPair, rc)
}
