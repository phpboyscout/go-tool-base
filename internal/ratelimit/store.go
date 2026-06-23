// Package ratelimit provides the bounded, LRU-evicting per-key token-bucket
// store shared by the HTTP and gRPC server-side rate limiters. Keeping it in one
// place means both transports get identical memory-safety guarantees rather than
// two divergent stores.
package ratelimit

import (
	"container/list"
	"sync"

	"golang.org/x/time/rate"
)

// DefaultMaxTrackedKeys bounds the per-key store so an attacker rotating source
// keys cannot allocate unbounded *rate.Limiter values and exhaust memory.
const DefaultMaxTrackedKeys = 8192

// Store is a bounded, LRU-evicting map of per-key token buckets. It is
// mutex-guarded and caps the number of tracked keys; when full, the
// least-recently-used key is evicted. A re-sighted evicted key gets a fresh,
// full bucket — acceptable because eviction only happens under key churn.
type Store struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	maxKeys int
	order   *list.List // front = most-recently-used
	items   map[string]*list.Element
}

type entry struct {
	key     string
	limiter *rate.Limiter
}

// NewStore returns a Store handing out buckets of the given rate and burst,
// tracking at most maxKeys keys (clamped to >= 1).
func NewStore(limit rate.Limit, burst, maxKeys int) *Store {
	if maxKeys < 1 {
		maxKeys = DefaultMaxTrackedKeys
	}

	return &Store{
		limit:   limit,
		burst:   burst,
		maxKeys: maxKeys,
		order:   list.New(),
		items:   make(map[string]*list.Element),
	}
}

// LimiterFor returns the token bucket for key, creating it on first sighting and
// promoting it to most-recently-used. Evicts the LRU key when over capacity.
func (s *Store) LimiterFor(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	if el, ok := s.items[key]; ok {
		s.order.MoveToFront(el)

		return el.Value.(*entry).limiter
	}

	lim := rate.NewLimiter(s.limit, s.burst)
	el := s.order.PushFront(&entry{key: key, limiter: lim})
	s.items[key] = el

	if s.order.Len() > s.maxKeys {
		if back := s.order.Back(); back != nil {
			s.order.Remove(back)
			delete(s.items, back.Value.(*entry).key)
		}
	}

	return lim
}

// Len reports the number of tracked keys.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.items)
}
