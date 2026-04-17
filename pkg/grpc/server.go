package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/cockroachdb/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// healthSource is the narrow interface required by RegisterHealthService: health
// query methods plus context access for the background update goroutine lifecycle.
type healthSource interface {
	controls.HealthReporter
	GetContext() context.Context
}

const healthUpdateInterval = 10 * time.Second

// DefaultMaxGRPCMessageBytes caps both send and receive message sizes on
// servers constructed via NewServer. Closes M-2 from
// docs/development/reports/security-audit-2026-04-17.md. Set to 1 MiB —
// tools with extraordinary message sizes can override via the explicit
// grpc.MaxRecvMsgSize / grpc.MaxSendMsgSize options passed to NewServer.
const DefaultMaxGRPCMessageBytes = 1 << 20 // 1 MiB

// NewServer returns a new preconfigured grpc.Server.
//
// Default gRPC options applied (before caller-supplied opts):
//   - grpc.MaxRecvMsgSize(DefaultMaxGRPCMessageBytes)
//   - grpc.MaxSendMsgSize(DefaultMaxGRPCMessageBytes)
//
// Caller-supplied grpc.ServerOption values in opt override the defaults
// (gRPC applies later options last, so a caller can raise or lower the
// limits explicitly).
func NewServer(cfg config.Containable, opt ...grpc.ServerOption) (*grpc.Server, error) {
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
	if cfg.GetBool("server.grpc.reflection") {
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

// Start returns a curried function suitable for use with the controls package.
// TLS configuration cascades: server.grpc.tls.* overrides server.tls.* shared defaults.
func Start(cfg config.Containable, logger logger.Logger, srv *grpc.Server) controls.StartFunc {
	portStr := cfg.GetInt("server.grpc.port")
	if portStr == 0 {
		portStr = cfg.GetInt("server.port")
	}

	port := fmt.Sprintf(":%d", portStr)
	tlsEnabled, cert, key := gtbhttp.ResolveTLSConfig(cfg, "server.grpc.tls")

	return func(ctx context.Context) error {
		var lc net.ListenConfig

		lis, err := lc.Listen(ctx, "tcp", port)
		if err != nil {
			return errors.Wrap(err, "failed to listen")
		}

		if tlsEnabled {
			tlsLis, tlsErr := wrapTLS(lis, cert, key)
			if tlsErr != nil {
				return tlsErr
			}

			lis = tlsLis

			logger.Info("starting gRPC server", "tls", true, "addr", port)
		} else {
			logger.Info("starting gRPC server", "tls", false, "addr", port)
		}

		go func() {
			if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				logger.Error("gRPC server failed", "error", err)
			}
		}()

		return nil
	}
}

// wrapTLS wraps a net.Listener with TLS using the shared hardened config
// and the provided certificate and key files.
func wrapTLS(lis net.Listener, certFile, keyFile string) (net.Listener, error) {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, errors.Wrap(err, "loading gRPC TLS certificate")
	}

	tlsCfg := gtbhttp.DefaultTLSConfig()
	tlsCfg.Certificates = []tls.Certificate{certificate}

	return tls.NewListener(lis, tlsCfg), nil
}

// TLSServerCredentials returns gRPC server credentials using the shared
// hardened TLS config. Use this when you need to pass credentials directly
// to grpc.NewServer via grpc.Creds() instead of using the Start function.
func TLSServerCredentials(certFile, keyFile string) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, errors.Wrap(err, "loading gRPC TLS certificate")
	}

	tlsCfg := gtbhttp.DefaultTLSConfig()
	tlsCfg.Certificates = []tls.Certificate{certificate}

	return credentials.NewTLS(tlsCfg), nil
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

// Status returns a curried function suitable for use with the controls package.
func Status(srv *grpc.Server) controls.StatusFunc {
	return func() error {
		if srv == nil {
			return errors.New("grpc server is nil")
		}

		return nil
	}
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

// Register creates a new gRPC server and registers it with the controller under the given id.
// The opts variadic accepts both grpc.ServerOption and RegisterOption values.
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, opts ...any) (*grpc.Server, error) {
	var rc registerConfig

	var serverOpts []grpc.ServerOption

	for _, o := range opts {
		switch v := o.(type) {
		case RegisterOption:
			v(&rc)
		case grpc.ServerOption:
			serverOpts = append(serverOpts, v)
		}
	}

	// Prepend interceptor chain options before explicit server options.
	if rc.chain != nil {
		serverOpts = append(rc.chain.ServerOptions(), serverOpts...)
	}

	srv, err := NewServer(cfg, serverOpts...)
	if err != nil {
		return nil, err
	}

	RegisterHealthService(srv, controller)

	controller.Register(id,
		controls.WithStart(Start(cfg, logger, srv)),
		controls.WithStop(Stop(logger, srv)),
		controls.WithStatus(Status(srv)),
	)

	return srv, nil
}
