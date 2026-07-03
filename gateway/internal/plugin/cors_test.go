package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newCORSTestHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestCORS_MissingAllowedOriginsErrors(t *testing.T) {
	if _, err := newCORS(map[string]any{}); err == nil {
		t.Fatal("newCORS() error = nil, want non-nil when config.allowed_origins is missing")
	}
}

func TestCORS_CredentialsWithWildcardOriginErrors(t *testing.T) {
	_, err := newCORS(map[string]any{
		"allowed_origins":   []any{"*"},
		"allow_credentials": true,
	})
	if err == nil {
		t.Fatal("newCORS() error = nil, want non-nil for allow_credentials=true combined with a wildcard origin")
	}
}

func TestCORS_Name(t *testing.T) {
	p, err := newCORS(map[string]any{"allowed_origins": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}
	if p.Name() != "cors" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "cors")
	}
}

func TestCORS_NoOriginHeaderPassesThrough(t *testing.T) {
	p, err := newCORS(map[string]any{"allowed_origins": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty when no Origin header is present", got)
	}
}

func TestCORS_AllowedOriginSetsHeadersAndCallsNext(t *testing.T) {
	p, err := newCORS(map[string]any{"allowed_origins": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called for an allowed origin")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want %q", got, "Origin")
	}
}

func TestCORS_DisallowedOriginOmitsHeadersButCallsNext(t *testing.T) {
	p, err := newCORS(map[string]any{"allowed_origins": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called for a disallowed origin")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestCORS_WildcardOriginEchoesLiteralAsterisk(t *testing.T) {
	p, err := newCORS(map[string]any{"allowed_origins": []any{"*"}})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

func TestCORS_PreflightAllowedOriginRespondsAndSkipsNext(t *testing.T) {
	p, err := newCORS(map[string]any{
		"allowed_origins": []any{"https://example.com"},
		"allowed_methods": []any{"GET", "POST"},
		"allowed_headers": []any{"Content-Type", "Authorization"},
		"max_age":         600,
	})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodOptions, "/catalog", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called for a preflight request, want it skipped")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, "GET, POST")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want %q", got, "Content-Type, Authorization")
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Fatalf("Access-Control-Max-Age = %q, want %q", got, "600")
	}
}

func TestCORS_PreflightDisallowedOriginRespondsWithoutHeaders(t *testing.T) {
	p, err := newCORS(map[string]any{"allowed_origins": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodOptions, "/catalog", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called for a preflight request, want it skipped")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestCORS_RegularOptionsWithoutRequestMethodIsNotPreflight(t *testing.T) {
	p, err := newCORS(map[string]any{"allowed_origins": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodOptions, "/catalog", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called for a plain OPTIONS request without Access-Control-Request-Method")
	}
}

func TestCORS_CredentialsHeaderSetForAllowedOrigin(t *testing.T) {
	p, err := newCORS(map[string]any{
		"allowed_origins":   []any{"https://example.com"},
		"allow_credentials": true,
	})
	if err != nil {
		t.Fatalf("newCORS() error = %v", err)
	}

	called := false
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	p.Wrap(newCORSTestHandler(&called)).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
	}
}

func TestCORS_InvalidAllowedOriginsTypeErrors(t *testing.T) {
	if _, err := newCORS(map[string]any{"allowed_origins": "not-a-list"}); err == nil {
		t.Fatal("newCORS() error = nil, want non-nil when config.allowed_origins is not a list")
	}
}

func TestCORS_InvalidAllowedOriginsEntryErrors(t *testing.T) {
	if _, err := newCORS(map[string]any{"allowed_origins": []any{123}}); err == nil {
		t.Fatal("newCORS() error = nil, want non-nil when a config.allowed_origins entry is not a string")
	}
}
