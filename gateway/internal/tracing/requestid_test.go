package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_GeneratesIDWhenNoInboundHeader(t *testing.T) {
	var seenInHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInHandler = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	Middleware(false)(next).ServeHTTP(rec, req)

	if seenInHandler == "" {
		t.Fatal("FromContext() = \"\", want a generated request id")
	}
	if got := rec.Header().Get(HeaderName); got != seenInHandler {
		t.Fatalf("response header %s = %q, want it to match the id seen by the handler %q", HeaderName, got, seenInHandler)
	}
}

func TestMiddleware_TrustsInboundHeaderWhenEnabled(t *testing.T) {
	const inboundID = "upstream-lb-id-123"

	var seenInHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInHandler = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	req.Header.Set(HeaderName, inboundID)
	rec := httptest.NewRecorder()
	Middleware(true)(next).ServeHTTP(rec, req)

	if seenInHandler != inboundID {
		t.Fatalf("FromContext() = %q, want the trusted inbound id %q", seenInHandler, inboundID)
	}
	if got := rec.Header().Get(HeaderName); got != inboundID {
		t.Fatalf("response header %s = %q, want %q", HeaderName, got, inboundID)
	}
}

func TestMiddleware_RegeneratesWhenTrustDisabledEvenWithInboundHeader(t *testing.T) {
	const inboundID = "untrusted-client-supplied-id"

	var seenInHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInHandler = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	req.Header.Set(HeaderName, inboundID)
	rec := httptest.NewRecorder()
	Middleware(false)(next).ServeHTTP(rec, req)

	if seenInHandler == inboundID {
		t.Fatal("FromContext() returned the untrusted inbound id, want a freshly generated one")
	}
	if seenInHandler == "" {
		t.Fatal("FromContext() = \"\", want a generated request id")
	}
}

func TestMiddleware_TrustEnabledButNoInboundHeaderStillGenerates(t *testing.T) {
	var seenInHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInHandler = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	Middleware(true)(next).ServeHTTP(rec, req)

	if seenInHandler == "" {
		t.Fatal("FromContext() = \"\", want a generated request id when trust is enabled but no header was sent")
	}
}

func TestMiddleware_GeneratesUniqueIDsAcrossRequests(t *testing.T) {
	seen := map[string]bool{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[FromContext(r.Context())] = true
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
		rec := httptest.NewRecorder()
		Middleware(false)(next).ServeHTTP(rec, req)
	}

	if len(seen) != 10 {
		t.Fatalf("saw %d unique ids across 10 requests, want 10", len(seen))
	}
}

func TestFromContext_EmptyWhenNotSet(t *testing.T) {
	if got := FromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got != "" {
		t.Fatalf("FromContext() = %q, want empty string when middleware never ran", got)
	}
}
