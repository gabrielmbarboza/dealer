// Package proxy builds per-service reverse proxies that forward requests to
// internal origin services, preserving the incoming method, path, headers
// and body unchanged.
package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// NewReverseProxy builds a reverse proxy that forwards requests to
// originURL, keeping the request path unstripped (e.g. a request to
// /payments on the gateway reaches originURL + /payments on the origin).
// name is only used to identify the service in error responses/logs.
// timeout bounds both dialing the origin and waiting for its response
// headers, so a hung or unreachable origin fails fast with a 502 instead of
// tying up the gateway indefinitely.
func NewReverseProxy(name, originURL string, timeout time.Duration) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(originURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: invalid origin_url %q: %w", originURL, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("proxy: invalid origin_url %q: missing scheme or host", originURL)
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: timeout,
		}).DialContext,
		ResponseHeaderTimeout: timeout,
	}

	originalDirector := rp.Director
	rp.Director = func(r *http.Request) {
		userAgent := r.Header.Get("User-Agent")
		originalDirector(r)
		r.Header.Set("User-Agent", userAgent)
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy: service %q origin %s: %v", name, originURL, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		if encErr := json.NewEncoder(w).Encode(map[string]string{
			"error":   "bad_gateway",
			"service": name,
		}); encErr != nil {
			log.Printf("proxy: encode bad_gateway response: %v", encErr)
		}
	}

	return rp, nil
}
