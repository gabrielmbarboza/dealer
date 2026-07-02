package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Recorder struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func New() *Recorder {
	registry := prometheus.NewRegistry()

	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dealer_http_requests_total",
		Help: "Total requests handled per service, method and status code.",
	}, []string{"service", "method", "status"})

	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dealer_http_request_duration_seconds",
		Help:    "Request duration in seconds per service and method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"service", "method"})

	registry.MustRegister(requests, duration)

	return &Recorder{registry: registry, requests: requests, duration: duration}
}

func (r *Recorder) Wrap(service string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, req)

		r.duration.WithLabelValues(service, req.Method).Observe(time.Since(start).Seconds())
		r.requests.WithLabelValues(service, req.Method, strconv.Itoa(sw.status)).Inc()
	})
}

func (r *Recorder) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
