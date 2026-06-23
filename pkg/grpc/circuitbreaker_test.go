package grpc

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// testClock is a goroutine-safe manually-advanced clock.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock {
	return &testClock{t: time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)}
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func unaryInvoker(err error, calls *atomic.Int64) grpc.UnaryInvoker {
	return func(_ context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		calls.Add(1)

		return err
	}
}

// fakeClientStream returns fixed errors from RecvMsg/SendMsg.
type fakeClientStream struct {
	grpc.ClientStream
	recvErr error
	sendErr error
}

func (f *fakeClientStream) RecvMsg(any) error { return f.recvErr }
func (f *fakeClientStream) SendMsg(any) error { return f.sendErr }

func streamerReturning(stream grpc.ClientStream, err error, calls *atomic.Int64) grpc.Streamer {
	return func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		if calls != nil {
			calls.Add(1)
		}

		return stream, err
	}
}

func TestCircuitState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "open", StateOpen.String())
	assert.Equal(t, "half-open", StateHalfOpen.String())
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultCircuitBreakerConfig()

	assert.Equal(t, 5, cfg.FailureThreshold)
	assert.Equal(t, 30*time.Second, cfg.Cooldown)
	assert.Equal(t, 1, cfg.HalfOpenMaxRequests)
	assert.Nil(t, cfg.IsFailure)
}

func TestDefaultGRPCIsFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil ok", err: nil, want: false},
		{name: "unavailable", err: status.Error(codes.Unavailable, ""), want: true},
		{name: "deadline", err: status.Error(codes.DeadlineExceeded, ""), want: true},
		{name: "resource exhausted not a failure", err: status.Error(codes.ResourceExhausted, ""), want: false},
		{name: "internal not a failure", err: status.Error(codes.Internal, ""), want: false},
		{name: "invalid arg not a failure", err: status.Error(codes.InvalidArgument, ""), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, defaultGRPCIsFailure(tt.err))
		})
	}
}

func TestUnaryBreaker_OpensAndRejects(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	ic := CircuitBreakerInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 2, Cooldown: time.Minute})
	invoker := unaryInvoker(status.Error(codes.Unavailable, "down"), &calls)

	for range 2 {
		err := ic(context.Background(), "/m", nil, nil, nil, invoker)
		require.Equal(t, codes.Unavailable, status.Code(err))
	}

	callsBefore := calls.Load()

	// Open: rejected with Unavailable, invoker not called.
	err := ic(context.Background(), "/m", nil, nil, nil, invoker)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Equal(t, callsBefore, calls.Load(), "open breaker must not call invoker")
}

func TestUnaryBreaker_ResourceExhaustedDoesNotTrip(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	ic := CircuitBreakerInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 2, Cooldown: time.Minute})
	invoker := unaryInvoker(status.Error(codes.ResourceExhausted, "slow down"), &calls)

	for range 5 {
		err := ic(context.Background(), "/m", nil, nil, nil, invoker)
		require.Equal(t, codes.ResourceExhausted, status.Code(err))
	}

	assert.Equal(t, int64(5), calls.Load(), "ResourceExhausted never trips the breaker")
}

func TestUnaryBreaker_HalfOpenRecovery(t *testing.T) {
	t.Parallel()

	clock := newTestClock()

	var down atomic.Bool
	down.Store(true)

	ic := newCircuitBreakerInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 1, Cooldown: 30 * time.Second}, clock.now)
	invoker := func(_ context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		if down.Load() {
			return status.Error(codes.Unavailable, "down")
		}

		return nil
	}

	require.Equal(t, codes.Unavailable, status.Code(ic(context.Background(), "/m", nil, nil, nil, invoker))) // trip

	// Before cooldown: rejected.
	require.Equal(t, codes.Unavailable, status.Code(ic(context.Background(), "/m", nil, nil, nil, invoker)))

	// After cooldown the downstream is healthy: trial succeeds -> closed.
	clock.add(31 * time.Second)
	down.Store(false)
	require.NoError(t, ic(context.Background(), "/m", nil, nil, nil, invoker))
	require.NoError(t, ic(context.Background(), "/m", nil, nil, nil, invoker))
}

func TestStreamBreaker_EstablishmentFailureCounts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	ic := CircuitBreakerStreamInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 1, Cooldown: time.Minute})
	streamer := streamerReturning(nil, status.Error(codes.Unavailable, "down"), &calls)

	_, err := ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	require.Equal(t, codes.Unavailable, status.Code(err))

	// Breaker opened from the single establishment failure (threshold 1).
	_, err = ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Equal(t, int64(1), calls.Load(), "open breaker does not establish a new stream")
}

