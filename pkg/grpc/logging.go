package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// GRPCLoggingOption configures gRPC transport logging behaviour.
type GRPCLoggingOption func(*grpcLoggingConfig)

type grpcLoggingConfig struct {
	level      logger.Level
	logLatency bool
	pathFilter map[string]struct{}
}

func defaultGRPCLoggingConfig() grpcLoggingConfig {
	return grpcLoggingConfig{
		level:      logger.InfoLevel,
		logLatency: true,
	}
}

// WithGRPCLogLevel sets the log level for successful RPCs.
// Errors always log at logger.ErrorLevel.
func WithGRPCLogLevel(level logger.Level) GRPCLoggingOption {
	return func(c *grpcLoggingConfig) {
		c.level = level
	}
}

// WithoutGRPCLatency disables the "latency" field.
func WithoutGRPCLatency() GRPCLoggingOption {
	return func(c *grpcLoggingConfig) {
		c.logLatency = false
	}
}

// WithGRPCPathFilter excludes RPCs matching the given full method names from logging.
func WithGRPCPathFilter(methods ...string) GRPCLoggingOption {
	return func(c *grpcLoggingConfig) {
		if c.pathFilter == nil {
			c.pathFilter = make(map[string]struct{}, len(methods))
		}

		for _, m := range methods {
			c.pathFilter[m] = struct{}{}
		}
	}
}

// LoggingInterceptor returns an Interceptor (unary + stream) that logs
// each completed RPC.
func LoggingInterceptor(l logger.Logger, opts ...GRPCLoggingOption) Interceptor {
	cfg := defaultGRPCLoggingConfig()
	for _, o := range opts {
		o(&cfg)
	}

	return Interceptor{
		Unary:  unaryLoggingInterceptor(l, cfg),
		Stream: streamLoggingInterceptor(l, cfg),
	}
}

func unaryLoggingInterceptor(l logger.Logger, cfg grpcLoggingConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, filtered := cfg.pathFilter[info.FullMethod]; filtered {
			return handler(ctx, req)
		}

		start := time.Now()
		resp, err := handler(ctx, req)

		emitRPCLog(l, cfg, info.FullMethod, "unary", start, err)

		return resp, err
	}
}

func streamLoggingInterceptor(l logger.Logger, cfg grpcLoggingConfig) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if _, filtered := cfg.pathFilter[info.FullMethod]; filtered {
			return handler(srv, ss)
		}

		start := time.Now()
		err := handler(srv, ss)

		emitRPCLog(l, cfg, info.FullMethod, "stream", start, err)

		return err
	}
}

func emitRPCLog(l logger.Logger, cfg grpcLoggingConfig, method, rpcType string, start time.Time, err error) {
	code := codes.OK
	if err != nil {
		if s, ok := status.FromError(err); ok {
			code = s.Code()
		} else {
			code = codes.Unknown
		}
	}

	level := cfg.level
	if code != codes.OK {
		level = logger.ErrorLevel
	}

	keyvals := make([]any, 0, 10)
	keyvals = append(keyvals, "method", method, "code", code.String(), "type", rpcType)

	if cfg.logLatency {
		keyvals = append(keyvals, "latency", time.Since(start).String())
	}

	grpcLogAtLevel(l.With(keyvals...), level, "rpc completed")
}

func grpcLogAtLevel(l logger.Logger, level logger.Level, msg string) {
	switch level {
	case logger.DebugLevel:
		l.Debug(msg)
	case logger.WarnLevel:
		l.Warn(msg)
	case logger.ErrorLevel:
		l.Error(msg)
	case logger.FatalLevel:
		l.Fatal(msg)
	default:
		l.Info(msg)
	}
}
