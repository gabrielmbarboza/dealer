// Package gateway ties together config loading/hot-reload, routing,
// plugins and reverse proxying into a single http.Handler.
package gateway

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/gabrielmbarboza/dealer/gateway/internal/config"
	"github.com/gabrielmbarboza/dealer/gateway/internal/metrics"
	"github.com/gabrielmbarboza/dealer/gateway/internal/plugin"
	"github.com/gabrielmbarboza/dealer/gateway/internal/proxy"
	"github.com/gabrielmbarboza/dealer/gateway/internal/router"
	"github.com/gabrielmbarboza/dealer/gateway/internal/tracing"
)

// DefaultPollInterval is used when Options.PollInterval is not set.
const DefaultPollInterval = 2 * time.Second

// DefaultOriginTimeout is used when Options.OriginTimeout is not set.
const DefaultOriginTimeout = 10 * time.Second

// DefaultMaxRequestBodyBytes is used when Options.MaxRequestBodyBytes is
// not set.
const DefaultMaxRequestBodyBytes int64 = 10 << 20 // 10 MiB

const DefaultUnhealthyCooldown = 10 * time.Second

// DefaultHealthCheckInterval is used when a service's health_check.interval
// is not set. Only relevant when a health check path is configured.
const DefaultHealthCheckInterval = 10 * time.Second

// DefaultHealthCheckTimeout is used when Options.HealthCheckTimeout is not
// set. Only relevant when a health check path is configured.
const DefaultHealthCheckTimeout = 2 * time.Second

// DefaultRetryMaxAttempts is used when Options.RetryMaxAttempts is not set.
// 1 means retries are disabled: a request is attempted once.
const DefaultRetryMaxAttempts = 1

// DefaultRetryBackoffBase is used when Options.RetryBackoffBase is not set.
const DefaultRetryBackoffBase = 100 * time.Millisecond

// DefaultBreakerCooldown is used when Options.BreakerCooldown is not set.
// Only relevant when Options.BreakerThreshold is above 0.
const DefaultBreakerCooldown = 30 * time.Second

// Options configures a Gateway.
type Options struct {
	// PollInterval controls how often the config file is checked for
	// changes. Defaults to DefaultPollInterval when zero.
	PollInterval time.Duration

	// OriginTimeout bounds dialing an internal service and waiting for its
	// response headers, so a hung or unreachable origin fails fast instead
	// of tying up the gateway indefinitely. Defaults to DefaultOriginTimeout
	// when zero.
	OriginTimeout time.Duration

	// MaxRequestBodyBytes caps the request body size for every service,
	// regardless of whether that service configures its own
	// request_size_limiting plugin - a safety net for services that forget
	// to. A service's own plugin can still enforce a stricter limit.
	// Defaults to DefaultMaxRequestBodyBytes when zero.
	MaxRequestBodyBytes int64

	UnhealthyCooldown time.Duration

	// HealthCheckPath is the gateway-wide default path probed for active
	// health checks, used when a service's health_check block doesn't set
	// its own path. Empty by default: active health checks only run for a
	// service if it (or this default) resolves to a non-empty path.
	HealthCheckPath string

	// HealthCheckInterval is the gateway-wide default polling interval for
	// active health checks. Defaults to DefaultHealthCheckInterval when
	// zero.
	HealthCheckInterval time.Duration

	// HealthCheckTimeout bounds each individual health check request.
	// Defaults to DefaultHealthCheckTimeout when zero.
	HealthCheckTimeout time.Duration

	// RetryMaxAttempts is the gateway-wide total number of attempts per
	// request (including the first) on transient network errors. Defaults
	// to DefaultRetryMaxAttempts (1, i.e. disabled) when zero. Only
	// idempotent methods (GET/HEAD/OPTIONS) are retried unless a service
	// sets retry_unsafe_methods: true.
	RetryMaxAttempts int

	// RetryBackoffBase is the base delay between retry attempts; actual
	// delay grows exponentially with jitter added. Defaults to
	// DefaultRetryBackoffBase when zero.
	RetryBackoffBase time.Duration

	// BreakerThreshold is the gateway-wide number of consecutive failures
	// that trips an origin's circuit breaker open, fast-failing without
	// dialing it until BreakerCooldown elapses. <= 0 (the default)
	// disables the breaker entirely.
	BreakerThreshold int

	// BreakerCooldown is how long a tripped breaker stays open before
	// allowing a half-open trial request through; it escalates
	// exponentially on each further failed trial. Defaults to
	// DefaultBreakerCooldown when zero.
	BreakerCooldown time.Duration

	// TrustRequestID reuses an inbound X-Request-Id header instead of
	// always generating a fresh one. Only safe when the gateway sits
	// behind a trusted upstream (e.g. a load balancer that sets this
	// header itself) - otherwise off by default, since a client could
	// inject an arbitrary id into the gateway's logs otherwise.
	TrustRequestID bool

	// OTLPEndpoint enables OpenTelemetry tracing when set, exporting a
	// span per request over OTLP/HTTP to this endpoint. Empty (the
	// default) disables tracing entirely at effectively zero cost, since
	// the OTel API's default TracerProvider is a no-op.
	OTLPEndpoint string

	// ServiceName identifies this gateway instance in exported spans.
	// Defaults to "dealer" when empty. Only relevant when OTLPEndpoint is
	// set.
	ServiceName string
}

