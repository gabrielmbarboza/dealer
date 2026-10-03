package acmecert

import (
	"context"
	"testing"
)

func allowOnly(hosts ...string) func(string) bool {
	return func(host string) bool {
		for _, h := range hosts {
			if h == host {
				return true
			}
		}
		return false
	}
}

func TestNew_RequiresAnEmailAndAHostCheck(t *testing.T) {
	if _, err := New(Options{Allowed: allowOnly("a.com")}); err == nil {
		t.Fatal("New() without email error = nil, want an error")
	}
	if _, err := New(Options{Email: "ops@katu.com.br"}); err == nil {
		t.Fatal("New() without Allowed error = nil, want an error")
	}
}

func TestNew_OnlyRequestsCertificatesForConfiguredHosts(t *testing.T) {
	m, err := New(Options{Email: "ops@katu.com.br", CacheDir: t.TempDir(), Allowed: allowOnly("lojadamaria.com.br")})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := m.HostPolicy(context.Background(), "lojadamaria.com.br"); err != nil {
		t.Fatalf("HostPolicy(configured host) error = %v, want nil", err)
	}
	if err := m.HostPolicy(context.Background(), "attacker.example"); err == nil {
		t.Fatal("HostPolicy(unknown host) error = nil, want a refusal")
	}
	if m.Email != "ops@katu.com.br" {
		t.Fatalf("Email = %q", m.Email)
	}
}

func TestNew_UsesTheDefaultCacheDirAndAnOptionalDirectory(t *testing.T) {
	m, err := New(Options{Email: "ops@katu.com.br", Allowed: allowOnly()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m.Cache == nil || m.Client != nil {
		t.Fatal("want the default directory cache and the default (production) ACME directory")
	}

	staging, err := New(Options{Email: "ops@katu.com.br", Allowed: allowOnly(), DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if staging.Client == nil || staging.Client.DirectoryURL != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Fatal("want the staging ACME directory")
	}
}
