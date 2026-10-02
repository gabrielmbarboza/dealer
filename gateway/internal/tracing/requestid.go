// Package tracing provides cross-cutting request correlation: a request id
// generated or propagated for every request the gateway handles.
package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// HeaderName is the header a request id is read from (when trusted) and
// always echoed back on.
const HeaderName = "X-Request-Id"

type contextKey struct{}

type inboundKey struct{}

var (
	requestIDKey contextKey
	inboundIDKey inboundKey
)

// Middleware returns middleware that assigns every request a request id,
// stored in its context (retrievable via FromContext) and echoed back on
// the response header. When trustInbound is true, an inbound X-Request-Id
// is reused instead of generating a fresh one - only safe when the
// gateway sits behind a trusted upstream (e.g. a load balancer that sets
// this header itself), since otherwise a client could inject an arbitrary
// id into the gateway's logs/traces.
func Middleware(trustInbound bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if trustInbound {
				id = r.Header.Get(HeaderName)
			}
			if id == "" {
				id = newID()
			}

			w.Header().Set(HeaderName, id)

			// Also set it on the request itself, so the reverse proxy forwards
			// the gateway's id to the origin (letting it correlate its own
			// logs) instead of whatever the client sent. Headers are cloned so
			// the caller's request isn't mutated.
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			ctx = context.WithValue(ctx, inboundIDKey, r.Header.Get(HeaderName))
			r = r.WithContext(ctx)
			r.Header = r.Header.Clone()
			r.Header.Set(HeaderName, id)
			next.ServeHTTP(w, r)
		})
	}
}

// PreserveInbound restores, on the request forwarded to the origin, the
// X-Request-Id the client originally sent (if any), for services that need
// it untouched - e.g. a webhook whose provider signs that header. The
// gateway still uses its own id for its logs, traces and the response
// header, so an untrusted client can't inject ids there; only the origin
// sees the inbound value, as if the gateway weren't in the way.
func PreserveInbound(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inbound, _ := r.Context().Value(inboundIDKey).(string); inbound != "" {
			r.Header = r.Header.Clone()
			r.Header.Set(HeaderName, inbound)
		}
		next.ServeHTTP(w, r)
	})
}

// FromContext returns the request id Middleware stored in ctx, or "" if
// Middleware never ran.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
