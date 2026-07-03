package plugin

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

const staleAfter = 10 * time.Minute

const sweepEvery = 1000

// tokenBucketStore holds per-key token bucket state. memoryStore is the
// default, zero-dependency implementation; a distributed deployment can
// opt into sugarDBStore instead, sharing counters across gateway instances.
type tokenBucketStore interface {
	allow(key string, requestsPerSecond, burst float64, now time.Time) bool
}

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

type memoryStore struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	calls    uint64
}

func newMemoryStore() *memoryStore {
	return &memoryStore{visitors: make(map[string]*visitor)}
}

func (s *memoryStore) allow(key string, requestsPerSecond, burst float64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++
	if s.calls%sweepEvery == 0 {
		s.sweepLocked(now)
	}

	v, ok := s.visitors[key]
	if !ok {
		s.visitors[key] = &visitor{tokens: burst - 1, lastSeen: now}
		return true
	}

	elapsed := now.Sub(v.lastSeen).Seconds()
	v.lastSeen = now
	v.tokens += elapsed * requestsPerSecond
	if v.tokens > burst {
		v.tokens = burst
	}

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

func (s *memoryStore) sweepLocked(now time.Time) {
	for key, v := range s.visitors {
		if now.Sub(v.lastSeen) > staleAfter {
			delete(s.visitors, key)
		}
	}
}

// namespacedStore prefixes every key with a per-plugin-instance id before
// delegating to a shared store. Without this, two independently configured
// rate_limiting plugins in distributed mode would share one counter per
// client IP instead of each tracking it separately - the isolation the
// memoryStore gives for free just by being a separate Go map per instance.
type namespacedStore struct {
	prefix string
	inner  tokenBucketStore
}

func (s *namespacedStore) allow(key string, requestsPerSecond, burst float64, now time.Time) bool {
	return s.inner.allow(s.prefix+":"+key, requestsPerSecond, burst, now)
}

type rateLimiting struct {
	requestsPerSecond float64
	burst             float64
	now               func() time.Time
	store             tokenBucketStore
}

func newRateLimiting(cfg map[string]any) (Plugin, error) {
	raw, ok := cfg["requests_per_second"]
	if !ok {
		return nil, fmt.Errorf("rate_limiting: config.requests_per_second is required")
	}
	rps, err := toFloat64(raw)
	if err != nil {
		return nil, fmt.Errorf("rate_limiting: config.requests_per_second: %w", err)
	}
	if rps <= 0 {
		return nil, fmt.Errorf("rate_limiting: config.requests_per_second must be positive, got %v", rps)
	}

	burst := rps
	if rawBurst, ok := cfg["burst"]; ok {
		burst, err = toFloat64(rawBurst)
		if err != nil {
			return nil, fmt.Errorf("rate_limiting: config.burst: %w", err)
		}
		if burst < 1 {
			return nil, fmt.Errorf("rate_limiting: config.burst must be >= 1, got %v", burst)
		}
	}

	mode, _ := cfg["mode"].(string)
	var store tokenBucketStore
	switch mode {
	case "", "memory":
		store = newMemoryStore()
	case "distributed":
		shared, err := getSharedSugarDBStore()
		if err != nil {
			return nil, fmt.Errorf("rate_limiting: config.mode \"distributed\": %w", err)
		}
		prefix, err := randomID()
		if err != nil {
			return nil, fmt.Errorf("rate_limiting: config.mode \"distributed\": %w", err)
		}
		store = &namespacedStore{prefix: prefix, inner: shared}
	default:
		return nil, fmt.Errorf("rate_limiting: config.mode must be \"memory\" or \"distributed\", got %q", mode)
	}

	return &rateLimiting{
		requestsPerSecond: rps,
		burst:             burst,
		now:               time.Now,
		store:             store,
	}, nil
}

func (p *rateLimiting) Name() string { return "rate_limiting" }

func (p *rateLimiting) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.store.allow(clientIP(r), p.requestsPerSecond, p.burst, p.now()) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", v)
	}
}
