package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func serveResponseHeaders(t *testing.T, cfg map[string]any, next http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	p, err := newResponseHeaders(cfg)
	if err != nil {
		t.Fatalf("newResponseHeaders() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)
	return rec
}

func TestResponseHeaders_SetsConfiguredHeadersOnResponse(t *testing.T) {
	rec := serveResponseHeaders(t, map[string]any{
		"headers": map[string]any{
			"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		},
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Fatalf("Strict-Transport-Security = %q", got)
	}
}

func TestResponseHeaders_ReplacesTheOriginValue(t *testing.T) {
	rec := serveResponseHeaders(t, map[string]any{
		"headers": map[string]any{"Content-Security-Policy": "default-src 'self'"},
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src *")
		w.WriteHeader(http.StatusOK)
	}))

	if got := rec.Header().Values("Content-Security-Policy"); len(got) != 1 || got[0] != "default-src 'self'" {
		t.Fatalf("Content-Security-Policy = %q, want a single gateway value", got)
	}
}

func TestResponseHeaders_AppliesWhenTheOriginWritesWithoutWriteHeader(t *testing.T) {
	rec := serveResponseHeaders(t, map[string]any{
		"headers": map[string]any{"X-Frame-Options": "DENY"},
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want %q", got, "DENY")
	}
}

func TestResponseHeaders_AppliesToErrorResponses(t *testing.T) {
	rec := serveResponseHeaders(t, map[string]any{
		"headers": map[string]any{"Strict-Transport-Security": "max-age=60"},
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=60" {
		t.Fatalf("Strict-Transport-Security = %q, want %q", got, "max-age=60")
	}
}

func TestResponseHeaders_RuleAppliesOnlyToMatchingContentType(t *testing.T) {
	cfg := map[string]any{
		"rules": []any{
			map[string]any{
				"content_type": "text/html",
				"headers":      map[string]any{"Cache-Control": "no-cache"},
			},
		},
	}

	html := serveResponseHeaders(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
	}))
	if got := html.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("html Cache-Control = %q, want %q", got, "no-cache")
	}

	js := serveResponseHeaders(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
	}))
	if got := js.Header().Get("Cache-Control"); got != "max-age=3600" {
		t.Fatalf("js Cache-Control = %q, want the origin value %q", got, "max-age=3600")
	}
}

func TestResponseHeaders_FlushStillReachesTheUnderlyingWriter(t *testing.T) {
	rec := serveResponseHeaders(t, map[string]any{
		"headers": map[string]any{"X-Frame-Options": "DENY"},
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
	}))

	if !rec.Flushed {
		t.Fatal("response was not flushed")
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want %q", got, "DENY")
	}
}

func TestResponseHeaders_InvalidConfigErrors(t *testing.T) {
	cases := map[string]map[string]any{
		"headers not a map":       {"headers": "nope"},
		"header value not string": {"headers": map[string]any{"X-A": 1}},
		"rules not a list":        {"rules": "nope"},
		"rule not a map":          {"rules": []any{"nope"}},
		"rule without type":       {"rules": []any{map[string]any{"headers": map[string]any{"X-A": "b"}}}},
		"rule without headers":    {"rules": []any{map[string]any{"content_type": "text/html"}}},
	}
	for name, cfg := range cases {
		if _, err := newResponseHeaders(cfg); err == nil {
			t.Errorf("%s: newResponseHeaders() error = nil, want non-nil", name)
		}
	}
}

func TestResponseHeaders_Name(t *testing.T) {
	p, err := Build("response_headers", map[string]any{}, "test:0")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if p.Name() != "response_headers" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "response_headers")
	}
}
