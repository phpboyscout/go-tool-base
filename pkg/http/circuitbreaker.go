package http

import (
	"net/http"
	"time"

	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/circuitbreaker"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// CircuitState is the client circuit breaker's state.
type CircuitState int

// The public states are derived from the shared core so the two enumerations
// can never silently drift out of order.
const (
	// StateClosed admits all requests; failures are counted.
	StateClosed = CircuitState(circuitbreaker.StateClosed)
	// StateOpen rejects all requests immediately with ErrCircuitOpen until the
	// cooldown elapses, then transitions to StateHalfOpen.
	StateOpen = CircuitState(circuitbreaker.StateOpen)
	// StateHalfOpen admits a limited number of trial requests; success closes
	// the breaker, failure re-opens it.
	StateHalfOpen = CircuitState(circuitbreaker.StateHalfOpen)
)

// String renders the state for logging.
func (s CircuitState) String() string {
	return circuitbreaker.State(s).String()
}

// ErrCircuitOpen is returned by the breaker when it is open. Callers may test
// for it with errors.Is. It is returned directly (not stack-wrapped per call):
// an open breaker is an expected control-flow signal on a high-volume reject
// path, not an exceptional error needing a fresh stack each time.
var ErrCircuitOpen = errors.New("http: circuit breaker is open")

// CircuitBreakerConfig configures the client-side circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures (within Closed)
	// that trips the breaker open. Must be >= 1. Default: 5.
	FailureThreshold int `mapstructure:"failure_threshold" yaml:"failure_threshold" json:"failure_threshold"`

	// Cooldown is how long the breaker stays Open before allowing a trial.
	// Default: 30s.
	Cooldown time.Duration `mapstructure:"cooldown" yaml:"cooldown" json:"cooldown"`

	// HalfOpenMaxRequests is the number of trial requests allowed in HalfOpen.
	// The first success closes the breaker; any failure re-opens it.
	// Must be >= 1. Default: 1.
	HalfOpenMaxRequests int `mapstructure:"half_open_max_requests" yaml:"half_open_max_requests" json:"half_open_max_requests"`

	// IsFailure classifies a round-trip outcome as a failure for breaker
	// accounting. When nil, the default treats transport errors and 5xx
	// responses (>=500) as failures; 4xx and 2xx/3xx are successes. A 429
	// (client rate-limited) therefore does NOT trip the breaker — that is
	// retry's job, not the breaker's.
	IsFailure func(resp *http.Response, err error) bool `mapstructure:"-" yaml:"-" json:"-"`

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
// trial 1, default 5xx/transport-error failure classification.
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

// defaultHTTPIsFailure counts transport errors and 5xx responses as failures.
// 4xx (including 429) and 2xx/3xx are successes: a 429 means "slow down", which
// is the retry layer's concern, not a signal that the downstream is unhealthy.
func defaultHTTPIsFailure(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}

	return resp != nil && resp.StatusCode >= http.StatusInternalServerError
}

// WithCircuitBreaker returns a ClientMiddleware that fails fast while a
// downstream is consistently failing, avoiding wasted retry/backoff cycles.
//
// Place it OUTSIDE the retry transport — i.e. in the ClientChain via
// WithClientMiddleware, which wraps the transport after retry — so the breaker
// sees the final post-retry verdict: one retry-exhausted logical call counts as
// a single breaker failure, not one per attempt. Once Open, calls are rejected
// before entering the retry layer, so no backoff sleeps are spent on a service
// known to be down.
func WithCircuitBreaker(log logger.Logger, cfg CircuitBreakerConfig) ClientMiddleware {
	return newCircuitBreakerMiddleware(log, cfg, nil)
}

// newCircuitBreakerMiddleware is the testable constructor; now is injected so
// cooldown transitions are deterministic in tests (nil → time.Now).
func newCircuitBreakerMiddleware(log logger.Logger, cfg CircuitBreakerConfig, now func() time.Time) ClientMiddleware {
	isFailure := cfg.IsFailure
	if isFailure == nil {
		isFailure = defaultHTTPIsFailure
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

	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			done, allowed := br.Allow()
			if !allowed {
				return nil, ErrCircuitOpen
			}

			resp, err := next.RoundTrip(req)
			done(isFailure(resp, err))

			return resp, err
		})
	}
}
