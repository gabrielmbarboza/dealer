package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewTracerProvider_NilWhenEndpointEmpty(t *testing.T) {
	tp, shutdown, err := NewTracerProvider(context.Background(), "", "dealer")
	if err != nil {
		t.Fatalf("NewTracerProvider() error = %v", err)
	}
	if tp != nil {
		t.Fatalf("tp = %v, want nil when otlpEndpoint is empty", tp)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v, want nil no-op", err)
	}
}

func TestNewTracerProvider_BuildsProviderWhenEndpointSet(t *testing.T) {
	tp, shutdown, err := NewTracerProvider(context.Background(), "http://127.0.0.1:4318", "dealer")
	if err != nil {
		t.Fatalf("NewTracerProvider() error = %v", err)
	}
	if tp == nil {
		t.Fatal("tp = nil, want a real TracerProvider when otlpEndpoint is set")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
}

func TestSpanMiddleware_CreatesSpanForRequest(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SpanMiddleware(tp)(next)
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("len(spans) = %d, want 1", len(spans))
	}
	if spans[0].Name != "GET /catalog" {
		t.Fatalf("span name = %q, want %q", spans[0].Name, "GET /catalog")
	}
}

func TestSpanMiddleware_IncludesRequestIDAttributeWhenPresent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Mirrors gateway.go's composition: request-id middleware runs before
	// (outside) the span middleware.
	handler := Middleware(false)(SpanMiddleware(tp)(next))
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("len(spans) = %d, want 1", len(spans))
	}
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "request.id" && attr.Value.AsString() != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("span attributes missing a non-empty request.id")
	}
}

func TestSpanMiddleware_NoopProviderStillCallsNext(t *testing.T) {
	var provider trace.TracerProvider = noop.NewTracerProvider()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := SpanMiddleware(provider)(next)
	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called with a no-op TracerProvider")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
