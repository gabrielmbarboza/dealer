// Package gateway ties together config loading/hot-reload, routing,
// plugins and reverse proxying into a single http.Handler.
package gateway

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gabrielmbarboza/dealer/gateway/internal/config"
	"github.com/gabrielmbarboza/dealer/gateway/internal/plugin"
	"github.com/gabrielmbarboza/dealer/gateway/internal/proxy"
	"github.com/gabrielmbarboza/dealer/gateway/internal/router"
)

// DefaultPollInterval is used when Options.PollInterval is not set.
const DefaultPollInterval = 2 * time.Second

// DefaultOriginTimeout is used when Options.OriginTimeout is not set.
const DefaultOriginTimeout = 10 * time.Second

// DefaultMaxRequestBodyBytes is used when Options.MaxRequestBodyBytes is
// not set.
const DefaultMaxRequestBodyBytes int64 = 10 << 20 // 10 MiB

const DefaultUnhealthyCooldown = 10 * time.Second

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
}

// Gateway is an http.Handler that forwards requests to internal services
// as described by a hot-reloadable YAML config file.
type Gateway struct {
	mux    atomic.Pointer[http.ServeMux]
	cancel context.CancelFunc
}

// New loads configPath, builds the initial routing table, and starts a
// background watcher that hot-reloads the config on file changes.
func New(configPath string, opts Options) (*Gateway, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	gw := &Gateway{}

	originTimeout := opts.OriginTimeout
	if originTimeout <= 0 {
		originTimeout = DefaultOriginTimeout
	}
	maxBodyBytes := opts.MaxRequestBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = DefaultMaxRequestBodyBytes
	}
	unhealthyCooldown := opts.UnhealthyCooldown
	if unhealthyCooldown <= 0 {
		unhealthyCooldown = DefaultUnhealthyCooldown
	}

	mux, err := buildMux(cfg, originTimeout, maxBodyBytes, unhealthyCooldown)
	if err != nil {
		return nil, err
	}
	gw.mux.Store(mux)

	interval := opts.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	watcher := config.NewWatcher(configPath, interval, func(newCfg *config.Config) error {
		newMux, err := buildMux(newCfg, originTimeout, maxBodyBytes, unhealthyCooldown)
		if err != nil {
			return err
		}
		gw.mux.Store(newMux)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	gw.cancel = cancel
	go watcher.Start(ctx)

	return gw, nil
}

// ServeHTTP dispatches to the currently active routing table.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mux.Load().ServeHTTP(w, r)
}

// Close stops the background config watcher.
func (g *Gateway) Close() {
	if g.cancel != nil {
		g.cancel()
	}
}

// buildMux compiles cfg into a *http.ServeMux, wiring each service's
// plugins (in declared order) in front of its reverse proxy. A global
// request_size_limiting plugin is prepended for every service so
// maxBodyBytes applies even when a service doesn't configure its own.
func buildMux(cfg *config.Config, originTimeout time.Duration, maxBodyBytes int64, unhealthyCooldown time.Duration) (*http.ServeMux, error) {
	return router.Build(cfg, func(svc config.Service) (http.Handler, error) {
		plugins := make([]plugin.Plugin, 0, len(svc.Plugins)+1)
		plugins = append(plugins, plugin.NewRequestSizeLimiting(maxBodyBytes))
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
		rp, err := proxy.NewOriginProxy(svc.Name, origins, originTimeout, unhealthyCooldown)
		if err != nil {
			return nil, err
		}

		return plugin.Chain(plugins, rp), nil
	})
}
