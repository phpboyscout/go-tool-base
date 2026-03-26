package grpc

import (
	"context"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestLoggingInterceptor_Unary_DefaultFields(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf)

	info := &grpc.UnaryServerInfo{FullMethod: "/pkg.Service/DoThing"}
	handler := func(_ context.Context, _ any) (any, error) {
		return "ok", nil
	}

	resp, err := interceptor.Unary(context.Background(), "req", info, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)

	require.Equal(t, 1, buf.Len())
	entry := buf.Entries()[0]
	assert.Equal(t, logger.InfoLevel, entry.Level)
	assert.Equal(t, "rpc completed", entry.Message)

	kv := keyvalMap(entry.Keyvals)
	assert.Equal(t, "/pkg.Service/DoThing", kv["method"])
	assert.Equal(t, "OK", kv["code"])
	assert.Equal(t, "unary", kv["type"])
	assert.Contains(t, kv, "latency")
}

func TestLoggingInterceptor_Unary_ErrorLogsAtErrorLevel(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf)

	info := &grpc.UnaryServerInfo{FullMethod: "/pkg.Service/Fail"}
	handler := func(_ context.Context, _ any) (any, error) {
		return nil, status.Error(codes.Internal, "something broke")
	}

	_, err := interceptor.Unary(context.Background(), "req", info, handler)
	require.Error(t, err)

	require.Equal(t, 1, buf.Len())
	entry := buf.Entries()[0]
	assert.Equal(t, logger.ErrorLevel, entry.Level)

	kv := keyvalMap(entry.Keyvals)
	assert.Equal(t, "Internal", kv["code"])
}

func TestLoggingInterceptor_Unary_NonGRPCError(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf)

	info := &grpc.UnaryServerInfo{FullMethod: "/pkg.Service/Fail"}
	handler := func(_ context.Context, _ any) (any, error) {
		return nil, errors.New("plain error")
	}

	_, err := interceptor.Unary(context.Background(), "req", info, handler)
	require.Error(t, err)

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.Equal(t, "Unknown", kv["code"])
}

func TestLoggingInterceptor_Unary_WithLogLevel(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf, WithGRPCLogLevel(logger.DebugLevel))

	info := &grpc.UnaryServerInfo{FullMethod: "/pkg.Service/Do"}
	handler := func(_ context.Context, _ any) (any, error) { return "ok", nil }

	_, _ = interceptor.Unary(context.Background(), "req", info, handler)

	assert.Equal(t, logger.DebugLevel, buf.Entries()[0].Level)
}

func TestLoggingInterceptor_Unary_WithoutLatency(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf, WithoutGRPCLatency())

	info := &grpc.UnaryServerInfo{FullMethod: "/pkg.Service/Do"}
	handler := func(_ context.Context, _ any) (any, error) { return "ok", nil }

	_, _ = interceptor.Unary(context.Background(), "req", info, handler)

	kv := keyvalMap(buf.Entries()[0].Keyvals)
	assert.NotContains(t, kv, "latency")
}

func TestLoggingInterceptor_Unary_WithPathFilter(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf, WithGRPCPathFilter("/grpc.health.v1.Health/Check"))

	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}
	handler := func(_ context.Context, _ any) (any, error) { return "ok", nil }

	_, _ = interceptor.Unary(context.Background(), "req", info, handler)
	assert.Equal(t, 0, buf.Len(), "filtered path should not be logged")

	// Non-filtered path should log
	info2 := &grpc.UnaryServerInfo{FullMethod: "/pkg.Service/Do"}
	_, _ = interceptor.Unary(context.Background(), "req", info2, handler)
	assert.Equal(t, 1, buf.Len())
}

func TestLoggingInterceptor_Stream_DefaultFields(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf)

	info := &grpc.StreamServerInfo{FullMethod: "/pkg.Service/StreamThings"}
	handler := func(_ any, _ grpc.ServerStream) error { return nil }

	err := interceptor.Stream(nil, nil, info, handler)
	require.NoError(t, err)

	require.Equal(t, 1, buf.Len())
	entry := buf.Entries()[0]
	assert.Equal(t, logger.InfoLevel, entry.Level)

	kv := keyvalMap(entry.Keyvals)
	assert.Equal(t, "/pkg.Service/StreamThings", kv["method"])
	assert.Equal(t, "OK", kv["code"])
	assert.Equal(t, "stream", kv["type"])
}

func TestLoggingInterceptor_Stream_Error(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf)

	info := &grpc.StreamServerInfo{FullMethod: "/pkg.Service/StreamFail"}
	handler := func(_ any, _ grpc.ServerStream) error {
		return status.Error(codes.Unavailable, "service down")
	}

	err := interceptor.Stream(nil, nil, info, handler)
	require.Error(t, err)

	entry := buf.Entries()[0]
	assert.Equal(t, logger.ErrorLevel, entry.Level)

	kv := keyvalMap(entry.Keyvals)
	assert.Equal(t, "Unavailable", kv["code"])
}

func TestLoggingInterceptor_Stream_PathFilter(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	interceptor := LoggingInterceptor(buf, WithGRPCPathFilter("/grpc.health.v1.Health/Watch"))

	info := &grpc.StreamServerInfo{FullMethod: "/grpc.health.v1.Health/Watch"}
	handler := func(_ any, _ grpc.ServerStream) error { return nil }

	_ = interceptor.Stream(nil, nil, info, handler)
	assert.Equal(t, 0, buf.Len())
}

func TestLoggingInterceptor_ReturnsUnaryAndStream(t *testing.T) {
	t.Parallel()

	interceptor := LoggingInterceptor(logger.NewNoop())
	assert.NotNil(t, interceptor.Unary)
	assert.NotNil(t, interceptor.Stream)
}

// keyvalMap converts a flat keyval slice to a map for test assertions.
func keyvalMap(keyvals []any) map[string]any {
	m := make(map[string]any)
	for i := 0; i+1 < len(keyvals); i += 2 {
		if k, ok := keyvals[i].(string); ok {
			m[k] = keyvals[i+1]
		}
	}

	return m
}