func TestStreamBreaker_PerMessageFailureCounts(t *testing.T) {
	t.Parallel()

	// Stream establishes fine, but RecvMsg returns Unavailable: that per-message
	// failure must count against the breaker (OQ3 — not establishment-only).
	stream := &fakeClientStream{recvErr: status.Error(codes.Unavailable, "mid-stream")}
	ic := CircuitBreakerStreamInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 1, Cooldown: time.Minute})
	streamer := streamerReturning(stream, nil, nil)

	cs, err := ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	require.NoError(t, err)

	// Drain one message -> records the failure.
	require.Equal(t, codes.Unavailable, status.Code(cs.RecvMsg(nil)))

	// Breaker now open: next establishment rejected.
	_, err = ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestStreamBreaker_EOFIsSuccess(t *testing.T) {
	t.Parallel()

	stream := &fakeClientStream{recvErr: io.EOF}
	ic := CircuitBreakerStreamInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 1, Cooldown: time.Minute})
	streamer := streamerReturning(stream, nil, nil)

	cs, err := ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	require.NoError(t, err)
	require.ErrorIs(t, cs.RecvMsg(nil), io.EOF) // clean close = success

	// Still closed: a fresh stream establishes.
	var calls atomic.Int64
	streamer2 := streamerReturning(&fakeClientStream{recvErr: io.EOF}, nil, &calls)
	_, err = ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), calls.Load())
}

func TestStreamBreaker_SendMsgFailureCounts(t *testing.T) {
	t.Parallel()

	// A SendMsg error (non-EOF) is a per-message failure and must count.
	stream := &fakeClientStream{sendErr: status.Error(codes.Unavailable, "send broke")}
	ic := CircuitBreakerStreamInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 1, Cooldown: time.Minute})
	streamer := streamerReturning(stream, nil, nil)

	cs, err := ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	require.NoError(t, err)
	require.Equal(t, codes.Unavailable, status.Code(cs.SendMsg(nil)))

	// Breaker opened from the send failure.
	_, err = ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestStreamBreaker_SendMsgEOFNotCounted(t *testing.T) {
	t.Parallel()

	// io.EOF from SendMsg means the RPC completed; the real status comes via
	// RecvMsg, so SendMsg's EOF is not itself a breaker failure.
	stream := &fakeClientStream{sendErr: io.EOF}
	ic := CircuitBreakerStreamInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 1, Cooldown: time.Minute})
	streamer := streamerReturning(stream, nil, nil)

	cs, err := ic(context.Background(), &grpc.StreamDesc{}, nil, "/m", streamer)
	require.NoError(t, err)
	require.ErrorIs(t, cs.SendMsg(nil), io.EOF)

	// Still closed: a fresh stream establishes.
	var calls atomic.Int64
	_, err = ic(context.Background(), &grpc.StreamDesc{}, nil, "/m",
		streamerReturning(&fakeClientStream{recvErr: io.EOF}, nil, &calls))
	require.NoError(t, err)
	assert.Equal(t, int64(1), calls.Load())
}

func TestUnaryBreaker_OnStateChangeFires(t *testing.T) {
	t.Parallel()

	var (
		mu          sync.Mutex
		transitions []string
	)

	cfg := CircuitBreakerConfig{
		FailureThreshold: 1,
		Cooldown:         time.Minute,
		OnStateChange: func(from, to CircuitState) {
			mu.Lock()
			defer mu.Unlock()
			transitions = append(transitions, from.String()+"->"+to.String())
		},
	}
	ic := CircuitBreakerInterceptor(logger.NewNoop(), cfg)
	var calls atomic.Int64
	_ = ic(context.Background(), "/m", nil, nil, nil, unaryInvoker(status.Error(codes.Unavailable, ""), &calls))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"closed->open"}, transitions)
}

func TestUnaryBreaker_ConcurrentRace(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	ic := CircuitBreakerInterceptor(logger.NewNoop(), CircuitBreakerConfig{FailureThreshold: 3, Cooldown: time.Millisecond})
	invoker := unaryInvoker(status.Error(codes.Unavailable, "down"), &calls)

	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)

		go func() {
			defer wg.Done()
			_ = ic(context.Background(), "/m", nil, nil, nil, invoker)
		}()
	}

	wg.Wait()
}
