package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestRateLimiting(t *testing.T, cfg map[string]any, instanceID string) (*rateLimiting, error) {
	t.Helper()
	p, err := newRateLimiting(cfg, instanceID)
	if err != nil {
		return nil, err
	}
	return p.(*rateLimiting), nil
}

func serveRateLimited(p Plugin, remoteAddr string) int {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	p.Wrap(next).ServeHTTP(rec, req)
	return rec.Code
}

func TestRateLimiting_AllowsUpToBurstThenBlocks(t *testing.T) {
	p, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 1, "burst": 3}, "")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		if code := serveRateLimited(p, "1.2.3.4:1111"); code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, code, http.StatusOK)
		}
	}

	if code := serveRateLimited(p, "1.2.3.4:1111"); code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", code, http.StatusTooManyRequests)
	}
}

func TestRateLimiting_RefillsOverTime(t *testing.T) {
	p, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 1, "burst": 1}, "")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}

	now := time.Now()
	p.now = func() time.Time { return now }

	if code := serveRateLimited(p, "1.2.3.4:1111"); code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", code, http.StatusOK)
	}
	if code := serveRateLimited(p, "1.2.3.4:1111"); code != http.StatusTooManyRequests {
		t.Fatalf("second request (no time elapsed): status = %d, want %d", code, http.StatusTooManyRequests)
	}

	now = now.Add(2 * time.Second)
	if code := serveRateLimited(p, "1.2.3.4:1111"); code != http.StatusOK {
		t.Fatalf("third request (after refill): status = %d, want %d", code, http.StatusOK)
	}
}

func TestRateLimiting_TracksClientsIndependently(t *testing.T) {
	p, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 1, "burst": 1}, "")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}

	if code := serveRateLimited(p, "1.1.1.1:1111"); code != http.StatusOK {
		t.Fatalf("client A: status = %d, want %d", code, http.StatusOK)
	}
	if code := serveRateLimited(p, "2.2.2.2:2222"); code != http.StatusOK {
		t.Fatalf("client B: status = %d, want %d", code, http.StatusOK)
	}
	if code := serveRateLimited(p, "1.1.1.1:1111"); code != http.StatusTooManyRequests {
		t.Fatalf("client A retry: status = %d, want %d", code, http.StatusTooManyRequests)
	}
}

func TestRateLimiting_InvalidModeErrors(t *testing.T) {
	if _, err := newRateLimiting(map[string]any{"requests_per_second": 1, "mode": "bogus"}, ""); err == nil {
		t.Fatal("newRateLimiting() error = nil, want non-nil for an unrecognized config.mode")
	}
}

func TestRateLimiting_DefaultModeIsMemory(t *testing.T) {
	p, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 1}, "")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}
	if _, ok := p.store.(*memoryStore); !ok {
		t.Fatalf("store = %T, want *memoryStore", p.store)
	}
}

func TestRateLimiting_DistributedModeUsesSugarDBBackedStore(t *testing.T) {
	p, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 1, "burst": 3, "mode": "distributed"}, "catalog:0")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}
	if _, ok := p.store.(*namespacedStore); !ok {
		t.Fatalf("store = %T, want *namespacedStore", p.store)
	}

	for i := 0; i < 3; i++ {
		if code := serveRateLimited(p, "9.9.9.9:1111"); code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, code, http.StatusOK)
		}
	}
	if code := serveRateLimited(p, "9.9.9.9:1111"); code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", code, http.StatusTooManyRequests)
	}
}

func TestRateLimiting_DistributedModeNamespacesIndependentPluginInstances(t *testing.T) {
	// Two different plugin declarations - e.g. two different services, or
	// two rate_limiting blocks in the same service - get distinct
	// instanceIDs from plugin.Build (see gateway.buildMux) and must not
	// share counters for the same client.
	pA, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 1, "burst": 1, "mode": "distributed"}, "catalog:0")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}
	pB, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 1, "burst": 1, "mode": "distributed"}, "payments:0")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}

	const clientAddr = "8.8.8.8:2222"
	if code := serveRateLimited(pA, clientAddr); code != http.StatusOK {
		t.Fatalf("plugin A, first request: status = %d, want %d", code, http.StatusOK)
	}
	if code := serveRateLimited(pB, clientAddr); code != http.StatusOK {
		t.Fatalf("plugin B, first request: status = %d, want %d (must not be blocked by plugin A's counter)", code, http.StatusOK)
	}
}

// TestRateLimiting_DistributedModeSharesCounterAcrossInstantiationsWithSameID
// proves the actual point of "distributed" mode: two separate rateLimiting
// instances built from the same instanceID - as happens on every config
// hot-reload, and across every gateway process loading the same config -
// must share the same counter instead of each starting fresh.
func TestRateLimiting_DistributedModeSharesCounterAcrossInstantiationsWithSameID(t *testing.T) {
	cfg := map[string]any{"requests_per_second": 1, "burst": 1, "mode": "distributed"}

	first, err := newTestRateLimiting(t, cfg, "catalog:0")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}
	const clientAddr = "7.7.7.7:3333"
	if code := serveRateLimited(first, clientAddr); code != http.StatusOK {
		t.Fatalf("first instance, first request: status = %d, want %d", code, http.StatusOK)
	}

	reloaded, err := newTestRateLimiting(t, cfg, "catalog:0")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}
	if code := serveRateLimited(reloaded, clientAddr); code != http.StatusTooManyRequests {
		t.Fatalf("instance rebuilt with the same instanceID: status = %d, want %d (counter must survive a rebuild)", code, http.StatusTooManyRequests)
	}
}

func TestRateLimiting_MissingRequestsPerSecondErrors(t *testing.T) {
	if _, err := newRateLimiting(map[string]any{}, ""); err == nil {
		t.Fatal("newRateLimiting() error = nil, want non-nil when requests_per_second is missing")
	}
}

func TestRateLimiting_NonPositiveRequestsPerSecondErrors(t *testing.T) {
	if _, err := newRateLimiting(map[string]any{"requests_per_second": 0}, ""); err == nil {
		t.Fatal("newRateLimiting() error = nil, want non-nil when requests_per_second is not positive")
	}
}

func TestRateLimiting_BurstBelowOneErrors(t *testing.T) {
	if _, err := newRateLimiting(map[string]any{"requests_per_second": 5, "burst": 0}, ""); err == nil {
		t.Fatal("newRateLimiting() error = nil, want non-nil when burst is below 1")
	}
}

func TestRateLimiting_DefaultsBurstToRequestsPerSecond(t *testing.T) {
	p, err := newTestRateLimiting(t, map[string]any{"requests_per_second": 3}, "")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}
	if p.burst != 3 {
		t.Fatalf("burst = %v, want %v", p.burst, 3)
	}
}

func TestRateLimiting_Name(t *testing.T) {
	p, err := newRateLimiting(map[string]any{"requests_per_second": 1}, "")
	if err != nil {
		t.Fatalf("newRateLimiting() error = %v", err)
	}
	if p.Name() != "rate_limiting" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "rate_limiting")
	}
}