// resolvedOptions bundles every Options field after defaults have been
// applied, so buildMux doesn't need an ever-growing positional parameter
// list as new hardening knobs are added.
type resolvedOptions struct {
	originTimeout       time.Duration
	maxBodyBytes        int64
	unhealthyCooldown   time.Duration
	healthCheckPath     string
	healthCheckInterval time.Duration
	healthCheckTimeout  time.Duration
	retryMaxAttempts    int
	retryBackoffBase    time.Duration
	breakerThreshold    int
	breakerCooldown     time.Duration
}

// Gateway is an http.Handler that forwards requests to internal services
// as described by a hot-reloadable YAML config file.
type Gateway struct {
	mux     atomic.Pointer[http.ServeMux]
	cancel  context.CancelFunc
	metrics *metrics.Recorder
	handler http.Handler

	// proberCancel stops the previous generation's active health-check
	// goroutines. Each config (re)load starts a fresh set of Probers under
	// a new context and cancels whichever one this pointer previously held,
	// so hot-reload never leaks a generation's probers.
	proberCancel atomic.Pointer[context.CancelFunc]

	// tracingShutdown flushes and stops the OpenTelemetry TracerProvider.
	// A no-op when tracing was never enabled (Options.OTLPEndpoint empty).
	tracingShutdown func(context.Context) error
}

