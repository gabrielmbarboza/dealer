package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddHeader_AddsConfiguredHeaders(t *testing.T) {
	p, err := newAddHeader(map[string]any{
		"headers": map[string]any{
			"X-Gateway":         "dealer",
			"X-Gateway-Service": "catalog",
		},
	})
	if err != nil {
		t.Fatalf("newAddHeader() error = %v", err)
	}

	var gotUA, gotSvc string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("X-Gateway")
		gotSvc = r.Header.Get("X-Gateway-Service")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)

	if gotUA != "dealer" {
		t.Fatalf("X-Gateway = %q, want %q", gotUA, "dealer")
	}
	if gotSvc != "catalog" {
		t.Fatalf("X-Gateway-Service = %q, want %q", gotSvc, "catalog")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAddHeader_EmptyConfigIsNoOp(t *testing.T) {
	p, err := newAddHeader(map[string]any{})
	if err != nil {
		t.Fatalf("newAddHeader() error = %v", err)
	}

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

func TestAddHeader_InvalidHeadersConfigErrors(t *testing.T) {
	if _, err := newAddHeader(map[string]any{"headers": "not-a-map"}); err == nil {
		t.Fatal("newAddHeader() error = nil, want non-nil for invalid headers config")
	}
}

func TestAddHeader_Name(t *testing.T) {
	p, err := newAddHeader(map[string]any{})
	if err != nil {
		t.Fatalf("newAddHeader() error = %v", err)
	}
	if p.Name() != "add_header" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "add_header")
	}
}
