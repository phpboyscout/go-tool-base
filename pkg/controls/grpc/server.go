package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/cockroachdb/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
  "github.com/phpboyscout/go-tool-base/pkg/logger"
)

// NewServer returns a new preconfigured grpc.Server.
func NewServer(cfg config.Containable, opt ...grpc.ServerOption) (*grpc.Server, error) {
	srv := grpc.NewServer(opt...)
	reflection.Register(srv)

	return srv, nil
}

// Start returns a curried function suitable for use with the controls package.
func Start(cfg config.Containable, logger *slog.Logger, srv *grpc.Server) controls.StartFunc {
	port := fmt.Sprintf(":%d", cfg.GetInt("server.port"))

	return func(ctx context.Context) error {
		var lc net.ListenConfig

		lis, err := lc.Listen(ctx, "tcp", port)
		if err != nil {
			return errors.Wrap(err, "failed to listen")
		}

		logger.Info(fmt.Sprintf("Starting gRPC server on %s", port))

		if err := srv.Serve(lis); err != nil {
			return errors.Wrap(err, "gRPC server failed")
		}

		return nil
	}
}

// Stop returns a curried function suitable for use with the controls package.
func Stop(logger *slog.Logger, srv *grpc.Server) controls.StopFunc {
	return func(_ context.Context) {
		logger.Info("Stopping gRPC server")
		srv.GracefulStop()
	}
}

// Status returns a curried function suitable for use with the controls package.
func Status() {}

// Register creates a new gRPC server and registers it with the controller under the given id.
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, opt ...grpc.ServerOption) error {
	srv, err := NewServer(cfg, opt...)
	if err != nil {
		return err
	}

	controller.Register(id,
		controls.WithStart(Start(cfg, logger, srv)),
		controls.WithStop(Stop(logger, srv)),
		controls.WithStatus(Status),
	)

	return nil
}
