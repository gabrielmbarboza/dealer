package main

import (
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

func writeTestCert(t *testing.T, certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}
}

func TestBuildTLSConfig_DisabledWhenBothEmpty(t *testing.T) {
	tlsConfig, reloader, err := buildTLSConfig("", "")
	if err != nil {
		t.Fatalf("buildTLSConfig() error = %v", err)
	}
	if tlsConfig != nil || reloader != nil {
		t.Fatalf("buildTLSConfig() = (%v, %v), want (nil, nil) when TLS is not configured", tlsConfig, reloader)
	}
}

func TestBuildTLSConfig_FailsWhenOnlyCertSet(t *testing.T) {
	if _, _, err := buildTLSConfig("cert.pem", ""); err == nil {
		t.Fatal("buildTLSConfig() error = nil, want error when only cert file is set")
	}
}

func TestBuildTLSConfig_FailsWhenOnlyKeySet(t *testing.T) {
	if _, _, err := buildTLSConfig("", "key.pem"); err == nil {
		t.Fatal("buildTLSConfig() error = nil, want error when only key file is set")
	}
}

func TestBuildTLSConfig_FailsOnInvalidPair(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := buildTLSConfig(filepath.Join(dir, "missing-cert.pem"), filepath.Join(dir, "missing-key.pem")); err == nil {
		t.Fatal("buildTLSConfig() error = nil, want error for missing cert/key files")
	}
}

func TestBuildTLSConfig_EnabledWithValidPair(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeTestCert(t, certPath, keyPath)

	tlsConfig, reloader, err := buildTLSConfig(certPath, keyPath)
	if err != nil {
		t.Fatalf("buildTLSConfig() error = %v", err)
	}
	if tlsConfig == nil || tlsConfig.GetCertificate == nil {
		t.Fatal("buildTLSConfig() tlsConfig.GetCertificate = nil, want a configured callback")
	}
	if reloader == nil {
		t.Fatal("buildTLSConfig() reloader = nil, want a non-nil Reloader to start polling")
	}
}

func TestNewDebugServer_DisabledWhenAddrEmpty(t *testing.T) {
	if s := newDebugServer(""); s != nil {
		t.Fatalf("newDebugServer(\"\") = %v, want nil (profiling must be opt-in)", s)
	}
}

func TestNewDebugServer_EnabledWithAddr(t *testing.T) {
	const addr = "127.0.0.1:6060"

	s := newDebugServer(addr)
	if s == nil {
		t.Fatal("newDebugServer() = nil, want a non-nil *http.Server")
	}
	if s.Addr != addr {
		t.Fatalf("Addr = %q, want %q", s.Addr, addr)
	}
	if s.Handler != nil {
		t.Fatalf("Handler = %v, want nil so it falls back to http.DefaultServeMux (where net/http/pprof registers itself)", s.Handler)
	}
}
