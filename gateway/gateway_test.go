package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testPollInterval = 20 * time.Millisecond
const testWaitTimeout = 2 * time.Second

type echoBody struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body"`
}

func newEchoOrigin(t *testing.T, tag string, calls *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			atomic.AddInt32(calls, 1)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Origin", tag)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(echoBody{Method: r.Method, Path: r.URL.Path, Body: string(body)})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestGateway_FullPassthrough(t *testing.T) {
	origin := newEchoOrigin(t, "echo", nil)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "echo"
    path: "/echo"
    origin_url: %q
`, origin.URL))

	gw, err := New(configPath, Options{PollInterval: testPollInterval})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gw.Close)

	server := httptest.NewServer(gw)
	t.Cleanup(server.Close)

	const reqBody = `{"amount":100}`
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/echo", strings.NewReader(reqBody))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("X-Origin") != "echo" {
		t.Fatalf("X-Origin = %q, want %q", resp.Header.Get("X-Origin"), "echo")
	}

	var got echoBody
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Method != http.MethodPost || got.Path != "/echo" || got.Body != reqBody {
		t.Fatalf("got = %+v", got)
	}
}

func TestGateway_PathParamRouteIsUnstripped(t *testing.T) {
	origin := newEchoOrigin(t, "orders", nil)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "orders"
    path: "/orders/{id}"
    origin_url: %q
`, origin.URL))

	gw, err := New(configPath, Options{PollInterval: testPollInterval})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gw.Close)

	server := httptest.NewServer(gw)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/orders/42")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	var got echoBody
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Path != "/orders/42" {
		t.Fatalf("origin saw path = %q, want %q (unstripped)", got.Path, "/orders/42")
	}
}

func TestGateway_JWTAuthBlocksWithoutValidToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	var calls int32
	origin := newEchoOrigin(t, "payments", &calls)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "payments"
    path: "/payments"
    origin_url: %q
    plugins:
      - name: jwt_auth
        config:
          secret_env: JWT_SECRET
`, origin.URL))

	gw, err := New(configPath, Options{PollInterval: testPollInterval})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gw.Close)

	server := httptest.NewServer(gw)
	t.Cleanup(server.Close)

	resp, err := http.Post(server.URL+"/payments", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("origin call count = %d, want 0 (request should never reach it)", got)
	}
}

func TestGateway_HotReloadPicksUpNewOriginWithoutRestart(t *testing.T) {
	originA := newEchoOrigin(t, "A", nil)
	originB := newEchoOrigin(t, "B", nil)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "catalog"
    path: "/catalog"
    origin_url: %q
`, originA.URL))

	gw, err := New(configPath, Options{PollInterval: testPollInterval})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gw.Close)

	server := httptest.NewServer(gw)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/catalog")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("X-Origin"); got != "A" {
		t.Fatalf("X-Origin = %q, want %q before reload", got, "A")
	}

	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "catalog"
    path: "/catalog"
    origin_url: %q
`, originB.URL))

	waitUntil(t, func() bool {
		resp, err := http.Get(server.URL + "/catalog")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.Header.Get("X-Origin") == "B"
	})
}

func TestGateway_BadReloadKeepsServingPreviousConfig(t *testing.T) {
	origin := newEchoOrigin(t, "good", nil)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "catalog"
    path: "/catalog"
    origin_url: %q
`, origin.URL))

	gw, err := New(configPath, Options{PollInterval: testPollInterval})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gw.Close)

	server := httptest.NewServer(gw)
	t.Cleanup(server.Close)

	// Deliberately broken: missing origin_url.
	writeConfig(t, configPath, `
services:
  - name: "catalog"
    path: "/catalog"
`)
	time.Sleep(testPollInterval * 5)

	resp, err := http.Get(server.URL + "/catalog")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Origin") != "good" {
		t.Fatalf("X-Origin = %q, want %q (previous valid config should still be serving)", resp.Header.Get("X-Origin"), "good")
	}
}

func TestGateway_GlobalBodyLimitBlocksOversizedRequestWithoutPerServicePlugin(t *testing.T) {
	var calls int32
	origin := newEchoOrigin(t, "catalog", &calls)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "catalog"
    path: "/catalog"
    origin_url: %q
`, origin.URL))

	gw, err := New(configPath, Options{PollInterval: testPollInterval, MaxRequestBodyBytes: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gw.Close)

	server := httptest.NewServer(gw)
	t.Cleanup(server.Close)

	resp, err := http.Post(server.URL+"/catalog", "text/plain", strings.NewReader("this body is definitely over ten bytes"))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("origin call count = %d, want 0 (oversized request should never reach it)", got)
	}
}

func TestGateway_OriginTimeoutReturnsBadGatewayForSlowOrigin(t *testing.T) {
	unblock := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-unblock
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	defer close(unblock)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	writeConfig(t, configPath, fmt.Sprintf(`
services:
  - name: "slow"
    path: "/slow"
    origin_url: %q
`, origin.URL))

	gw, err := New(configPath, Options{PollInterval: testPollInterval, OriginTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(gw.Close)

	server := httptest.NewServer(gw)
	t.Cleanup(server.Close)

	start := time.Now()
	resp, err := http.Get(server.URL + "/slow")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("request took %v, want it to fail fast", elapsed)
	}
}
