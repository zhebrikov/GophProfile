package middleware

import (
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"golang.org/x/time/rate"
)

const (
	defaultLimiterTTL             = 10 * time.Minute
	defaultLimiterCleanupInterval = time.Minute
)

// LimiterStore decides whether a request for the given key is allowed.
// The in-memory implementation uses per-key token buckets; a Redis-backed
// store can replace it without changing the middleware.
type LimiterStore interface {
	Allow(key string) bool
}

type memoryLimiterStore struct {
	mu    sync.Mutex
	cache *cache.Cache
	r     rate.Limit
	b     int
	ttl   time.Duration
}

// NewMemoryLimiterStore returns a TTL-backed in-memory LimiterStore.
// Inactive keys expire after ttl; cleanupInterval controls background eviction.
func NewMemoryLimiterStore(rps float64, burst int, ttl, cleanupInterval time.Duration) LimiterStore {
	if ttl <= 0 {
		ttl = defaultLimiterTTL
	}
	if cleanupInterval <= 0 {
		cleanupInterval = defaultLimiterCleanupInterval
	}
	return &memoryLimiterStore{
		cache: cache.New(ttl, cleanupInterval),
		r:     rate.Limit(rps),
		b:     burst,
		ttl:   ttl,
	}
}

func (s *memoryLimiterStore) Allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	lim, ok := s.get(key)
	if !ok {
		lim = rate.NewLimiter(s.r, s.b)
	}
	// Refresh TTL so active keys stay; inactive ones are eventually evicted.
	s.cache.Set(key, lim, s.ttl)
	return lim.Allow()
}

func (s *memoryLimiterStore) get(key string) (*rate.Limiter, bool) {
	v, ok := s.cache.Get(key)
	if !ok {
		return nil, false
	}
	lim, ok := v.(*rate.Limiter)
	return lim, ok
}
