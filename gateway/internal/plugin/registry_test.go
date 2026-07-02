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
