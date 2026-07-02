// Command stubs runs lightweight HTTP stand-ins for the catalog, payments
// and orders origins declared in config.yml, so the gateway's own overhead
// can be load tested in isolation from real backend latency/variance.
//
// Each origin counts accepted TCP connections separately from HTTP requests
// served, exposed at /__stats. A healthy connection pool on the gateway's
// side keeps accepts well below requests (connections get reused); accepts
// tracking requests 1:1 is a sign of connection churn.
package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

type origin struct {
	name     string
	addr     string
	latency  time.Duration
	status   int
	accepts  atomic.Int64
	requests atomic.Int64
}

func main() {
	origins := []*origin{
		{
			name:    "catalog",
			addr:    envOr("CATALOG_ADDR", "0.0.0.0:3001"),
			latency: durationEnvOr("CATALOG_LATENCY", 15*time.Millisecond),
			status:  http.StatusOK,
		},
		{
			name:    "payments",
			addr:    envOr("PAYMENTS_ADDR", "0.0.0.0:3002"),
			latency: durationEnvOr("PAYMENTS_LATENCY", 40*time.Millisecond),
			status:  http.StatusCreated,
		},
		{
			name:    "orders",
			addr:    envOr("ORDERS_ADDR", "0.0.0.0:3003"),
			latency: durationEnvOr("ORDERS_LATENCY", 25*time.Millisecond),
			status:  http.StatusOK,
		},
	}

	errc := make(chan error, len(origins))
	for _, o := range origins {
		o := o
		go func() {
			ln, err := net.Listen("tcp", o.addr)
			if err != nil {
				errc <- err
				return
			}
			log.Printf("stubs: %s listening on %s (latency=%s)", o.name, o.addr, o.latency)
			errc <- http.Serve(&countingListener{Listener: ln, accepts: &o.accepts}, o.mux())
		}()
	}

	log.Fatal(<-errc)
}

// mux serves the origin's business handler plus a /__stats endpoint used to
// inspect TCP-accept vs HTTP-request counts during a load test.
func (o *origin) mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/__stats", o.statsHandler)
	mux.HandleFunc("/", o.handler)
	return mux
}

func (o *origin) statsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int64{
		"accepts":  o.accepts.Load(),
		"requests": o.requests.Load(),
	})
}

// handler replies after o.latency with a small JSON body, simulating a
// downstream service that does a fixed amount of work per request.
func (o *origin) handler(w http.ResponseWriter, r *http.Request) {
	o.requests.Add(1)
	time.Sleep(o.latency)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(o.status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": o.name,
		"method":  r.Method,
		"path":    r.URL.Path,
		"status":  "ok",
	})
}

// countingListener counts every accepted TCP connection, regardless of how
// many HTTP requests get multiplexed over it via keep-alive.
type countingListener struct {
	net.Listener
	accepts *atomic.Int64
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepts.Add(1)
	}
	return c, err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnvOr(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("stubs: invalid %s %q: %v", key, v, err)
	}
	return d
}
