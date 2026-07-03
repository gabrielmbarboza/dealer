// Package tlscert loads a TLS certificate/key pair and keeps it fresh by
// polling both files on disk, so the gateway's TLS listener can pick up a
// renewed certificate without a restart.
package tlscert

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"
)

// Reloader loads a certFile/keyFile pair and serves it via GetCertificate,
// re-reading the files from disk whenever Start's poll loop notices either
// one has changed.
type Reloader struct {
	certFile string
	keyFile  string
	cert     atomic.Pointer[tls.Certificate]

	lastCertModTime time.Time
	lastKeyModTime  time.Time
}

// NewReloader loads certFile/keyFile immediately, failing if the pair is
// missing or invalid.
func NewReloader(certFile, keyFile string) (*Reloader, error) {
	r := &Reloader{certFile: certFile, keyFile: keyFile}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	if info, err := os.Stat(certFile); err == nil {
		r.lastCertModTime = info.ModTime()
	}
	if info, err := os.Stat(keyFile); err == nil {
		r.lastKeyModTime = info.ModTime()
	}
	return r, nil
}

// GetCertificate satisfies tls.Config.GetCertificate, returning whichever
// certificate was most recently loaded successfully.
func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.cert.Load(), nil
}

// Reload re-reads certFile/keyFile from disk and, if they parse as a valid
// pair, atomically swaps them in. A failure leaves the previously loaded
// certificate in place rather than clearing it.
func (r *Reloader) Reload() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("tlscert: load key pair: %w", err)
	}
	r.cert.Store(&cert)
	return nil
}

// Start polls certFile/keyFile every interval until ctx is canceled,
// reloading whenever either file's modification time changes. It is
// intended to be run in its own goroutine.
func (r *Reloader) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pollOnce()
		}
	}
}

func (r *Reloader) pollOnce() {
	certInfo, err := os.Stat(r.certFile)
	if err != nil {
		log.Printf("tlscert: stat %s: %v", r.certFile, err)
		return
	}
	keyInfo, err := os.Stat(r.keyFile)
	if err != nil {
		log.Printf("tlscert: stat %s: %v", r.keyFile, err)
		return
	}

	if certInfo.ModTime().Equal(r.lastCertModTime) && keyInfo.ModTime().Equal(r.lastKeyModTime) {
		return
	}

	if err := r.Reload(); err != nil {
		log.Printf("tlscert: reload: %v", err)
		return
	}

	r.lastCertModTime = certInfo.ModTime()
	r.lastKeyModTime = keyInfo.ModTime()
}
