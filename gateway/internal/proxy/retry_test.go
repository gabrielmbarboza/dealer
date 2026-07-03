package proxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

var errRetryableNet = &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}

type stubRoundTripper struct {
	mu     sync.Mutex
	calls  int
	bodies [][]byte

	errs      []error
	responses []*http.Response
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	i := s.calls
	s.calls++
	s.mu.Unlock()

	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, b)
		s.mu.Unlock()
	}

	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i < len(s.responses) {
		return s.responses[i], nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (s *stubRoundTripper) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newTestRetryTransport(next http.RoundTripper, maxAttempts int, retryUnsafe bool) *retryTransport {
	t := newRetryTransport(next, maxAttempts, time.Millisecond, retryUnsafe)
	t.sleep = func(time.Duration) {}
	return t
}

func TestRetryTransport_GETRetriesOnTransientErrorThenSucceeds(t *testing.T) {
	next := &stubRoundTripper{errs: []error{errRetryableNet}}
	rt := newTestRetryTransport(next, 3, false)

	req, _ := http.NewRequest(http.MethodGet, "http://origin.example/catalog", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := next.callCount(); got != 2 {
		t.Fatalf("call count = %d, want 2 (1 failure + 1 success)", got)
	}
}

func TestRetryTransport_ExhaustsAttemptsReturnsLastError(t *testing.T) {
	next := &stubRoundTripper{errs: []error{errRetryableNet, errRetryableNet}}
	rt := newTestRetryTransport(next, 2, false)

	req, _ := http.NewRequest(http.MethodGet, "http://origin.example/catalog", nil)
	_, err := rt.RoundTrip(req)
	if !errors.Is(err, errRetryableNet) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, errRetryableNet)
	}
	if got := next.callCount(); got != 2 {
		t.Fatalf("call count = %d, want 2 (maxAttempts reached)", got)
	}
}

func TestRetryTransport_DoesNotRetryPOSTByDefault(t *testing.T) {
	next := &stubRoundTripper{errs: []error{errRetryableNet}}
	rt := newTestRetryTransport(next, 3, false)

	req, _ := http.NewRequest(http.MethodPost, "http://origin.example/payments", strings.NewReader(`{}`))
	_, err := rt.RoundTrip(req)
	if !errors.Is(err, errRetryableNet) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, errRetryableNet)
	}
	if got := next.callCount(); got != 1 {
		t.Fatalf("call count = %d, want 1 (POST must not be retried by default)", got)
	}
}

func TestRetryTransport_RetriesPOSTWhenUnsafeMethodsEnabled(t *testing.T) {
	next := &stubRoundTripper{errs: []error{errRetryableNet}}
	rt := newTestRetryTransport(next, 3, true)

	req, _ := http.NewRequest(http.MethodPost, "http://origin.example/payments", strings.NewReader(`{}`))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := next.callCount(); got != 2 {
		t.Fatalf("call count = %d, want 2", got)
	}
}

func TestRetryTransport_DoesNotRetryNonNetworkErrors(t *testing.T) {
	boom := errors.New("boom")
	next := &stubRoundTripper{errs: []error{boom}}
	rt := newTestRetryTransport(next, 3, false)

	req, _ := http.NewRequest(http.MethodGet, "http://origin.example/catalog", nil)
	_, err := rt.RoundTrip(req)
	if !errors.Is(err, boom) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, boom)
	}
	if got := next.callCount(); got != 1 {
		t.Fatalf("call count = %d, want 1 (non-network errors must not be retried)", got)
	}
}

func TestRetryTransport_Non2xxResponseIsNotRetried(t *testing.T) {
	next := &stubRoundTripper{responses: []*http.Response{
		{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(""))},
	}}
	rt := newTestRetryTransport(next, 3, false)

	req, _ := http.NewRequest(http.MethodGet, "http://origin.example/catalog", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	if got := next.callCount(); got != 1 {
		t.Fatalf("call count = %d, want 1 (a valid HTTP response, even a 500, is not a RoundTrip error and must not be retried)", got)
	}
}

func TestRetryTransport_ReplaysBodyIdenticallyAcrossAttempts(t *testing.T) {
	next := &stubRoundTripper{errs: []error{errRetryableNet}}
	rt := newTestRetryTransport(next, 3, true)

	const body = `{"amount":100}`
	req, _ := http.NewRequest(http.MethodPost, "http://origin.example/payments", strings.NewReader(body))
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	if len(next.bodies) != 2 {
		t.Fatalf("len(bodies) = %d, want 2", len(next.bodies))
	}
	for i, b := range next.bodies {
		if string(b) != body {
			t.Fatalf("attempt %d body = %q, want %q", i, string(b), body)
		}
	}
}

func TestRetryTransport_MaxAttemptsOneDisablesRetry(t *testing.T) {
	next := &stubRoundTripper{errs: []error{errRetryableNet, nil}}
	rt := newTestRetryTransport(next, 1, false)

	req, _ := http.NewRequest(http.MethodGet, "http://origin.example/catalog", nil)
	_, err := rt.RoundTrip(req)
	if !errors.Is(err, errRetryableNet) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, errRetryableNet)
	}
	if got := next.callCount(); got != 1 {
		t.Fatalf("call count = %d, want 1 (maxAttempts=1 means retry is disabled)", got)
	}
}
