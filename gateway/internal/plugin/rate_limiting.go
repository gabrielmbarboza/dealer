package plugin

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const staleAfter = 10 * time.Minute

const sweepEvery = 1000

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

type rateLimiting struct {
	requestsPerSecond float64
	burst             float64
	now               func() time.Time

	mu       sync.Mutex
	visitors map[string]*visitor
	calls    atomic.Uint64
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

	return &rateLimiting{
		requestsPerSecond: rps,
		burst:             burst,
		now:               time.Now,
		visitors:          make(map[string]*visitor),
	}, nil
}

func (p *rateLimiting) Name() string { return "rate_limiting" }

func (p *rateLimiting) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.allow(clientIP(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *rateLimiting) allow(key string) bool {
	now := p.now()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.calls.Add(1)%sweepEvery == 0 {
		p.sweepLocked(now)
	}

	v, ok := p.visitors[key]
	if !ok {
		v = &visitor{tokens: p.burst - 1, lastSeen: now}
		p.visitors[key] = v
		return true
	}

	elapsed := now.Sub(v.lastSeen).Seconds()
	v.lastSeen = now
	v.tokens += elapsed * p.requestsPerSecond
	if v.tokens > p.burst {
		v.tokens = p.burst
	}

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

func (p *rateLimiting) sweepLocked(now time.Time) {
	for key, v := range p.visitors {
		if now.Sub(v.lastSeen) > staleAfter {
			delete(p.visitors, key)
		}
	}
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
