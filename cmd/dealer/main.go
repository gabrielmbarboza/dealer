package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on http.DefaultServeMux, served only by the opt-in debug server
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

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

func main() {
	configPath := envOr("DEALER_CONFIG_PATH", "config.yml")

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
	})
	if err != nil {
		log.Fatalf("gateway: %v", err)
	}
	defer gw.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", infoHandler)
	mux.Handle("/", gw)

	srv := &http.Server{
		Addr:              "0.0.0.0:3000",
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.ListenAndServe()
	}()

	if debugSrv != nil {
		go func() {
			log.Printf("main: debug/pprof server listening on %s", debugSrv.Addr)
			if err := debugSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("main: debug server error: %v", err)
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
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("main: graceful shutdown failed: %v", err)
		}
	}
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
