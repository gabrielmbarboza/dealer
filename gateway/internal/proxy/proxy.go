// Package proxy builds per-service reverse proxies that forward requests to
// internal origin services, preserving the incoming method, path, headers
// and body unchanged.
package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/propagation"
)

// traceContextPropagator injects the active span's W3C trace context
// (traceparent/tracestate) into the outgoing request to the origin, so a
// downstream service can continue the same trace. A safe no-op when no
// span is active (tracing disabled): Inject only writes headers for a
// valid span context.
var traceContextPropagator = propagation.TraceContext{}

// maxIdleConnsPerHost bounds the per-origin idle connection pool. Go's
// zero-value default (DefaultMaxIdleConnsPerHost = 2) is too small for a
// gateway: any concurrency above 2 in-flight requests to the same origin
// forces the transport to tear down and re-dial connections instead of
// reusing them.
const maxIdleConnsPerHost = 100

// RetryOptions configures retryTransport for a reverse proxy. The zero
// value (MaxAttempts <= 1) disables retries entirely.
type RetryOptions struct {
	// MaxAttempts is the total number of attempts per request, including
	// the first. <= 1 disables retries.
	MaxAttempts int

	// BackoffBase is the base delay between attempts; actual delay grows
	// exponentially with jitter added.
	BackoffBase time.Duration

	// RetryUnsafeMethods allows retrying non-idempotent methods (POST,
	// PUT, PATCH, DELETE). Off by default: retrying a method that isn't
	// guaranteed idempotent risks the origin executing it twice.
	RetryUnsafeMethods bool
}

// NewReverseProxy builds a reverse proxy that forwards requests to
// originURL, keeping the request path unstripped (e.g. a request to
// /payments on the gateway reaches originURL + /payments on the origin).
// name is only used to identify the service in error responses/logs.
// timeout bounds both dialing the origin and waiting for its response
// headers, so a hung or unreachable origin fails fast with a 502 instead of
// tying up the gateway indefinitely.
func NewReverseProxy(name, originURL string, timeout time.Duration, retry RetryOptions) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(originURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: invalid origin_url %q: %w", originURL, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("proxy: invalid origin_url %q: missing scheme or host", originURL)
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	var transport http.RoundTripper = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: timeout,
		}).DialContext,
		ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
	}
	if retry.MaxAttempts > 1 {
		transport = newRetryTransport(transport, retry.MaxAttempts, retry.BackoffBase, retry.RetryUnsafeMethods)
	}
	rp.Transport = transport

	originalDirector := rp.Director
	rp.Director = func(r *http.Request) {
		userAgent := r.Header.Get("User-Agent")
		originalDirector(r)
		r.Header.Set("User-Agent", userAgent)
		traceContextPropagator.Inject(r.Context(), propagation.HeaderCarrier(r.Header))
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy: service %q origin %s: %v", name, originURL, err)
		writeBadGateway(w, name)
	}

	return rp, nil
}

// writeBadGateway writes the gateway's standard JSON 502 body, shared by
// NewReverseProxy's ErrorHandler and roundRobinProxy's circuit-breaker
// fast-fail path so both produce an identical response shape.
func writeBadGateway(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	if encErr := json.NewEncoder(w).Encode(map[string]string{
		"error":   "bad_gateway",
		"service": name,
	}); encErr != nil {
		log.Printf("proxy: encode bad_gateway response: %v", encErr)
	}
}
