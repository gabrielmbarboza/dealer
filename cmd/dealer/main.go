package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on http.DefaultServeMux, served only by the opt-in debug server
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/gabrielmbarboza/cmd/dealer/internal/tlscert"
	dealer "github.com/gabrielmbarboza/dealer/config"
	"github.com/gabrielmbarboza/dealer/gateway"
)

// http.Server timeouts - guard against slow/hanging clients (e.g.
// Slowloris-style attacks) tying up connections indefinitely.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// DefaultTLSReloadInterval is used when DEALER_TLS_RELOAD_INTERVAL is not set.
const DefaultTLSReloadInterval = 30 * time.Second

// DefaultListenAddr is used when DEALER_LISTEN_ADDR is not set.
const DefaultListenAddr = "0.0.0.0:3000"

func main() {
	configPath := envOr("DEALER_CONFIG_PATH", "config.yml")
	listenAddr := envOr("DEALER_LISTEN_ADDR", DefaultListenAddr)

	pollInterval := gateway.DefaultPollInterval
	if raw := os.Getenv("DEALER_CONFIG_POLL_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_CONFIG_POLL_INTERVAL %q: %v", raw, err)
		}
		pollInterval = d
	}

	originTimeout := gateway.DefaultOriginTimeout
	if raw := os.Getenv("DEALER_ORIGIN_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_ORIGIN_TIMEOUT %q: %v", raw, err)
		}
		originTimeout = d
	}

	maxBodyBytes := gateway.DefaultMaxRequestBodyBytes
	if raw := os.Getenv("DEALER_MAX_BODY_BYTES"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			log.Fatalf("main: invalid DEALER_MAX_BODY_BYTES %q: %v", raw, err)
		}
		maxBodyBytes = n
	}

	unhealthyCooldown := gateway.DefaultUnhealthyCooldown
	if raw := os.Getenv("DEALER_UNHEALTHY_COOLDOWN"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_UNHEALTHY_COOLDOWN %q: %v", raw, err)
		}
		unhealthyCooldown = d
	}

	healthCheckInterval := gateway.DefaultHealthCheckInterval
	if raw := os.Getenv("DEALER_HEALTH_CHECK_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_HEALTH_CHECK_INTERVAL %q: %v", raw, err)
		}
		healthCheckInterval = d
	}

	healthCheckTimeout := gateway.DefaultHealthCheckTimeout
	if raw := os.Getenv("DEALER_HEALTH_CHECK_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_HEALTH_CHECK_TIMEOUT %q: %v", raw, err)
		}
		healthCheckTimeout = d
	}

	retryMaxAttempts := gateway.DefaultRetryMaxAttempts
	if raw := os.Getenv("DEALER_RETRY_MAX_ATTEMPTS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_RETRY_MAX_ATTEMPTS %q: %v", raw, err)
		}
		retryMaxAttempts = n
	}

	retryBackoffBase := gateway.DefaultRetryBackoffBase
	if raw := os.Getenv("DEALER_RETRY_BACKOFF_BASE"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_RETRY_BACKOFF_BASE %q: %v", raw, err)
		}
		retryBackoffBase = d
	}

	var breakerThreshold int
	if raw := os.Getenv("DEALER_CIRCUIT_BREAKER_THRESHOLD"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_CIRCUIT_BREAKER_THRESHOLD %q: %v", raw, err)
		}
		breakerThreshold = n
	}

	breakerCooldown := gateway.DefaultBreakerCooldown
	if raw := os.Getenv("DEALER_CIRCUIT_BREAKER_COOLDOWN"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_CIRCUIT_BREAKER_COOLDOWN %q: %v", raw, err)
		}
		breakerCooldown = d
	}

	var trustRequestID bool
	if raw := os.Getenv("DEALER_TRUST_REQUEST_ID"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_TRUST_REQUEST_ID %q: %v", raw, err)
		}
		trustRequestID = b
	}

	tlsConfig, certReloader, err := buildTLSConfig(os.Getenv("DEALER_TLS_CERT_FILE"), os.Getenv("DEALER_TLS_KEY_FILE"))
	if err != nil {
		log.Fatalf("main: %v", err)
	}
	tlsReloadInterval := DefaultTLSReloadInterval
	if raw := os.Getenv("DEALER_TLS_RELOAD_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_TLS_RELOAD_INTERVAL %q: %v", raw, err)
		}
		tlsReloadInterval = d
	}

	debugSrv := newDebugServer(os.Getenv("DEALER_DEBUG_ADDR"))
	if debugSrv != nil {
		// Both are 0 (disabled) unless a debug server was requested, so
		// this has no cost on the default/production path.
		runtime.SetMutexProfileFraction(1)
		runtime.SetBlockProfileRate(1)
	}

	gw, err := gateway.New(configPath, gateway.Options{
		PollInterval:        pollInterval,
		OriginTimeout:       originTimeout,
		MaxRequestBodyBytes: maxBodyBytes,
		UnhealthyCooldown:   unhealthyCooldown,
		HealthCheckPath:     os.Getenv("DEALER_HEALTH_CHECK_PATH"),
		HealthCheckInterval: healthCheckInterval,
		HealthCheckTimeout:  healthCheckTimeout,
		RetryMaxAttempts:    retryMaxAttempts,
		RetryBackoffBase:    retryBackoffBase,
		BreakerThreshold:    breakerThreshold,
		BreakerCooldown:     breakerCooldown,
		TrustRequestID:      trustRequestID,
		// OTLPEndpoint/ServiceName intentionally read OpenTelemetry's own
		// standard env vars, not DEALER_*-prefixed ones, for interop with
		// existing OTel tooling/collectors - see README.
		OTLPEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		ServiceName:  os.Getenv("OTEL_SERVICE_NAME"),
	})
	if err != nil {
		log.Fatalf("gateway: %v", err)
	}
	defer gw.Close()

	metricsSrv := newMetricsServer(os.Getenv("DEALER_METRICS_ADDR"), gw.MetricsHandler())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", infoHandler)
	mux.Handle("/", gw)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if certReloader != nil {
		go certReloader.Start(ctx, tlsReloadInterval)
	}

	serveErr := make(chan error, 1)
	go func() {
		if tlsConfig != nil {
			// Cert/key paths are already loaded into srv.TLSConfig via
			// GetCertificate, so empty strings here are correct - see
			// (*tls.Config).GetCertificate's doc comment.
			serveErr <- srv.ListenAndServeTLS("", "")
		} else {
			serveErr <- srv.ListenAndServe()
		}
	}()

	if debugSrv != nil {
		go func() {
			log.Printf("main: debug/pprof server listening on %s", debugSrv.Addr)
			if err := debugSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("main: debug server error: %v", err)
			}
		}()
	}

	if metricsSrv != nil {
		go func() {
			log.Printf("main: metrics server listening on %s", metricsSrv.Addr)
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("main: metrics server error: %v", err)
			}
		}()
	}

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("main: server error: %v", err)
		}
	case <-ctx.Done():
		log.Println("main: shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if debugSrv != nil {
			_ = debugSrv.Shutdown(shutdownCtx)
		}
		if metricsSrv != nil {
			_ = metricsSrv.Shutdown(shutdownCtx)
		}
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("main: graceful shutdown failed: %v", err)
		}
	}
}

