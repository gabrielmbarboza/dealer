package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testProbeInterval = 20 * time.Millisecond
const testProbeWaitTimeout = 2 * time.Second

func waitUntilProbe(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testProbeWaitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func newToggleableOrigin(healthy *atomic.Bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
}

func availableNow(s *originState) bool {
	return s.available(time.Now(), time.Hour, 0, 0)
}

func TestProber_ProbesImmediatelyWithoutWaitingForFirstTick(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(false)
	origin := newToggleableOrigin(&healthy)
	defer origin.Close()

	state := &originState{}
	p := NewProber([]ProbeTarget{{URL: origin.URL, state: state}}, "/healthz", time.Hour, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitUntilProbe(t, func() bool {
		return !availableNow(state)
	})
}

func TestProber_MarksOriginDownOnUnhealthyStatus(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(false)
	origin := newToggleableOrigin(&healthy)
	defer origin.Close()

	state := &originState{}
	p := NewProber([]ProbeTarget{{URL: origin.URL, state: state}}, "/healthz", testProbeInterval, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitUntilProbe(t, func() bool {
		return !availableNow(state)
	})
}

func TestProber_MarksOriginUpAfterRecovery(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(false)
	origin := newToggleableOrigin(&healthy)
	defer origin.Close()

	state := &originState{}
	p := NewProber([]ProbeTarget{{URL: origin.URL, state: state}}, "/healthz", testProbeInterval, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitUntilProbe(t, func() bool {
		return !availableNow(state)
	})

	healthy.Store(true)

	waitUntilProbe(t, func() bool {
		return availableNow(state)
	})
}

func TestProber_UnreachableOriginCountsAsDown(t *testing.T) {
	origin := newToggleableOrigin(&atomic.Bool{})
	origin.Close() // already unreachable

	state := &originState{}
	p := NewProber([]ProbeTarget{{URL: origin.URL, state: state}}, "/healthz", testProbeInterval, 200*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitUntilProbe(t, func() bool {
		return !availableNow(state)
	})
}

func TestProber_StartStopsOnContextCancel(t *testing.T) {
	state := &originState{}
	p := NewProber([]ProbeTarget{{URL: "http://0.0.0.0:1", state: state}}, "/healthz", testProbeInterval, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(testProbeWaitTimeout):
		t.Fatal("Start() did not return after context cancellation")
	}
}
