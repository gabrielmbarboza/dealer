package tracing

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// NewTracerProvider builds a TracerProvider exporting spans via OTLP/HTTP.
// Configuration comes entirely from OpenTelemetry's own standard
// environment variables (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_
// HEADERS, etc. - see otlptracehttp's own docs) rather than DEALER_*
// ones, so Dealer interoperates with existing OTel tooling/collectors
// without reinventing that configuration surface.
//
// otlpEndpoint gates whether tracing is enabled at all: empty returns a
// nil provider and a no-op shutdown func, since the OTLP exporter's own
// default endpoint (localhost:4318) would otherwise have Dealer silently
// try to export spans nobody asked for. serviceName identifies this
// gateway instance in exported spans.
func NewTracerProvider(ctx context.Context, otlpEndpoint, serviceName string) (*sdktrace.TracerProvider, func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if otlpEndpoint == "" {
		return nil, noop, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(otlpEndpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("tracing: build OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, nil, fmt.Errorf("tracing: build resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	return tp, tp.Shutdown, nil
}

// SpanMiddleware wraps next in a span per request, named "METHOD path".
// tp is the OTel API's trace.TracerProvider interface, not the SDK-specific
// type, so this is safe to apply unconditionally: with the default no-op
// TracerProvider (when tracing is disabled), Start/End are no-ops and this
// middleware costs nothing beyond the interface call itself.
func SpanMiddleware(tp trace.TracerProvider) func(http.Handler) http.Handler {
	tracer := tp.Tracer("github.com/gabrielmbarboza/dealer/gateway")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)
			defer span.End()

			span.SetAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.path", r.URL.Path),
			)
			if id := FromContext(ctx); id != "" {
				span.SetAttributes(attribute.String("request.id", id))
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
