package proxy

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// BreakerOptions configures the circuit breaker shared by every origin of
// a service. Threshold <= 0 disables the breaker entirely - origins then
// behave exactly as they did before PR5, governed only by cooldown.
type BreakerOptions struct {
	// Threshold is the number of consecutive failures (regardless of
	// timing) that trips the breaker open for an origin.
	Threshold int

	// Cooldown is how long the breaker stays open before allowing a
	// half-open trial request through. Escalates exponentially (capped)
	// on each further failed trial.
	Cooldown time.Duration
}

type weightedOrigin struct {
	url     string
	handler http.Handler
	state   *originState
}

type roundRobinProxy struct {
	name     string
	origins  []*weightedOrigin
	cooldown time.Duration
	breaker  BreakerOptions
	now      func() time.Time
	next     atomic.Uint64
}

// NewOriginProxy builds a proxy for name across originURLs. A single origin
// is still wrapped in a roundRobinProxy of one, rather than being a
// separate code path, so it gets the same failure/probe-tracked state as a
// multi-origin service - see ProbeTargets.
func NewOriginProxy(name string, originURLs []string, timeout, cooldown time.Duration, retry RetryOptions, breaker BreakerOptions) (http.Handler, error) {
	if len(originURLs) == 0 {
		return nil, fmt.Errorf("proxy: service %q: at least one origin_url is required", name)
	}

	lb := &roundRobinProxy{
		name:     name,
		origins:  make([]*weightedOrigin, 0, len(originURLs)),
		cooldown: cooldown,
		breaker:  breaker,
		now:      time.Now,
	}
	for _, u := range originURLs {
		rp, err := NewReverseProxy(name, u, timeout, retry)
		if err != nil {
			return nil, err
		}
		state := &originState{}
		originalErrorHandler := rp.ErrorHandler
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			state.recordFailure(lb.now())
			originalErrorHandler(w, r, err)
		}
		rp.ModifyResponse = func(resp *http.Response) error {
			state.recordSuccess()
			return nil
		}
		lb.origins = append(lb.origins, &weightedOrigin{url: u, handler: rp, state: state})
	}

	return lb, nil
}

func (lb *roundRobinProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := uint64(len(lb.origins))
	start := lb.next.Add(1)
	now := lb.now()

	for i := uint64(0); i < n; i++ {
		o := lb.origins[(start+i)%n]
		if o.state.available(now, lb.cooldown, lb.breaker.Threshold, lb.breaker.Cooldown) {
			o.handler.ServeHTTP(w, r)
			return
		}
	}

	// Every origin is unavailable. Falling back to attempting one anyway
	// is still worthwhile for a plain cooldown (a real dial attempt/502
	// beats none) - but if that origin's breaker is actually tripped,
	// dialing it defeats the point of having a breaker at all, so fail
	// fast without ever attempting it.
	fallback := lb.origins[start%n]
	if fallback.state.breakerOpen(now, lb.breaker.Threshold, lb.breaker.Cooldown) {
		log.Printf("proxy: service %q origin %s: circuit breaker open, skipping dial", lb.name, fallback.url)
		writeBadGateway(w, lb.name)
		return
	}
	fallback.handler.ServeHTTP(w, r)
}

// ProbeTarget is one origin a Prober can actively health-check.
type ProbeTarget struct {
	URL   string
	state *originState
}

// ProbeTargets exposes h's per-origin state for active health probing, or
// nil if h wasn't built by NewOriginProxy.
func ProbeTargets(h http.Handler) []ProbeTarget {
	lb, ok := h.(*roundRobinProxy)
	if !ok {
		return nil
	}
	targets := make([]ProbeTarget, len(lb.origins))
	for i, o := range lb.origins {
		targets[i] = ProbeTarget{URL: o.url, state: o.state}
	}
	return targets
}
