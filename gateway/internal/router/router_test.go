package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gabrielmbarboza/dealer/gateway/internal/config"
)

var errBoom = errors.New("boom")

func stubHandlerFor(t *testing.T) func(config.Service) (http.Handler, error) {
	t.Helper()
	return func(svc config.Service) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Service", svc.Name)
			if id := r.PathValue("id"); id != "" {
				w.Header().Set("X-Id", id)
			}
			w.WriteHeader(http.StatusOK)
		}), nil
	}
}

func TestBuild_PathParamCapture(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			{Name: "orders", Path: "/orders/{id}", OriginURL: "http://example.com"},
		},
	}

	mux, err := Build(cfg, stubHandlerFor(t))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/orders/42", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Id"); got != "42" {
		t.Fatalf("X-Id = %q, want %q", got, "42")
	}
}

func TestBuild_NoMethodsMatchesAllVerbs(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			{Name: "catalog", Path: "/catalog", OriginURL: "http://example.com"},
		},
	}

	mux, err := Build(cfg, stubHandlerFor(t))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/catalog", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("method %s: status = %d, want %d", method, rec.Code, http.StatusOK)
		}
	}
}

func TestBuild_MethodMismatchDoesNotMatch(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			{Name: "checkout", Path: "/checkout", OriginURL: "http://example.com", Methods: []string{"GET"}},
		},
	}

	mux, err := Build(cfg, stubHandlerFor(t))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/checkout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestBuild_UnmatchedPathIsNotFound(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			{Name: "orders", Path: "/orders/{id}", OriginURL: "http://example.com"},
		},
	}

	mux, err := Build(cfg, stubHandlerFor(t))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestBuild_HandlerForErrorPropagates(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			{Name: "broken", Path: "/broken", OriginURL: "http://example.com"},
		},
	}

	_, err := Build(cfg, func(svc config.Service) (http.Handler, error) {
		return nil, errBoom
	})
	if err == nil {
		t.Fatal("Build() error = nil, want non-nil")
	}
}