// New loads configPath, builds the initial routing table, and starts a
// background watcher that hot-reloads the config on file changes.
func New(configPath string, opts Options) (*Gateway, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	gw := &Gateway{metrics: metrics.New()}

	serviceName := opts.ServiceName
	if serviceName == "" {
		serviceName = "dealer"
	}
	tp, tracingShutdown, err := tracing.NewTracerProvider(context.Background(), opts.OTLPEndpoint, serviceName)
	if err != nil {
		return nil, fmt.Errorf("tracing: %w", err)
	}
	gw.tracingShutdown = tracingShutdown

	var tracerProvider trace.TracerProvider = noop.NewTracerProvider()
	if tp != nil {
		tracerProvider = tp
	}

	gw.handler = tracing.Middleware(opts.TrustRequestID)(tracing.SpanMiddleware(tracerProvider)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gw.mux.Load().ServeHTTP(w, r)
	})))

	resolved := resolvedOptions{
		originTimeout:       opts.OriginTimeout,
		maxBodyBytes:        opts.MaxRequestBodyBytes,
		unhealthyCooldown:   opts.UnhealthyCooldown,
		healthCheckPath:     opts.HealthCheckPath,
		healthCheckInterval: opts.HealthCheckInterval,
		healthCheckTimeout:  opts.HealthCheckTimeout,
		retryMaxAttempts:    opts.RetryMaxAttempts,
		retryBackoffBase:    opts.RetryBackoffBase,
		breakerThreshold:    opts.BreakerThreshold,
		breakerCooldown:     opts.BreakerCooldown,
	}
	if resolved.originTimeout <= 0 {
		resolved.originTimeout = DefaultOriginTimeout
	}
	if resolved.maxBodyBytes <= 0 {
		resolved.maxBodyBytes = DefaultMaxRequestBodyBytes
	}
	if resolved.unhealthyCooldown <= 0 {
		resolved.unhealthyCooldown = DefaultUnhealthyCooldown
	}
	if resolved.healthCheckInterval <= 0 {
		resolved.healthCheckInterval = DefaultHealthCheckInterval
	}
	if resolved.healthCheckTimeout <= 0 {
		resolved.healthCheckTimeout = DefaultHealthCheckTimeout
	}
	if resolved.retryMaxAttempts <= 0 {
		resolved.retryMaxAttempts = DefaultRetryMaxAttempts
	}
	if resolved.retryBackoffBase <= 0 {
		resolved.retryBackoffBase = DefaultRetryBackoffBase
	}
	if resolved.breakerCooldown <= 0 {
		resolved.breakerCooldown = DefaultBreakerCooldown
	}

	mux, probers, err := buildMux(cfg, resolved, gw.metrics)
	if err != nil {
		return nil, err
	}
	gw.mux.Store(mux)
	gw.startProbers(probers)

	interval := opts.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	watcher := config.NewWatcher(configPath, interval, func(newCfg *config.Config) error {
		newMux, newProbers, err := buildMux(newCfg, resolved, gw.metrics)
		if err != nil {
			return err
		}
		gw.mux.Store(newMux)
		gw.startProbers(newProbers)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	gw.cancel = cancel
	go watcher.Start(ctx)

	return gw, nil
}

// startProbers starts probers under a fresh context and cancels whichever
// generation's probers were previously running.
func (g *Gateway) startProbers(probers []*proxy.Prober) {
	ctx, cancel := context.WithCancel(context.Background())
	for _, p := range probers {
		go p.Start(ctx)
	}
	if oldCancel := g.proberCancel.Swap(&cancel); oldCancel != nil {
		(*oldCancel)()
	}
}

// ServeHTTP assigns a request id (see tracing.Middleware), then dispatches
// to the currently active routing table.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}

// Close stops the background config watcher, the current generation's
// active health-check probers, and flushes/stops OpenTelemetry tracing.
func (g *Gateway) Close() {
	if g.cancel != nil {
		g.cancel()
	}
	if cancel := g.proberCancel.Load(); cancel != nil {
		(*cancel)()
	}
	if g.tracingShutdown != nil {
		_ = g.tracingShutdown(context.Background())
	}
}

func (g *Gateway) MetricsHandler() http.Handler {
	return g.metrics.Handler()
}

// buildMux compiles cfg into a *http.ServeMux, wiring each service's
// plugins (in declared order) in front of its reverse proxy. A global
// request_size_limiting plugin is prepended for every service so
// maxBodyBytes applies even when a service doesn't configure its own. It
// also returns a Prober per service that resolves to a non-empty health
// check path, ready to be started by the caller.
func buildMux(cfg *config.Config, opts resolvedOptions, recorder *metrics.Recorder) (*http.ServeMux, []*proxy.Prober, error) {
	var probers []*proxy.Prober

	mux, err := router.Build(cfg, func(svc config.Service) (http.Handler, error) {
		plugins := make([]plugin.Plugin, 0, len(svc.Plugins)+1)
		plugins = append(plugins, plugin.NewRequestSizeLimiting(opts.maxBodyBytes))
		for _, pc := range svc.Plugins {
			p, err := plugin.Build(pc.Name, pc.Config)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: %w", pc.Name, err)
			}
			plugins = append(plugins, p)
		}

		origins := svc.OriginURLs
		if len(origins) == 0 {
			origins = []string{svc.OriginURL}
		}
		retry := proxy.RetryOptions{
			MaxAttempts:        opts.retryMaxAttempts,
			BackoffBase:        opts.retryBackoffBase,
			RetryUnsafeMethods: svc.RetryUnsafeMethods,
		}
		breaker := proxy.BreakerOptions{
			Threshold: opts.breakerThreshold,
			Cooldown:  opts.breakerCooldown,
		}
		rp, err := proxy.NewOriginProxy(svc.Name, origins, opts.originTimeout, opts.unhealthyCooldown, retry, breaker)
		if err != nil {
			return nil, err
		}

		if svc.HealthCheck != nil {
			path := opts.healthCheckPath
			if svc.HealthCheck.Path != "" {
				path = svc.HealthCheck.Path
			}
			interval := opts.healthCheckInterval
			if svc.HealthCheck.Interval != "" {
				d, err := time.ParseDuration(svc.HealthCheck.Interval)
				if err != nil {
					return nil, fmt.Errorf("service %q: health_check.interval: %w", svc.Name, err)
				}
				interval = d
			}
			if path != "" {
				probers = append(probers, proxy.NewProber(proxy.ProbeTargets(rp), path, interval, opts.healthCheckTimeout))
			}
		}

		return recorder.Wrap(svc.Name, plugin.Chain(plugins, rp)), nil
	})
	if err != nil {
		return nil, nil, err
	}

	return mux, probers, nil
}
