package tlscert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testPollInterval = 20 * time.Millisecond
const testWaitTimeout = 2 * time.Second

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

// writeCert generates a self-signed certificate for commonName and writes
// the cert/key PEM pair to certPath/keyPath.
func writeCert(t *testing.T, certPath, keyPath, commonName string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}
}

func TestNewReloader_LoadsInitialCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeCert(t, certPath, keyPath, "first")

	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewReloader() error = %v", err)
	}

	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	if got := x509Cert.Subject.CommonName; got != "first" {
		t.Fatalf("CommonName = %q, want %q", got, "first")
	}
}

func TestNewReloader_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewReloader(filepath.Join(dir, "missing-cert.pem"), filepath.Join(dir, "missing-key.pem")); err == nil {
		t.Fatal("NewReloader() error = nil, want error for missing files")
	}
}

func TestReloader_StartPicksUpCertRotation(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeCert(t, certPath, keyPath, "first")

	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewReloader() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx, testPollInterval)

	time.Sleep(testPollInterval * 2)
	writeCert(t, certPath, keyPath, "second")

	waitUntil(t, func() bool {
		cert, err := r.GetCertificate(nil)
		if err != nil {
			return false
		}
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		return err == nil && x509Cert.Subject.CommonName == "second"
	})
}

func TestReloader_StartIgnoresInvalidRotation(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeCert(t, certPath, keyPath, "first")

	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewReloader() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx, testPollInterval)

	time.Sleep(testPollInterval * 2)
	if err := os.WriteFile(certPath, []byte("not a certificate"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	time.Sleep(testPollInterval * 5)

	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	if got := x509Cert.Subject.CommonName; got != "first" {
		t.Fatalf("CommonName = %q, want %q (invalid rotation must not clear the previous cert)", got, "first")
	}
}

func TestReloader_StartStopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeCert(t, certPath, keyPath, "first")

	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewReloader() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Start(ctx, testPollInterval)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(testWaitTimeout):
		t.Fatal("Start() did not return after context cancellation")
	}
}
