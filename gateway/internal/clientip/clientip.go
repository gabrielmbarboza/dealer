// Package clientip resolves the address of the client behind the gateway's
// direct peer, for plugins that act per client (rate_limiting). Behind a
// load balancer, every request comes from the balancer's address; with the
// balancer listed as a trusted proxy, the client is read from the
// X-Forwarded-For chain instead.
package clientip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type contextKey struct{}

var clientIPKey contextKey

// Resolver finds the client address, believing X-Forwarded-For only as far
// as the hops it was handed through are trusted proxies. A nil Resolver
// trusts nothing.
type Resolver struct {
	trusted []netip.Prefix
}

// Parse builds a Resolver from a comma-separated list of CIDRs and/or bare
// IPs (e.g. "10.0.0.0/8, 192.0.2.1"). An empty list trusts nothing.
func Parse(list string) (*Resolver, error) {
	r := &Resolver{}
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			r.trusted = append(r.trusted, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("clientip: invalid trusted proxy %q", entry)
		}
		r.trusted = append(r.trusted, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return r, nil
}

// Resolve returns the client address for r. The direct peer is the answer
// unless it is a trusted proxy; then X-Forwarded-For is walked from the
// right (the hop closest to the gateway), skipping trusted proxies, and the
// first untrusted address is the client. Entries left of it are whatever
// the client claimed and are never believed. A malformed entry stops the
// walk at the last good hop. If every hop is trusted, the leftmost wins.
func (res *Resolver) Resolve(r *http.Request) string {
	peer := peerAddr(r)
	if res == nil || len(res.trusted) == 0 {
		return peer
	}
	addr, err := netip.ParseAddr(peer)
	if err != nil || !res.isTrusted(addr) {
		return peer
	}

	hops := forwardedFor(r)
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(hops[i])
		if err != nil {
			return client
		}
		client = hop.Unmap().String()
		if !res.isTrusted(hop) {
			return client
		}
	}
	return client
}

func (res *Resolver) isTrusted(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range res.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// Middleware resolves the client address once per request and stores it in
// the request context for FromRequest.
func Middleware(res *Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), clientIPKey, res.Resolve(r))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FromRequest returns the client address Middleware stored, or the direct
// peer's address when Middleware never ran.
func FromRequest(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey).(string); ok && ip != "" {
		return ip
	}
	return peerAddr(r)
}

func peerAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func forwardedFor(r *http.Request) []string {
	var hops []string
	for _, value := range r.Header.Values("X-Forwarded-For") {
		for _, hop := range strings.Split(value, ",") {
			if hop = strings.TrimSpace(hop); hop != "" {
				hops = append(hops, hop)
			}
		}
	}
	return hops
}
