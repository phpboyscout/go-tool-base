package ratelimit

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewStore_ClampsMaxKeys(t *testing.T) {
	t.Parallel()

	s := NewStore(1, 1, 0)
	assert.Equal(t, DefaultMaxTrackedKeys, s.maxKeys)
}

func TestStore_Bounded(t *testing.T) {
	t.Parallel()

	const maxKeys = 16

	s := NewStore(1, 1, maxKeys)
	for i := range 1000 {
		s.LimiterFor("key-" + strconv.Itoa(i))
	}

	assert.LessOrEqual(t, s.Len(), maxKeys, "store must not exceed maxKeys under churn")
}

func TestStore_StableKeyReturnsSameLimiter(t *testing.T) {
	t.Parallel()

	s := NewStore(1, 1, 8)

	first := s.LimiterFor("stable")
	second := s.LimiterFor("stable")

	assert.Same(t, first, second, "a stable key returns the same limiter instance")
}

func TestStore_LRUEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	// Cap 2: insert A, B; touch A (MRU); insert C -> B (LRU) evicted, A survives.
	s := NewStore(1, 1, 2)

	a := s.LimiterFor("a")
	s.LimiterFor("b")
	s.LimiterFor("a") // A now most-recently-used
	s.LimiterFor("c") // evicts B

	assert.Equal(t, 2, s.Len())
	assert.Same(t, a, s.LimiterFor("a"), "A survived eviction and kept its limiter")
}

func TestStore_ConcurrentRace(t *testing.T) {
	t.Parallel()

	s := NewStore(1000, 1000, 64)

	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()
			s.LimiterFor("k-" + strconv.Itoa(n%128)).Allow()
		}(i)
	}

	wg.Wait()
}
