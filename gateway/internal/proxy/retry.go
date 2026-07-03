package proxy

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"time"
)

// retryTransport wraps an http.RoundTripper, retrying a request when the
// round trip itself fails with a transient network error. A response that
// came back successfully - even a 4xx/5xx one - is not a RoundTrip error
// and is never retried; that's the origin answering, not a failure to
// reach it.
type retryTransport struct {
	next        http.RoundTripper
	maxAttempts int
	retryUnsafe bool
	backoffBase time.Duration
	sleep       func(time.Duration)
}

// newRetryTransport builds a retryTransport. maxAttempts <= 1 disables
// retries entirely. Only idempotent methods (GET/HEAD/OPTIONS) are retried
// unless retryUnsafeMethods is set.
func newRetryTransport(next http.RoundTripper, maxAttempts int, backoffBase time.Duration, retryUnsafeMethods bool) *retryTransport {
	return &retryTransport{
		next:        next,
		maxAttempts: maxAttempts,
		retryUnsafe: retryUnsafeMethods,
		backoffBase: backoffBase,
		sleep:       time.Sleep,
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.maxAttempts <= 1 || !t.methodIsRetryable(req.Method) {
		return t.next.RoundTrip(req)
	}

	var bodyBytes []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		bodyBytes = b
	}

	var resp *http.Response
	var err error
	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err = t.next.RoundTrip(req)
		if err == nil || attempt == t.maxAttempts || !isRetryableError(err) {
			return resp, err
		}
		t.sleep(t.backoff(attempt))
	}
	return resp, err
}

func (t *retryTransport) methodIsRetryable(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return t.retryUnsafe
	}
}

func (t *retryTransport) backoff(attempt int) time.Duration {
	d := t.backoffBase << (attempt - 1)
	return d + time.Duration(rand.Int63n(int64(t.backoffBase)+1))
}

func isRetryableError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}
