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

var requestIDKey contextKey

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
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
		})
	}
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
