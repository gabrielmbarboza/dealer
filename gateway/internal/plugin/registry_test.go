package plugin

import "testing"

func TestBuild_KnownPluginNames(t *testing.T) {
	names := []string{"add_header", "http_log"}
	for _, name := range names {
		p, err := Build(name, map[string]any{})
		if err != nil {
			t.Fatalf("Build(%q) error = %v", name, err)
		}
		if p.Name() != name {
			t.Fatalf("Build(%q).Name() = %q", name, p.Name())
		}
	}
}

func TestBuild_UnknownPluginNameErrors(t *testing.T) {
	if _, err := Build("does_not_exist", map[string]any{}); err == nil {
		t.Fatal("Build() error = nil, want non-nil for an unknown plugin name")
	}
}

func TestBuild_PropagatesFactoryError(t *testing.T) {
	if _, err := Build("request_size_limiting", map[string]any{}); err == nil {
		t.Fatal("Build() error = nil, want non-nil when required config is missing")
	}
}

func TestBuild_RateLimiting(t *testing.T) {
	p, err := Build("rate_limiting", map[string]any{"requests_per_second": 5})
	if err != nil {
		t.Fatalf("Build(%q) error = %v", "rate_limiting", err)
	}
	if p.Name() != "rate_limiting" {
		t.Fatalf("Build(%q).Name() = %q", "rate_limiting", p.Name())
	}
}

func TestBuild_CORS(t *testing.T) {
	p, err := Build("cors", map[string]any{"allowed_origins": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("Build(%q) error = %v", "cors", err)
	}
	if p.Name() != "cors" {
		t.Fatalf("Build(%q).Name() = %q", "cors", p.Name())
	}
}
