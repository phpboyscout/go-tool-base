package grpc

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gitlab.com/phpboyscout/go-tool-base/internal/circuitbreaker"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// CircuitState is the client circuit breaker's state.
type CircuitState int

// The public states are derived from the shared core so the two enumerations
// can never silently drift out of order.
const (
	// StateClosed admits all RPCs; failures are counted.
	StateClosed = CircuitState(circuitbreaker.StateClosed)
	// StateOpen rejects all RPCs immediately with codes.Unavailable until the
	// cooldown elapses, then transitions to StateHalfOpen.
	StateOpen = CircuitState(circuitbreaker.StateOpen)
	// StateHalfOpen admits a limited number of trial RPCs; success closes the
	// breaker, failure re-opens it.
	StateHalfOpen = CircuitState(circuitbreaker.StateHalfOpen)
)

// String renders the state for logging.
func (s CircuitState) String() string {
	return circuitbreaker.State(s).String()
}

// errCircuitOpen is the immutable gRPC error returned while the breaker is open.
// It uses codes.Unavailable so it is indistinguishable on the wire from a
// genuine downstream outage — the correct semantic. It is a package-level value
// because the open state is a high-volume reject path; reallocating a status per
// rejected call would be pure waste.
var errCircuitOpen = status.Error(codes.Unavailable, "circuit breaker is open")

// CircuitBreakerConfig configures the client-side circuit breaker for gRPC.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures (in Closed) that
	// trips the breaker open. Must be >= 1. Default: 5.
	FailureThreshold int `mapstructure:"failure_threshold" yaml:"failure_threshold" json:"failure_threshold"`
	// Cooldown is how long the breaker stays Open before a trial. Default: 30s.
	Cooldown time.Duration `mapstructure:"cooldown" yaml:"cooldown" json:"cooldown"`
	// HalfOpenMaxRequests is the number of trial RPCs allowed in HalfOpen.
	// Must be >= 1. Default: 1.
	HalfOpenMaxRequests int `mapstructure:"half_open_max_requests" yaml:"half_open_max_requests" json:"half_open_max_requests"`

	// IsFailure classifies an RPC outcome. When nil, the default treats
	// Unavailable and DeadlineExceeded as failures and every other code
	// (including OK and ResourceExhausted) as a success.
	//
	// ResourceExhausted is deliberately NOT a failure: like an HTTP 429 it means
	// "you are being rate-limited", which is the retry/backoff layer's concern,
	// not a signal that the downstream is unhealthy. Counting it would let a
	// server's own rate limiter trip its callers' breakers. Supply a custom
	// IsFailure to change this.
	IsFailure func(err error) bool `mapstructure:"-" yaml:"-" json:"-"`

	// OnStateChange is invoked on every state transition. Optional; transitions
	// are also logged via the constructor's logger.
	OnStateChange func(from, to CircuitState) `mapstructure:"-" yaml:"-" json:"-"`
}

// CircuitBreakerConfigOverrides records which typed circuit breaker config
// fields were explicitly supplied by an adapter.
type CircuitBreakerConfigOverrides struct {
	FailureThreshold    bool
	Cooldown            bool
	HalfOpenMaxRequests bool
}

// DefaultCircuitBreakerConfig returns: threshold 5, cooldown 30s, half-open
// trial 1, default Unavailable/DeadlineExceeded failure classification.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    circuitbreaker.DefaultFailureThreshold,
		Cooldown:            circuitbreaker.DefaultCooldown,
		HalfOpenMaxRequests: circuitbreaker.DefaultHalfOpenMax,
	}
}

// MergeCircuitBreakerConfig applies explicitly supplied typed override values
// to base while leaving code-only function fields under caller control.
func MergeCircuitBreakerConfig(base, override CircuitBreakerConfig, fields CircuitBreakerConfigOverrides) CircuitBreakerConfig {
	if fields.FailureThreshold {
		base.FailureThreshold = override.FailureThreshold
	}

	if fields.Cooldown {
		base.Cooldown = override.Cooldown
	}

	if fields.HalfOpenMaxRequests {
		base.HalfOpenMaxRequests = override.HalfOpenMaxRequests
	}

	return base
}

