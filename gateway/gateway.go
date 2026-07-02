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

// Options configures a Gateway.
type Options struct {
	// PollInterval controls how often the config file is checked for
	// changes. Defaults to DefaultPollInterval when zero.
	PollInterval time.Duration
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

	mux, err := buildMux(cfg)
	if err != nil {
		return nil, err
	}
	gw.mux.Store(mux)

	interval := opts.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	watcher := config.NewWatcher(configPath, interval, func(newCfg *config.Config) error {
		newMux, err := buildMux(newCfg)
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
// plugins (in declared order) in front of its reverse proxy.
func buildMux(cfg *config.Config) (*http.ServeMux, error) {
	return router.Build(cfg, func(svc config.Service) (http.Handler, error) {
		plugins := make([]plugin.Plugin, 0, len(svc.Plugins))
		for _, pc := range svc.Plugins {
			p, err := plugin.Build(pc.Name, pc.Config)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: %w", pc.Name, err)
			}
			plugins = append(plugins, p)
		}

		rp, err := proxy.NewReverseProxy(svc.Name, svc.OriginURL)
		if err != nil {
			return nil, err
		}

		return plugin.Chain(plugins, rp), nil
	})
}
