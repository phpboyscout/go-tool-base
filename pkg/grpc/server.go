package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// healthSource is the narrow interface required by RegisterHealthService: health
// query methods plus context access for the background update goroutine lifecycle.
type healthSource interface {
	controls.HealthReporter
	GetContext() context.Context
}

const healthUpdateInterval = 10 * time.Second

// DefaultConfigPrefix is the config prefix a gRPC server reads (port,
// reflection, TLS) unless overridden with WithConfigPrefix. Use it to run a
// second gRPC server on its own config block, e.g. "server.internal".
const DefaultConfigPrefix = "server.grpc"

// maxPort is the highest valid TCP port number.
const maxPort = 65535

// ConfigKeySharedPort is the shared fallback port used when the per-server
// port key (<prefix>.port) is unset.
const ConfigKeySharedPort = "server.port"

// serverConfig carries the construction settings consumed by NewServer, Start
// and DialLocal. A zero field falls back to config or the built-in default.
type serverConfig struct {
	prefix string
	port   *int
}

// defaultServerConfig returns the baseline settings: the default config prefix.
func defaultServerConfig() serverConfig {
	return serverConfig{prefix: DefaultConfigPrefix}
}

func (c serverConfig) tlsPrefix() string { return c.prefix + ".tls" }

// ServerSettings contains the data needed to construct and start a gRPC server
// without binding the core transport path to any particular config system.
type ServerSettings struct {
	Port       int  `mapstructure:"port" yaml:"port" json:"port"`
	Reflection bool `mapstructure:"reflection" yaml:"reflection" json:"reflection"`
}

// ServerOption configures the config prefix and port a gRPC server reads.
// ServerOption values are accepted by NewServer, Start, DialLocal and Register
// (alongside grpc.ServerOption / grpc.DialOption values).
type ServerOption func(*serverConfig)

// WithConfigPrefix sets the config prefix the server reads its port, reflection
// and TLS settings from (default "server.grpc"). Pass the SAME prefix to
// NewServer, Start and DialLocal to keep a non-default server consistent.
func WithConfigPrefix(prefix string) ServerOption {
	return func(c *serverConfig) {
		c.prefix = prefix
	}
}

// WithPort sets the listen (or dial) port explicitly, bypassing config lookup.
// It overrides both <prefix>.port and the server.port shared fallback.
func WithPort(port int) ServerOption {
	return func(c *serverConfig) {
		c.port = &port
	}
}

// resolvePort returns the port: an explicit WithPort value if supplied
// (validated), otherwise the typed server settings port.
func resolvePort(settings ServerSettings, sc serverConfig) (int, error) {
	if sc.port != nil {
		if *sc.port < 0 || *sc.port > maxPort {
			return 0, errors.Newf("grpc: invalid port %d (must be 0-%d)", *sc.port, maxPort)
		}

		return *sc.port, nil
	}

	return settings.Port, nil
}

// DefaultMaxGRPCMessageBytes caps both send and receive message sizes on
// servers constructed via NewServer. Closes M-2 from
// docs/development/reports/security-audit-2026-04-17.md. Set to 1 MiB —
// tools with extraordinary message sizes can override via the explicit
// grpc.MaxRecvMsgSize / grpc.MaxSendMsgSize options passed to NewServer.
const DefaultMaxGRPCMessageBytes = 1 << 20 // 1 MiB

// NewServer returns a new preconfigured grpc.Server from explicit
// typed settings.
//
// Default gRPC options applied (before caller-supplied opts):
//   - grpc.MaxRecvMsgSize(DefaultMaxGRPCMessageBytes)
//   - grpc.MaxSendMsgSize(DefaultMaxGRPCMessageBytes)
//
// Caller-supplied grpc.ServerOption values override the defaults (gRPC applies
// later options last, so a caller can raise or lower the limits explicitly).
//
// The opts variadic accepts both ServerOption values (e.g. WithConfigPrefix,
// retained for compatibility with adapter call sites) and grpc.ServerOption
// values; other types are ignored.
func NewServer(settings ServerSettings, opts ...any) (*grpc.Server, error) {
	sc := defaultServerConfig()

	var serverOpts []grpc.ServerOption

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case grpc.ServerOption:
			serverOpts = append(serverOpts, v)
		}
	}

	return newServer(settings, sc, serverOpts...)
}

func newServer(settings ServerSettings, sc serverConfig, opt ...grpc.ServerOption) (*grpc.Server, error) {
	if sc.prefix == "" {
		return nil, errors.New("grpc: config prefix must not be empty")
	}

	// numDefaultServerOpts is the count of default grpc.ServerOption
	// values prepended before caller-supplied opts: MaxRecvMsgSize and
	// MaxSendMsgSize.
	const numDefaultServerOpts = 2

	allOpts := make([]grpc.ServerOption, 0, numDefaultServerOpts+len(opt))
	allOpts = append(allOpts,
		grpc.MaxRecvMsgSize(DefaultMaxGRPCMessageBytes),
		grpc.MaxSendMsgSize(DefaultMaxGRPCMessageBytes),
	)
	allOpts = append(allOpts, opt...)

	srv := grpc.NewServer(allOpts...)
	if settings.Reflection {
		reflection.Register(srv)
	}

	return srv, nil
}