// buildTLSConfig loads certFile/keyFile into a *tls.Config for the gateway's
// listener, along with the Reloader that keeps it fresh. Both empty returns
// (nil, nil, nil): TLS is opt-in, so the gateway serves plaintext by default.
// Exactly one of the two set is rejected outright rather than silently
// falling back to plaintext.
func buildTLSConfig(certFile, keyFile string) (*tls.Config, *tlscert.Reloader, error) {
	if certFile == "" && keyFile == "" {
		return nil, nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, nil, fmt.Errorf("DEALER_TLS_CERT_FILE and DEALER_TLS_KEY_FILE must be set together")
	}

	reloader, err := tlscert.NewReloader(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("tls: %w", err)
	}

	return &tls.Config{GetCertificate: reloader.GetCertificate}, reloader, nil
}

// newDebugServer returns an *http.Server exposing net/http/pprof on addr,
// or nil if addr is empty. Profiling is opt-in (via DEALER_DEBUG_ADDR) and
// served on its own listener - separate from the public gateway mux - so
// operators can bind it to localhost/an internal interface instead of
// exposing profiling data on the public port.
func newDebugServer(addr string) *http.Server {
	if addr == "" {
		return nil
	}
	return &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: readHeaderTimeout,
	}
}

func newMetricsServer(addr string, handler http.Handler) *http.Server {
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", handler)
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(dealer.ProjectInfo()); err != nil {
		log.Printf("main: encode info response: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
