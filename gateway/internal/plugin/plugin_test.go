package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePlugin struct {
	name  string
	block bool
	calls *[]string
}

func (p *fakePlugin) Name() string { return p.name }

func (p *fakePlugin) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*p.calls = append(*p.calls, p.name)
		if p.block {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestChain_ExecutesInDeclarationOrder(t *testing.T) {
	var calls []string
	plugins := []Plugin{
		&fakePlugin{name: "first", calls: &calls},
		&fakePlugin{name: "second", calls: &calls},
		&fakePlugin{name: "third", calls: &calls},
	}
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "final")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Chain(plugins, final).ServeHTTP(rec, req)

	want := []string{"first", "second", "third", "final"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestChain_BlockingPluginStopsChain(t *testing.T) {
	var calls []string
	plugins := []Plugin{
		&fakePlugin{name: "first", calls: &calls},
		&fakePlugin{name: "blocker", block: true, calls: &calls},
		&fakePlugin{name: "third", calls: &calls},
	}
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "final")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Chain(plugins, final).ServeHTTP(rec, req)

	want := []string{"first", "blocker"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestChain_EmptyPluginListFallsThroughToFinal(t *testing.T) {
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Chain(nil, final).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
