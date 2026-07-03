package proxy

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Prober periodically checks each target's health by requesting path on its
// origin, marking it up/down in the target's originState.
type Prober struct {
	targets  []ProbeTarget
	path     string
	interval time.Duration
	client   *http.Client
}

// NewProber builds a Prober for targets, requesting path on each origin
// every interval and bounding each probe request by timeout.
func NewProber(targets []ProbeTarget, path string, interval, timeout time.Duration) *Prober {
	return &Prober{
		targets:  targets,
		path:     path,
		interval: interval,
		client:   &http.Client{Timeout: timeout},
	}
}

// Start probes immediately, then every interval, until ctx is canceled. It
// is intended to be run in its own goroutine.
func (p *Prober) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.probeOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probeOnce()
		}
	}
}

func (p *Prober) probeOnce() {
	for _, t := range p.targets {
		t.state.setForcedDown(!p.probe(t.URL))
	}
}

func (p *Prober) probe(originURL string) bool {
	resp, err := p.client.Get(strings.TrimSuffix(originURL, "/") + p.path)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
