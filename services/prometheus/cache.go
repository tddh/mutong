package prometheus

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/allegro/bigcache/v3"
)

// PrometheusCache provides a TTL-based cache for Prometheus query results.
// It uses bigcache under the hood for efficient in-memory storage.
type PrometheusCache struct {
	cache *bigcache.BigCache
	ttl   time.Duration
	mu    sync.RWMutex
}

// NewPrometheusCache creates a new cache instance with the given TTL (in seconds)
// and an approximate maximum number of entries. If TTL <= 0, a default 60s TTL is used.
func NewPrometheusCache(ttlSeconds int, maxSize int) *PrometheusCache {
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	cfg := bigcache.Config{
		Shards:             1024,
		LifeWindow:         time.Duration(ttlSeconds) * time.Second,
		CleanWindow:        5 * time.Minute,
		MaxEntriesInWindow: maxSize,
		// Do not set HardMaxCacheSize to keep compatibility with environment
	}
	bc, err := bigcache.New(context.Background(), cfg)
	if err != nil {
		// Fallback: return a cache with nil backing store to avoid crashes
		return &PrometheusCache{cache: nil, ttl: time.Duration(ttlSeconds) * time.Second}
	}
	return &PrometheusCache{cache: bc, ttl: time.Duration(ttlSeconds) * time.Second}
}

// Get retrieves a cached value for the given key. Returns the de-serialized
// slice of QueryRangeResult and a boolean indicating if the value was found.
func (p *PrometheusCache) Get(key string) ([]QueryRangeResult, bool) {
	if p == nil || p.cache == nil {
		return nil, false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	data, err := p.cache.Get(key)
	if err != nil {
		return nil, false
	}
	var value []QueryRangeResult
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, false
	}
	return value, true
}

// Set stores the given value in the cache for the provided key.
func (p *PrometheusCache) Set(key string, value []QueryRangeResult) {
	if p == nil || p.cache == nil {
		return
	}
	b, err := json.Marshal(value)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.cache.Set(key, b)
}

// Clear clears all cached entries.
func (p *PrometheusCache) Clear() {
	if p == nil || p.cache == nil {
		return
	}
	_ = p.cache.Reset()
}
