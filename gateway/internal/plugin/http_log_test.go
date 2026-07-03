package plugin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gabrielmbarboza/dealer/gateway/internal/tracing"
)

func TestHTTPLog_LogsMethodAndPath(t *testing.T) {
	var buf bytes.Buffer
	p := newHTTPLogWithWriter(&buf)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/payments", nil)
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)
	p.flush()

	logged := buf.String()
	if !strings.Contains(logged, http.MethodPost) || !strings.Contains(logged, "/payments") {
		t.Fatalf("log output = %q, want it to contain method and path", logged)
	}
}

// countingWriter counts how many times the underlying writer's Write is
// invoked, regardless of how many bytes each call carries.
type countingWriter struct {
	mu    sync.Mutex
	calls int
	buf   bytes.Buffer
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	return w.buf.Write(p)
}

func TestHTTPLog_BatchesWritesToUnderlyingWriter(t *testing.T) {
	const requests = 200

	cw := &countingWriter{}
	p := newHTTPLogWithWriter(cw)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < requests; i++ {
		req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
		rec := httptest.NewRecorder()
		p.Wrap(next).ServeHTTP(rec, req)
	}
	p.flush()

	cw.mu.Lock()
	calls := cw.calls
	cw.mu.Unlock()

	if calls >= requests {
		t.Fatalf("underlying writer got %d Write calls for %d requests, want writes to be batched (calls << requests)", calls, requests)
	}
}

func TestHTTPLog_NeverBlocks(t *testing.T) {
	var buf bytes.Buffer
	p := newHTTPLogWithWriter(&buf)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHTTPLog_IncludesRequestIDWhenPresentInContext(t *testing.T) {
	var buf bytes.Buffer
	p := newHTTPLogWithWriter(&buf)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Mirrors how gateway.go composes things: tracing.Middleware runs
	// outermost (assigning the request id) with the plugin chain nested
	// inside it.
	handler := tracing.Middleware(false)(p.Wrap(next))

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	p.flush()

	logged := buf.String()
	if !strings.Contains(logged, "request_id=") {
		t.Fatalf("log output = %q, want it to contain a request_id field", logged)
	}
}

func TestHTTPLog_OmitsRequestIDFieldWhenAbsentFromContext(t *testing.T) {
	var buf bytes.Buffer
	p := newHTTPLogWithWriter(&buf)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)
	p.flush()

	logged := buf.String()
	if strings.Contains(logged, "request_id=") {
		t.Fatalf("log output = %q, want no request_id field when tracing.Middleware never ran", logged)
	}
}

func TestHTTPLog_Name(t *testing.T) {
	p, err := newHTTPLog(map[string]any{})
	if err != nil {
		t.Fatalf("newHTTPLog() error = %v", err)
	}
	if p.Name() != "http_log" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "http_log")
	}
}
