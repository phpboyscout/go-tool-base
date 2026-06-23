// Package circuitbreaker implements the transport-agnostic Closed/Open/HalfOpen
// state machine shared by the HTTP and gRPC client circuit breakers. It carries
// no HTTP or gRPC types so both transports wrap a single, fully-unit-tested core
// rather than maintaining two divergent implementations.
//
// The breaker fails fast: while Open it admits nothing and the caller returns an
// error immediately. It never stores or serves a previously-seen response — it
// is a stability primitive, not a cache.
package circuitbreaker

import (
	"sync"
	"time"
)

// State is the breaker's current state.
type State int

const (
	// StateClosed admits all calls; consecutive failures are counted.
	StateClosed State = iota
	// StateOpen rejects all calls until the cooldown elapses.
	StateOpen
	// StateHalfOpen admits a bounded number of trial calls; the first success
	// closes the breaker, any failure re-opens it.
	StateHalfOpen
)

// String renders the state for logging.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Default policy values, exported so the transport wrappers' Default*Config
// constructors reference one source of truth rather than re-typing the literals.
const (
	DefaultFailureThreshold = 5
	DefaultCooldown         = 30 * time.Second
	DefaultHalfOpenMax      = 1
)

// Config configures a Breaker. The zero value is invalid; pass it through New,
// which clamps unset/invalid fields to safe defaults.
type Config struct {
	// FailureThreshold is the number of consecutive failures (in Closed) that
	// trips the breaker Open. Clamped to >= 1; default 5.
	FailureThreshold int
	// Cooldown is how long the breaker stays Open before admitting a trial.
	// Clamped to > 0; default 30s.
	Cooldown time.Duration
	// HalfOpenMaxRequests is the number of concurrent trial calls admitted in
	// HalfOpen. Clamped to >= 1; default 1.
	HalfOpenMaxRequests int
	// Now is the clock. Defaults to time.Now; injected in tests so cooldown
	// transitions are deterministic without sleeps.
	Now func() time.Time
	// OnStateChange, if set, is called on every state transition. It runs while
	// the breaker's lock is held, so it MUST NOT call back into the breaker.
	OnStateChange func(from, to State)
}

func (c Config) normalized() Config {
	if c.FailureThreshold < 1 {
		c.FailureThreshold = DefaultFailureThreshold
	}

	if c.Cooldown <= 0 {
		c.Cooldown = DefaultCooldown
	}

	if c.HalfOpenMaxRequests < 1 {
		c.HalfOpenMaxRequests = DefaultHalfOpenMax
	}

	if c.Now == nil {
		c.Now = time.Now
	}

	return c
}

// Breaker is a concurrency-safe Closed/Open/HalfOpen circuit breaker.
type Breaker struct {
	cfg Config

	mu                  sync.Mutex
	state               State
	consecutiveFailures int
	openedAt            time.Time
	halfOpenInFlight    int
}

// New returns a Breaker in the Closed state, with invalid config fields clamped
// to defaults.
func New(cfg Config) *Breaker {
	return &Breaker{cfg: cfg.normalized(), state: StateClosed}
}

// Allow reports whether a call may proceed. When it returns allowed==true the
// caller MUST invoke the returned done func exactly once with the call's outcome
// (failure==true if the call should count against the breaker). When allowed is
// false the breaker is Open (or its HalfOpen trial budget is exhausted) and done
// is nil — the caller fails fast.
func (b *Breaker) Allow() (done func(failure bool), allowed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// An Open breaker whose cooldown has elapsed advances to HalfOpen and may
	// admit a trial on this very call.
	if b.state == StateOpen && b.cfg.Now().Sub(b.openedAt) >= b.cfg.Cooldown {
		b.setState(StateHalfOpen)
		b.halfOpenInFlight = 0
		b.consecutiveFailures = 0
	}

	switch b.state {
	case StateOpen:
		return nil, false
	case StateHalfOpen:
		if b.halfOpenInFlight >= b.cfg.HalfOpenMaxRequests {
			return nil, false
		}

		b.halfOpenInFlight++
	case StateClosed:
		// admit
	}

	return b.record, true
}

// State returns the current state. It does not advance the cooldown clock; a
// post-cooldown Open breaker still reports Open until the next Allow.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.state
}

// record applies a completed call's outcome to the state machine.
func (b *Breaker) record(failure bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateHalfOpen:
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}

		if failure {
			b.trip()
		} else {
			b.setState(StateClosed)
			b.consecutiveFailures = 0
		}
	case StateClosed:
		if failure {
			b.consecutiveFailures++
			if b.consecutiveFailures >= b.cfg.FailureThreshold {
				b.trip()
			}
		} else {
			b.consecutiveFailures = 0
		}
	case StateOpen:
		// A call admitted before the breaker opened may complete after it has
		// opened (e.g. another trial re-opened it). Ignore defensively — the
		// open timer already governs recovery.
	}
}

// trip moves the breaker Open and stamps the cooldown start. Caller holds mu.
func (b *Breaker) trip() {
	b.setState(StateOpen)
	b.openedAt = b.cfg.Now()
	b.consecutiveFailures = 0
}

// setState transitions and fires OnStateChange. Caller holds mu.
func (b *Breaker) setState(to State) {
	if b.state == to {
		return
	}

	from := b.state
	b.state = to

	if b.cfg.OnStateChange != nil {
		b.cfg.OnStateChange(from, to)
	}
}
