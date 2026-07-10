package grpc

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const rpcKeyvalCapacity = 10

// GRPCLoggingOption configures gRPC transport logging behaviour.
type GRPCLoggingOption func(*grpcLoggingConfig)

type grpcLoggingConfig struct {
	level      slog.Level
	logLatency bool
	pathFilter map[string]struct{}
}

func defaultGRPCLoggingConfig() grpcLoggingConfig {
	return grpcLoggingConfig{
		level:      slog.LevelInfo,
		logLatency: true,
	}
}

// WithGRPCLogLevel sets the log level for successful RPCs.
// Errors always log at slog.LevelError.
func WithGRPCLogLevel(level slog.Level) GRPCLoggingOption {
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
func LoggingInterceptor(l *slog.Logger, opts ...GRPCLoggingOption) Interceptor {
	cfg := defaultGRPCLoggingConfig()
	for _, o := range opts {
		o(&cfg)
	}

	return Interceptor{
		Unary:  unaryLoggingInterceptor(l, cfg),
		Stream: streamLoggingInterceptor(l, cfg),
	}
}

func unaryLoggingInterceptor(l *slog.Logger, cfg grpcLoggingConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, filtered := cfg.pathFilter[info.FullMethod]; filtered {
			return handler(ctx, req)
		}

		start := time.Now()
		resp, err := handler(ctx, req)

		emitRPCLog(ctx, l, cfg, info.FullMethod, "unary", start, err)

		return resp, err
	}
}

func streamLoggingInterceptor(l *slog.Logger, cfg grpcLoggingConfig) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if _, filtered := cfg.pathFilter[info.FullMethod]; filtered {
			return handler(srv, ss)
		}

		start := time.Now()
		err := handler(srv, ss)

		ctx := context.Background()
		if ss != nil {
			ctx = ss.Context()
		}

		emitRPCLog(ctx, l, cfg, info.FullMethod, "stream", start, err)

		return err
	}
}

func emitRPCLog(
	ctx context.Context,
	l *slog.Logger,
	cfg grpcLoggingConfig,
	method, rpcType string,
	start time.Time,
	err error,
) {
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
		level = slog.LevelError
	}

	keyvals := make([]any, 0, rpcKeyvalCapacity)
	keyvals = append(keyvals, "method", method, "code", code.String(), "type", rpcType)

	if cfg.logLatency {
		keyvals = append(keyvals, "latency", time.Since(start).String())
	}

	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		keyvals = append(keyvals, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}

	grpcLogAtLevel(ctx, l.With(keyvals...), level, "rpc completed")
}

func grpcLogAtLevel(ctx context.Context, l *slog.Logger, level slog.Level, msg string) {
	// slog emits at a dynamic level directly; request logging never terminates
	// the process, so there is no fatal level to special-case.
	l.Log(ctx, level, msg)
}
