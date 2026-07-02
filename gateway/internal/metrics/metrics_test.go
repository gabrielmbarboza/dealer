package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecorder_WrapIncrementsRequestCounterWithLabels(t *testing.T) {
	r := New()
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	handler := r.Wrap("payments", next)
	req := httptest.NewRequest(http.MethodPost, "/payments", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	got := testutil.ToFloat64(r.requests.WithLabelValues("payments", "POST", "201"))
	if got != 1 {
		t.Fatalf("requests counter = %v, want 1", got)
	}
}

func TestRecorder_WrapDefaultsStatusTo200WhenWriteHeaderNeverCalled(t *testing.T) {
	r := New()
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	})

	handler := r.Wrap("catalog", next)
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	got := testutil.ToFloat64(r.requests.WithLabelValues("catalog", "GET", "200"))
	if got != 1 {
		t.Fatalf("requests counter = %v, want 1", got)
	}
}

func TestRecorder_WrapObservesDuration(t *testing.T) {
	r := New()
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := r.Wrap("catalog", next)
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	count := testutil.CollectAndCount(r.duration)
	if count != 1 {
		t.Fatalf("duration histogram series count = %d, want 1", count)
	}
}

func TestRecorder_HandlerServesExpositionFormat(t *testing.T) {
	r := New()
	handler := r.Wrap("catalog", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/catalog", nil))

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "dealer_http_requests_total") {
		t.Fatalf("body missing dealer_http_requests_total, got:\n%s", body)
	}
	if !strings.Contains(body, "dealer_http_request_duration_seconds") {
		t.Fatalf("body missing dealer_http_request_duration_seconds, got:\n%s", body)
	}
}
