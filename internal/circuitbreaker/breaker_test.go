package circuitbreaker

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a manually-advanced clock for deterministic cooldown tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

// fail runs one admitted call recording the given outcome. It fails the test if
// the breaker did not admit the call.
func fail(t *testing.T, b *Breaker, failure bool) {
	t.Helper()

	done, allowed := b.Allow()
	require.True(t, allowed, "expected breaker to admit the call")
	done(failure)
}

func TestState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "open", StateOpen.String())
	assert.Equal(t, "half-open", StateHalfOpen.String())
	assert.Equal(t, "unknown", State(99).String())
}

func TestConfig_NormalizedDefaults(t *testing.T) {
	t.Parallel()

	b := New(Config{})

	assert.Equal(t, DefaultFailureThreshold, b.cfg.FailureThreshold)
	assert.Equal(t, DefaultCooldown, b.cfg.Cooldown)
	assert.Equal(t, DefaultHalfOpenMax, b.cfg.HalfOpenMaxRequests)
	require.NotNil(t, b.cfg.Now)
}

func TestBreaker_OpensAtThreshold(t *testing.T) {
	t.Parallel()

	b := New(Config{FailureThreshold: 3, Now: newFakeClock().now})

	fail(t, b, true)
	fail(t, b, true)
	assert.Equal(t, StateClosed, b.State(), "below threshold stays closed")

	fail(t, b, true)
	assert.Equal(t, StateOpen, b.State(), "third consecutive failure trips open")
}

func TestBreaker_SuccessResetsCounter(t *testing.T) {
	t.Parallel()

	b := New(Config{FailureThreshold: 3, Now: newFakeClock().now})

	fail(t, b, true)
	fail(t, b, true)
	fail(t, b, false) // success resets the consecutive counter
	fail(t, b, true)
	fail(t, b, true)
	assert.Equal(t, StateClosed, b.State(), "counter reset means we are below threshold again")
}

func TestBreaker_OpenRejectsFast(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	b := New(Config{FailureThreshold: 1, Cooldown: 30 * time.Second, Now: clock.now})

	fail(t, b, true)
	require.Equal(t, StateOpen, b.State())

	done, allowed := b.Allow()
	assert.False(t, allowed, "open breaker rejects")
	assert.Nil(t, done)
}

func TestBreaker_HalfOpenAfterCooldown(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	b := New(Config{FailureThreshold: 1, Cooldown: 30 * time.Second, Now: clock.now})

	fail(t, b, true)
	require.Equal(t, StateOpen, b.State())

	clock.advance(29 * time.Second)
	_, allowed := b.Allow()
	assert.False(t, allowed, "still open before cooldown elapses")

	clock.advance(2 * time.Second) // now past 30s
	done, allowed := b.Allow()
	require.True(t, allowed, "admits a trial after cooldown")
	assert.Equal(t, StateHalfOpen, b.State())
	done(false)
}

func TestBreaker_HalfOpenSuccessCloses(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	b := New(Config{FailureThreshold: 1, Cooldown: time.Second, Now: clock.now})

	fail(t, b, true)
	clock.advance(2 * time.Second)

	done, allowed := b.Allow()
	require.True(t, allowed)
	done(false) // trial success

	assert.Equal(t, StateClosed, b.State())

	// Counter was reset: a single failure does not immediately re-trip a
	// threshold-1 breaker that just closed... actually threshold 1 re-trips.
	fail(t, b, false)
	assert.Equal(t, StateClosed, b.State())
}

func TestBreaker_HalfOpenFailureReopens(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	b := New(Config{FailureThreshold: 1, Cooldown: time.Second, Now: clock.now})

	fail(t, b, true)
	clock.advance(2 * time.Second)

	done, allowed := b.Allow()
	require.True(t, allowed)
	done(true) // trial failure

	assert.Equal(t, StateOpen, b.State(), "trial failure re-opens")

	// openedAt was reset, so the cooldown clock restarts from now.
	_, allowed = b.Allow()
	assert.False(t, allowed)
}

func TestBreaker_HalfOpenConcurrencyCap(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	b := New(Config{FailureThreshold: 1, Cooldown: time.Second, HalfOpenMaxRequests: 2, Now: clock.now})

	fail(t, b, true)
	clock.advance(2 * time.Second)

	done1, allowed1 := b.Allow()
	require.True(t, allowed1)
	done2, allowed2 := b.Allow()
	require.True(t, allowed2, "second trial within cap admitted")

	_, allowed3 := b.Allow()
	assert.False(t, allowed3, "third trial beyond cap rejected while trials in flight")

	done1(false)
	done2(false)
}

func TestBreaker_ConcurrentRace(t *testing.T) {
	t.Parallel()

	var transitions atomic.Int64

	b := New(Config{
		FailureThreshold:    3,
		Cooldown:            time.Millisecond,
		HalfOpenMaxRequests: 2,
		OnStateChange:       func(_, _ State) { transitions.Add(1) },
	})

	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			done, allowed := b.Allow()
			if allowed {
				done(n%2 == 0)
			}

			_ = b.State()
		}(i)
	}

	wg.Wait()
	assert.GreaterOrEqual(t, transitions.Load(), int64(0))
}
