package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// fakeServerStream is a minimal grpc.ServerStream carrying a context.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func okUnaryHandler(_ context.Context, _ any) (any, error) { return "ok", nil }

func okStreamHandler(_ any, _ grpc.ServerStream) error { return nil }

func TestDefaultRateLimitConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultRateLimitConfig()

	assert.InDelta(t, float64(defaultRateLimitRPS), cfg.RequestsPerSecond, 0)
	assert.Equal(t, defaultRateLimitBurst, cfg.Burst)
	assert.Nil(t, cfg.KeyFunc)
}

func TestRateLimitConfig_Normalized(t *testing.T) {
	t.Parallel()

	got := RateLimitConfig{RequestsPerSecond: -1, Burst: 0, MaxTrackedKeys: -3}.normalized()

	assert.InDelta(t, float64(defaultRateLimitRPS), got.RequestsPerSecond, 0)
	assert.Equal(t, defaultRateLimitBurst, got.Burst)
	assert.Positive(t, got.MaxTrackedKeys)
}

func TestRateLimitInterceptor_UnaryAdmitsThenRejects(t *testing.T) {
	t.Parallel()

	ic := RateLimitInterceptor(logger.ToSlog(logger.NewNoop()), RateLimitConfig{RequestsPerSecond: 1, Burst: 1})
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := ic.Unary(context.Background(), nil, info, okUnaryHandler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)

	_, err = ic.Unary(context.Background(), nil, info, okUnaryHandler)
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestRateLimitInterceptor_StreamAdmitsThenRejects(t *testing.T) {
	t.Parallel()

	ic := RateLimitInterceptor(logger.ToSlog(logger.NewNoop()), RateLimitConfig{RequestsPerSecond: 1, Burst: 1})
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}
	ss := &fakeServerStream{ctx: context.Background()}

	require.NoError(t, ic.Stream(nil, ss, info, okStreamHandler))

	err := ic.Stream(nil, ss, info, okStreamHandler)
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestRateLimitInterceptor_OnLimitedCallback(t *testing.T) {
	t.Parallel()

	var got string

	cfg := RateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
		OnLimited:         func(_ context.Context, fullMethod string) { got = fullMethod },
	}
	ic := RateLimitInterceptor(logger.ToSlog(logger.NewNoop()), cfg)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, _ = ic.Unary(context.Background(), nil, info, okUnaryHandler)
	_, _ = ic.Unary(context.Background(), nil, info, okUnaryHandler)

	assert.Equal(t, "/test.Service/Method", got, "OnLimited receives the rejected method")
}

func TestRateLimitInterceptor_PerPeerKey(t *testing.T) {
	t.Parallel()

	cfg := RateLimitConfig{RequestsPerSecond: 1, Burst: 1, KeyFunc: PeerKey}
	ic := RateLimitInterceptor(logger.ToSlog(logger.NewNoop()), cfg)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	ctxFor := func(ip string) context.Context {
		return peer.NewContext(context.Background(), &peer.Peer{
			Addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 1234},
		})
	}

	_, err := ic.Unary(ctxFor("10.0.0.1"), nil, info, okUnaryHandler)
	require.NoError(t, err, "first peer admitted")

	_, err = ic.Unary(ctxFor("10.0.0.2"), nil, info, okUnaryHandler)
	require.NoError(t, err, "distinct peer has its own bucket")

	_, err = ic.Unary(ctxFor("10.0.0.1"), nil, info, okUnaryHandler)
	require.Error(t, err, "first peer exhausted its bucket")
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestPeerKey(t *testing.T) {
	t.Parallel()

	// No peer in context -> empty key (shared bucket).
	assert.Empty(t, PeerKey(context.Background(), "/m"))

	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP("192.0.2.7"), Port: 9},
	})
	assert.Equal(t, "192.0.2.7:9", PeerKey(ctx, "/m"))
}
