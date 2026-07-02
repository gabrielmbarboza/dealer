package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