// RegisterHealthService registers the standard gRPC health service with the provided server,
// wired to the controller's status.
func RegisterHealthService(srv *grpc.Server, controller healthSource) {
	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, healthSrv)

	update := func() {
		// Update default status
		report := controller.Status()

		status := grpc_health_v1.HealthCheckResponse_SERVING
		if !report.OverallHealthy {
			status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
		}

		healthSrv.SetServingStatus("", status)

		// Update liveness status
		liveReport := controller.Liveness()

		liveStatus := grpc_health_v1.HealthCheckResponse_SERVING
		if !liveReport.OverallHealthy {
			liveStatus = grpc_health_v1.HealthCheckResponse_NOT_SERVING
		}

		healthSrv.SetServingStatus("liveness", liveStatus)

		// Update readiness status
		readyReport := controller.Readiness()

		readyStatus := grpc_health_v1.HealthCheckResponse_SERVING
		if !readyReport.OverallHealthy {
			readyStatus = grpc_health_v1.HealthCheckResponse_NOT_SERVING
		}

		healthSrv.SetServingStatus("readiness", readyStatus)
	}

	// Update immediately
	update()

	// Update health status based on controller status
	go func() {
		for {
			select {
			case <-controller.GetContext().Done():
				return
			case <-time.After(healthUpdateInterval):
				update()
			}
		}
	}()
}

// Start returns a curried function suitable for use with the
// controls package from explicit typed server and TLS settings.
func Start(logger logger.Logger, srv *grpc.Server, settings ServerSettings, tlsPair gtbtls.Pair, opts ...ServerOption) controls.StartFunc {
	sc := defaultServerConfig()
	for _, o := range opts {
		o(&sc)
	}

	if sc.prefix == "" {
		sc.prefix = DefaultConfigPrefix
	}

	return start(logger, srv, settings, tlsPair, sc, nil)
}

// serveState records the serve goroutine's exit error so Status can report a
// gRPC server whose serve loop has died, rather than only checking that the
// *grpc.Server is non-nil (which is invariantly true).
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

func start(logger logger.Logger, srv *grpc.Server, settings ServerSettings, tlsPair gtbtls.Pair, sc serverConfig, state *serveState) controls.StartFunc {
	portNum, portErr := resolvePort(settings, sc)

	return func(ctx context.Context) error {
		if portErr != nil {
			return portErr
		}

		var lc net.ListenConfig

		lis, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", portNum))
		if err != nil {
			return errors.Wrap(err, "failed to listen")
		}

		// Log the bound address, not the configured port: when the port is 0
		// (ephemeral) the listener reports the OS-assigned port.
		boundAddr := lis.Addr().String()

		if tlsPair.Enabled {
			tlsLis, tlsErr := wrapTLS(lis, tlsPair)
			if tlsErr != nil {
				return tlsErr
			}

			lis = tlsLis

			logger.Info("starting gRPC server", "tls", true, "addr", boundAddr)
		} else {
			logger.Info("starting gRPC server", "tls", false, "addr", boundAddr)
		}

		go func() {
			if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				logger.Error("gRPC server failed", "error", err)

				if state != nil {
					state.setExit(err)
				}
			}
		}()

		return nil
	}
}

// wrapTLS wraps a net.Listener with TLS using the shared hardened config and
// the certificate described by the pair.
//
// It advertises HTTP/2 via ALPN ("h2"). Without this the raw TLS listener does
// not negotiate h2, and grpc-go >= 1.67 clients (including the grpc-gateway)
// reject the connection with "missing selected ALPN property". The grpc.Creds
// path gets this for free via credentials.NewTLS; the listener path does not,
// so Pair.ServerConfig sets it explicitly here.
func wrapTLS(lis net.Listener, pair gtbtls.Pair) (net.Listener, error) {
	tlsCfg, err := pair.ServerConfig("h2")
	if err != nil {
		return nil, errors.Wrap(err, "configuring gRPC TLS")
	}

	return tls.NewListener(lis, tlsCfg), nil
}

// TLSServerCredentials returns gRPC server credentials using the shared
// hardened TLS config. Use this when you need to pass credentials directly
// to grpc.NewServer via grpc.Creds() instead of using the Start function.
// (credentials.NewTLS advertises h2 itself, so no explicit ALPN is needed.)
func TLSServerCredentials(certFile, keyFile string) (credentials.TransportCredentials, error) {
	tlsCfg, err := gtbtls.Pair{Cert: certFile, Key: keyFile}.ServerConfig()
	if err != nil {
		return nil, errors.Wrap(err, "configuring gRPC TLS")
	}

	return credentials.NewTLS(tlsCfg), nil
}

