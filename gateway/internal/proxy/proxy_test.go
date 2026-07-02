package proxy

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 200 * time.Millisecond

type echoBody struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func newEchoOrigin() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		resp := echoBody{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   string(body),
			Headers: map[string]string{
				"X-Custom": r.Header.Get("X-Custom"),
			},
		}
		w.Header().Set("X-Origin-Response", "yes")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestNewReverseProxy_FullPassthrough(t *testing.T) {
	origin := newEchoOrigin()
	defer origin.Close()

	rp, err := NewReverseProxy("payments", origin.URL, testTimeout)
	if err != nil {
		t.Fatalf("NewReverseProxy() error = %v", err)
	}

	gateway := httptest.NewServer(rp)
	defer gateway.Close()

	reqBody := `{"amount":100}`
	req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/payments", strings.NewReader(reqBody))
	req.Header.Set("X-Custom", "gateway-value")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if resp.Header.Get("X-Origin-Response") != "yes" {
		t.Fatalf("X-Origin-Response header missing from response passthrough")
	}

	var got echoBody
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if got.Method != http.MethodPost {
		t.Fatalf("origin saw method = %q, want %q", got.Method, http.MethodPost)
	}
	if got.Path != "/payments" {
		t.Fatalf("origin saw path = %q, want %q (unstripped)", got.Path, "/payments")
	}
	if got.Body != reqBody {
		t.Fatalf("origin saw body = %q, want %q", got.Body, reqBody)
	}
	if got.Headers["X-Custom"] != "gateway-value" {
		t.Fatalf("origin saw X-Custom = %q, want %q", got.Headers["X-Custom"], "gateway-value")
	}
}

func TestNewReverseProxy_OriginDownReturnsCustomBadGateway(t *testing.T) {
	origin := newEchoOrigin()
	originURL := origin.URL
	origin.Close() // origin is down before any request reaches it

	rp, err := NewReverseProxy("payments", originURL, testTimeout)
	if err != nil {
		t.Fatalf("NewReverseProxy() error = %v", err)
	}

	gatewayServer := httptest.NewServer(rp)
	defer gatewayServer.Close()

	resp, err := http.Get(gatewayServer.URL + "/payments")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body["error"] != "bad_gateway" {
		t.Fatalf("body[error] = %q, want %q", body["error"], "bad_gateway")
	}
	if body["service"] != "payments" {
		t.Fatalf("body[service] = %q, want %q", body["service"], "payments")
	}
}

func TestNewReverseProxy_InvalidOriginURLErrors(t *testing.T) {
	if _, err := NewReverseProxy("broken", "://not-a-url", testTimeout); err == nil {
		t.Fatal("NewReverseProxy() error = nil, want non-nil for invalid origin_url")
	}
}

func TestNewReverseProxy_SlowOriginTimesOutWithBadGateway(t *testing.T) {
	unblock := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-unblock
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	defer close(unblock)

	rp, err := NewReverseProxy("slow", origin.URL, testTimeout)
	if err != nil {
		t.Fatalf("NewReverseProxy() error = %v", err)
	}

	gatewayServer := httptest.NewServer(rp)
	defer gatewayServer.Close()

	start := time.Now()
	resp, err := http.Get(gatewayServer.URL + "/slow")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("request took %v, want it to fail fast around the %v timeout", elapsed, testTimeout)
	}
}

// countingListener counts every accepted TCP connection, regardless of how
// many HTTP requests get multiplexed over it via keep-alive.
type countingListener struct {
	net.Listener
	accepts *int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		atomic.AddInt32(l.accepts, 1)
	}
	return c, err
}

// TestNewReverseProxy_ReusesConnectionsToOrigin guards against the origin
// Transport falling back to Go's DefaultMaxIdleConnsPerHost (2): under any
// real concurrency that default forces the pool to tear down and re-dial
// almost every connection, which is invisible in functional tests but shows
// up as connection churn (and, over a real network, latency) under load.
func TestNewReverseProxy_ReusesConnectionsToOrigin(t *testing.T) {
	const concurrency = 8
	const waves = 5

	var accepts int32
	arrived := make(chan struct{}, concurrency)
	var release atomic.Value // chan struct{}
	release.Store(make(chan struct{}))

	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-release.Load().(chan struct{})
		w.WriteHeader(http.StatusOK)
	}))
	origin.Listener = &countingListener{Listener: origin.Listener, accepts: &accepts}
	origin.Start()
	defer origin.Close()

	rp, err := NewReverseProxy("catalog", origin.URL, testTimeout)
	if err != nil {
		t.Fatalf("NewReverseProxy() error = %v", err)
	}

	gatewayServer := httptest.NewServer(rp)
	defer gatewayServer.Close()

	client := gatewayServer.Client()

	for w := 0; w < waves; w++ {
		wave := make(chan struct{})
		release.Store(wave)

		var wg sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := client.Get(gatewayServer.URL + "/catalog")
				if err != nil {
					t.Errorf("Get() error = %v", err)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}()
		}

		// Wait until all `concurrency` requests are simultaneously
		// in-flight at the origin before releasing them, forcing the
		// transport to hold that many connections open at once.
		for i := 0; i < concurrency; i++ {
			<-arrived
		}
		close(wave)
		wg.Wait()
	}

	got := atomic.LoadInt32(&accepts)
	// A pooled transport dials ~concurrency connections once and reuses
	// them across waves. The buggy default (MaxIdleConnsPerHost=2) closes
	// almost all of them after every wave, so accepts would climb toward
	// concurrency*waves instead.
	want := int32(concurrency + 2) // small slack for pool warm-up
	if got > want {
		t.Fatalf("origin accepted %d TCP connections for %d requests (%d waves x %d concurrent), want <= %d - connections are being re-dialed instead of pooled",
			got, concurrency*waves, waves, concurrency, want)
	}
}
