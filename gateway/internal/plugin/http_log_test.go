package plugin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	logged := buf.String()
	if !strings.Contains(logged, http.MethodPost) || !strings.Contains(logged, "/payments") {
		t.Fatalf("log output = %q, want it to contain method and path", logged)
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

func TestHTTPLog_Name(t *testing.T) {
	p, err := newHTTPLog(map[string]any{})
	if err != nil {
		t.Fatalf("newHTTPLog() error = %v", err)
	}
	if p.Name() != "http_log" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "http_log")
	}
}
