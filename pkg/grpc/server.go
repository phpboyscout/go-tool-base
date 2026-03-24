package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cockroachdb/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const healthUpdateInterval = 10 * time.Second

// NewServer returns a new preconfigured grpc.Server.
func NewServer(cfg config.Containable, opt ...grpc.ServerOption) (*grpc.Server, error) {
	srv := grpc.NewServer(opt...)
	if cfg.GetBool("server.grpc.reflection") {
		reflection.Register(srv)
	}

	return srv, nil
}

// RegisterHealthService registers the standard gRPC health service with the provided server,
// wired to the controller's status.
func RegisterHealthService(srv *grpc.Server, controller controls.Controllable) {
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
func Start(cfg config.Containable, logger logger.Logger, srv *grpc.Server) controls.StartFunc {
	portStr := cfg.GetInt("server.grpc.port")
	if portStr == 0 {
		portStr = cfg.GetInt("server.port")
	}

	port := fmt.Sprintf(":%d", portStr)

	return func(ctx context.Context) error {
		var lc net.ListenConfig

		lis, err := lc.Listen(ctx, "tcp", port)
		if err != nil {
			return errors.Wrap(err, "failed to listen")
		}

		logger.Info(fmt.Sprintf("Starting gRPC server on %s", port))

		go func() {
			if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				logger.Error("gRPC server failed", "error", err)
			}
		}()

		return nil
	}
}

// Stop returns a curried function suitable for use with the controls package.
func Stop(logger logger.Logger, srv *grpc.Server) controls.StopFunc {
	return func(_ context.Context) {
		logger.Info("Stopping gRPC server")
		srv.GracefulStop()
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

// Register creates a new gRPC server and registers it with the controller under the given id.
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, opt ...grpc.ServerOption) (*grpc.Server, error) {
	srv, err := NewServer(cfg, opt...)
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
