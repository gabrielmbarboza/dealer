// Package acmecert gets and renews TLS certificates automatically over ACME
// (Let's Encrypt by default), one per host name the gateway serves, so each
// site on its own domain gets HTTPS without managing certificate files.
package acmecert

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// DefaultCacheDir is where certificates and the ACME account key are kept
// when Options.CacheDir is empty. It must survive restarts (a volume), or
// every restart requests new certificates and runs into the CA's rate limits.
const DefaultCacheDir = "acme-cache"

// Options configures the certificate manager.
type Options struct {
	// Email is the contact the CA uses for expiry and account notices.
	Email string
	// CacheDir keeps certificates and the account key between restarts.
	CacheDir string
	// DirectoryURL is the ACME directory; empty means Let's Encrypt
	// production. Use the staging directory while testing.
	DirectoryURL string
	// Allowed reports whether a certificate may be requested for a host:
	// only hosts the gateway actually serves, so a client can't make it
	// request certificates for arbitrary names.
	Allowed func(host string) bool
}

// New builds an autocert.Manager that accepts the CA's terms, caches on disk
// and requests certificates only for allowed hosts.
func New(opts Options) (*autocert.Manager, error) {
	if opts.Email == "" {
		return nil, errors.New("acmecert: an email is required")
	}
	if opts.Allowed == nil {
		return nil, errors.New("acmecert: a host check is required")
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = DefaultCacheDir
	}

	m := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(cacheDir),
		Email:  opts.Email,
		HostPolicy: func(_ context.Context, host string) error {
			if opts.Allowed(host) {
				return nil
			}
			return fmt.Errorf("acmecert: host %q is not served by this gateway", host)
		},
	}
	if opts.DirectoryURL != "" {
		m.Client = &acme.Client{DirectoryURL: opts.DirectoryURL}
	}
	return m, nil
}
