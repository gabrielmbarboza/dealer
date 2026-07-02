package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTaggedOrigin(tag string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Origin", tag)
		w.WriteHeader(http.StatusOK)
	}))
}

func TestNewOriginProxy_SingleOriginDelegatesToReverseProxy(t *testing.T) {
	origin := newTaggedOrigin("only")
	defer origin.Close()

	h, err := NewOriginProxy("catalog", []string{origin.URL}, testTimeout, time.Second)
	if err != nil {
		t.Fatalf("NewOriginProxy() error = %v", err)
	}

	gateway := httptest.NewServer(h)
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/catalog")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Origin") != "only" {
		t.Fatalf("X-Origin = %q, want %q", resp.Header.Get("X-Origin"), "only")
	}
}

func TestNewOriginProxy_RoundRobinsAcrossOrigins(t *testing.T) {
	originA := newTaggedOrigin("A")
	defer originA.Close()
	originB := newTaggedOrigin("B")
	defer originB.Close()

	h, err := NewOriginProxy("catalog", []string{originA.URL, originB.URL}, testTimeout, time.Second)
	if err != nil {
		t.Fatalf("NewOriginProxy() error = %v", err)
	}

	gateway := httptest.NewServer(h)
	defer gateway.Close()

	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		resp, err := http.Get(gateway.URL + "/catalog")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		seen[resp.Header.Get("X-Origin")]++
		resp.Body.Close()
	}

	if seen["A"] != 2 || seen["B"] != 2 {
		t.Fatalf("distribution = %v, want 2 requests to each of A and B", seen)
	}
}

func TestNewOriginProxy_EjectsFailingOriginUntilCooldownElapses(t *testing.T) {
	healthy := newTaggedOrigin("healthy")
	defer healthy.Close()

	down := newTaggedOrigin("down")
	downURL := down.URL
	down.Close()

	h, err := NewOriginProxy("catalog", []string{downURL, healthy.URL}, testTimeout, time.Minute)
	if err != nil {
		t.Fatalf("NewOriginProxy() error = %v", err)
	}
	lb := h.(*roundRobinProxy)

	now := time.Now()
	lb.now = func() time.Time { return now }

	gateway := httptest.NewServer(lb)
	defer gateway.Close()

	sawFailure := false
	for i := 0; i < 6; i++ {
		resp, err := http.Get(gateway.URL + "/catalog")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		tag := resp.Header.Get("X-Origin")
		status := resp.StatusCode
		resp.Body.Close()

		if status == http.StatusBadGateway {
			sawFailure = true
			continue
		}

		if !sawFailure {
			continue
		}
		if tag != "healthy" || status != http.StatusOK {
			t.Fatalf("request %d after down origin failed: status = %d, tag = %q, want 200/healthy (down origin should be skipped during cooldown)", i, status, tag)
		}
	}

	if !sawFailure {
		t.Fatal("down origin was never attempted; test didn't exercise the ejection path")
	}
}

func TestNewOriginProxy_AllOriginsInCooldownStillAttemptsOne(t *testing.T) {
	downA := newTaggedOrigin("A")
	downAURL := downA.URL
	downA.Close()

	downB := newTaggedOrigin("B")
	downBURL := downB.URL
	downB.Close()

	h, err := NewOriginProxy("catalog", []string{downAURL, downBURL}, testTimeout, time.Minute)
	if err != nil {
		t.Fatalf("NewOriginProxy() error = %v", err)
	}

	gateway := httptest.NewServer(h)
	defer gateway.Close()

	for i := 0; i < 2; i++ {
		resp, err := http.Get(gateway.URL + "/catalog")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("request %d: status = %d, want %d", i, resp.StatusCode, http.StatusBadGateway)
		}
	}
}

func TestNewOriginProxy_InvalidOriginURLErrors(t *testing.T) {
	if _, err := NewOriginProxy("broken", []string{"http://0.0.0.0:1", "://not-a-url"}, testTimeout, time.Second); err == nil {
		t.Fatal("NewOriginProxy() error = nil, want non-nil for invalid origin_url")
	}
}

func TestNewOriginProxy_NoOriginsErrors(t *testing.T) {
	if _, err := NewOriginProxy("empty", nil, testTimeout, time.Second); err == nil {
		t.Fatal("NewOriginProxy() error = nil, want non-nil when no origins are given")
	}
}
