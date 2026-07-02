package plugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestSizeLimiting_ContentLengthOverLimitBlocks(t *testing.T) {
	p, err := newRequestSizeLimiting(map[string]any{"max_bytes": 10})
	if err != nil {
		t.Fatalf("newRequestSizeLimiting() error = %v", err)
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/catalog", strings.NewReader("this body is definitely over ten bytes"))
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called, want blocked")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRequestSizeLimiting_UnderLimitPassesThroughIntact(t *testing.T) {
	p, err := newRequestSizeLimiting(map[string]any{"max_bytes": 1024})
	if err != nil {
		t.Fatalf("newRequestSizeLimiting() error = %v", err)
	}

	const body = `{"amount":100}`
	var gotBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/payments", strings.NewReader(body))
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotBody != body {
		t.Fatalf("body = %q, want %q", gotBody, body)
	}
}

func TestRequestSizeLimiting_UnknownLengthOversizedBodyFailsDownstream(t *testing.T) {
	p, err := newRequestSizeLimiting(map[string]any{"max_bytes": 10})
	if err != nil {
		t.Fatalf("newRequestSizeLimiting() error = %v", err)
	}

	var readErr error
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	// Wrapping in io.NopCloser hides the concrete *strings.Reader type from
	// httptest.NewRequest, so it can't infer ContentLength - simulating a
	// chunked/unknown-length request.
	body := io.NopCloser(strings.NewReader("this body is definitely over ten bytes"))
	req := httptest.NewRequest(http.MethodPost, "/catalog", body)
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)

	if readErr == nil {
		t.Fatal("ReadAll() error = nil, want a MaxBytesReader limit error")
	}
}

func TestRequestSizeLimiting_MissingMaxBytesErrors(t *testing.T) {
	if _, err := newRequestSizeLimiting(map[string]any{}); err == nil {
		t.Fatal("newRequestSizeLimiting() error = nil, want non-nil when max_bytes is missing")
	}
}

func TestRequestSizeLimiting_Name(t *testing.T) {
	p, err := newRequestSizeLimiting(map[string]any{"max_bytes": 10})
	if err != nil {
		t.Fatalf("newRequestSizeLimiting() error = %v", err)
	}
	if p.Name() != "request_size_limiting" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "request_size_limiting")
	}
}