// defaultGRPCIsFailure counts only Unavailable and DeadlineExceeded as breaker
// failures. See CircuitBreakerConfig.IsFailure for the ResourceExhausted note.
func defaultGRPCIsFailure(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

// newBreaker builds the shared core from a public config, wiring logging and the
// optional caller callback into OnStateChange. now is injected for tests.
func newBreaker(log logger.Logger, cfg CircuitBreakerConfig, now func() time.Time) (*circuitbreaker.Breaker, func(error) bool) {
	isFailure := cfg.IsFailure
	if isFailure == nil {
		isFailure = defaultGRPCIsFailure
	}

	br := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold:    cfg.FailureThreshold,
		Cooldown:            cfg.Cooldown,
		HalfOpenMaxRequests: cfg.HalfOpenMaxRequests,
		Now:                 now,
		OnStateChange: func(from, to circuitbreaker.State) {
			log.Debug("circuit breaker state change", "from", from.String(), "to", to.String())

			if cfg.OnStateChange != nil {
				cfg.OnStateChange(CircuitState(from), CircuitState(to))
			}
		},
	})

	return br, isFailure
}

// CircuitBreakerInterceptor returns a unary client interceptor that opens when a
// downstream is consistently failing and rejects calls with codes.Unavailable
// while open, avoiding wasted calls against a service known to be down. Install
// it via grpc.WithChainUnaryInterceptor.
func CircuitBreakerInterceptor(log logger.Logger, cfg CircuitBreakerConfig) grpc.UnaryClientInterceptor {
	return newCircuitBreakerInterceptor(log, cfg, nil)
}

func newCircuitBreakerInterceptor(log logger.Logger, cfg CircuitBreakerConfig, now func() time.Time) grpc.UnaryClientInterceptor {
	br, isFailure := newBreaker(log, cfg, now)

	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		done, allowed := br.Allow()
		if !allowed {
			return errCircuitOpen
		}

		err := invoker(ctx, method, req, reply, cc, opts...)
		done(isFailure(err))

		return err
	}
}

// CircuitBreakerStreamInterceptor returns a stream client interceptor with the
// same breaker semantics. Unlike a naive establishment-only breaker, it wraps
// the ClientStream so per-message errors (a RecvMsg/SendMsg that returns a
// classified failure) also count against the breaker; a clean io.EOF closes the
// stream as a success.
func CircuitBreakerStreamInterceptor(log logger.Logger, cfg CircuitBreakerConfig) grpc.StreamClientInterceptor {
	return newCircuitBreakerStreamInterceptor(log, cfg, nil)
}

func newCircuitBreakerStreamInterceptor(log logger.Logger, cfg CircuitBreakerConfig, now func() time.Time) grpc.StreamClientInterceptor {
	br, isFailure := newBreaker(log, cfg, now)

	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		done, allowed := br.Allow()
		if !allowed {
			return nil, errCircuitOpen
		}

		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			// Establishment failure is a terminal outcome for this trial.
			done(isFailure(err))

			return nil, err
		}

		return &breakerClientStream{
			ClientStream: stream,
			done:         done,
			isFailure:    isFailure,
		}, nil
	}
}

// breakerClientStream wraps a ClientStream to report the stream's terminal
// outcome to the breaker exactly once. A RecvMsg returning io.EOF is the normal
// end-of-stream (success); any other terminal error is classified by isFailure.
type breakerClientStream struct {
	grpc.ClientStream

	done      func(failure bool)
	isFailure func(error) bool
	finished  sync.Once
}

func (s *breakerClientStream) finish(failure bool) {
	s.finished.Do(func() { s.done(failure) })
}

func (s *breakerClientStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil {
		if errors.Is(err, io.EOF) {
			s.finish(false)
		} else {
			s.finish(s.isFailure(err))
		}
	}

	return err
}

func (s *breakerClientStream) SendMsg(m any) error {
	err := s.ClientStream.SendMsg(m)
	if err != nil {
		// A SendMsg error means the stream broke; io.EOF here signals the RPC
		// completed/aborted and the real status is available via RecvMsg, so it
		// is not itself classified as a failure.
		if !errors.Is(err, io.EOF) {
			s.finish(s.isFailure(err))
		}
	}

	return err
}