// TLSClientCredentials returns gRPC client transport credentials that trust the
// given CA/cert files. It is the client-side mirror of TLSServerCredentials —
// e.g. for the grpc-gateway dialing a gRPC server that presents a self-signed
// or private-CA certificate. With no files it trusts the system roots.
func TLSClientCredentials(caFiles ...string) (credentials.TransportCredentials, error) {
	tlsCfg, err := gtbtls.ClientConfig(caFiles...)
	if err != nil {
		return nil, err
	}

	return credentials.NewTLS(tlsCfg), nil
}

// DialLocal dials a local gRPC server using explicit typed server
// and TLS settings.
func DialLocal(settings ServerSettings, tlsPair gtbtls.Pair, opts ...any) (*grpc.ClientConn, error) {
	sc := defaultServerConfig()

	var dialOpts []grpc.DialOption

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case grpc.DialOption:
			dialOpts = append(dialOpts, v)
		}
	}

	return dialLocal(settings, tlsPair, sc, dialOpts...)
}

func dialLocal(settings ServerSettings, tlsPair gtbtls.Pair, sc serverConfig, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	port, err := resolvePort(settings, sc)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("localhost:%d", port)

	security := grpc.WithTransportCredentials(insecure.NewCredentials())

	if tlsPair.Enabled {
		creds, credErr := TLSClientCredentials(tlsPair.Cert)
		if credErr != nil {
			return nil, credErr
		}

		security = grpc.WithTransportCredentials(creds)
	}

	return grpc.NewClient(endpoint, append([]grpc.DialOption{security}, opts...)...)
}

// Stop returns a curried function suitable for use with the controls package.
// GracefulStop is attempted first to allow in-flight RPCs to finish. If the
// shutdown context expires (or if Serve has not been called yet, which would
// cause GracefulStop to block indefinitely), the server is force-stopped.
func Stop(logger logger.Logger, srv *grpc.Server) controls.StopFunc {
	return func(ctx context.Context) {
		logger.Info("Stopping gRPC server")

		done := make(chan struct{})

		go func() {
			srv.GracefulStop()
			close(done)
		}()

		select {
		case <-done:
			// Graceful shutdown completed within the timeout.
		case <-ctx.Done():
			logger.Warn("gRPC graceful stop timed out, forcing stop")
			srv.Stop()
		}
	}
}

// status reports the serve goroutine's exit error when state is non-nil, so a
// gRPC server whose serve loop has died is no longer reported as healthy.
func serverStatus(srv *grpc.Server, state *serveState) controls.StatusFunc {
	return func() error {
		if srv == nil {
			return errors.New("grpc server is nil")
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
func Status(srv *grpc.Server) controls.StatusFunc {
	return serverStatus(srv, nil)
}

// RegisterOption configures optional behaviour for gRPC server registration.
type RegisterOption func(*registerConfig)

type registerConfig struct {
	chain *InterceptorChain
}

// WithInterceptors prepends the given interceptor chain before any
// grpc.ServerOption interceptors passed via the variadic opts.
func WithInterceptors(chain InterceptorChain) RegisterOption {
	return func(c *registerConfig) {
		c.chain = &chain
	}
}

func register(
	id string,
	controller controls.Controllable,
	logger logger.Logger,
	settings ServerSettings,
	tlsPair gtbtls.Pair,
	sc serverConfig,
	rc registerConfig,
	serverOpts []grpc.ServerOption,
) (*grpc.Server, error) {
	// Prepend interceptor chain options before explicit server options.
	if rc.chain != nil {
		serverOpts = append(rc.chain.ServerOptions(), serverOpts...)
	}

	srv, err := newServer(settings, sc, serverOpts...)
	if err != nil {
		return nil, err
	}

	RegisterHealthService(srv, controller)

	state := &serveState{}
	controller.Register(id,
		controls.WithStart(start(logger, srv, settings, tlsPair, sc, state)),
		controls.WithStop(Stop(logger, srv)),
		controls.WithStatus(serverStatus(srv, state)),
	)

	return srv, nil
}

// Register creates a new gRPC server from explicit typed settings
// and registers it with the controller under the given id.
func Register(id string, controller controls.Controllable, logger logger.Logger, settings ServerSettings, tlsPair gtbtls.Pair, opts ...any) (*grpc.Server, error) {
	sc := defaultServerConfig()

	var rc registerConfig

	var serverOpts []grpc.ServerOption

	for _, o := range opts {
		switch v := o.(type) {
		case ServerOption:
			v(&sc)
		case RegisterOption:
			v(&rc)
		case grpc.ServerOption:
			serverOpts = append(serverOpts, v)
		}
	}

	return register(id, controller, logger, settings, tlsPair, sc, rc, serverOpts)
}
