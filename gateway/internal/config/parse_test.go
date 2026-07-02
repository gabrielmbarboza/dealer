package config

import (
	"os"
	"path/filepath"
	"testing"
)

const fullExampleYAML = `
services:
  - name: "catalog"
    path: "/catalog"
    origin_url: "http://0.0.0.0:3001"
    methods: ["GET", "POST"]
    plugins:
      - name: http_log
      - name: add_header
        config:
          headers:
            X-Gateway: "dealer"
      - name: request_size_limiting
        config:
          max_bytes: 1048576

  - name: payments
    path: "/payments"
    origin_url: "http://0.0.0.0:3002"
    methods: ["POST"]
    plugins:
      - name: jwt_auth
        config:
          secret_env: JWT_SECRET

  - name: orders
    path: "/orders/{id}"
    origin_url: "http://0.0.0.0:3003"
    methods: ["GET", "PUT"]
`

func TestParse_FullExampleRoundTrips(t *testing.T) {
	cfg, err := Parse([]byte(fullExampleYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(cfg.Services) != 3 {
		t.Fatalf("len(Services) = %d, want 3", len(cfg.Services))
	}

	catalog := cfg.Services[0]
	if catalog.Name != "catalog" || catalog.Path != "/catalog" || catalog.OriginURL != "http://0.0.0.0:3001" {
		t.Fatalf("catalog service = %+v", catalog)
	}
	if len(catalog.Methods) != 2 || catalog.Methods[0] != "GET" || catalog.Methods[1] != "POST" {
		t.Fatalf("catalog.Methods = %v", catalog.Methods)
	}
	if len(catalog.Plugins) != 3 {
		t.Fatalf("len(catalog.Plugins) = %d, want 3", len(catalog.Plugins))
	}
	if catalog.Plugins[1].Name != "add_header" {
		t.Fatalf("catalog.Plugins[1].Name = %q, want add_header", catalog.Plugins[1].Name)
	}
	headers, ok := catalog.Plugins[1].Config["headers"].(map[string]any)
	if !ok || headers["X-Gateway"] != "dealer" {
		t.Fatalf("catalog.Plugins[1].Config = %+v", catalog.Plugins[1].Config)
	}

	payments := cfg.Services[1]
	if len(payments.Plugins) != 1 || payments.Plugins[0].Name != "jwt_auth" {
		t.Fatalf("payments.Plugins = %+v", payments.Plugins)
	}
	if payments.Plugins[0].Config["secret_env"] != "JWT_SECRET" {
		t.Fatalf("payments.Plugins[0].Config = %+v", payments.Plugins[0].Config)
	}

	orders := cfg.Services[2]
	if orders.Path != "/orders/{id}" {
		t.Fatalf("orders.Path = %q", orders.Path)
	}
	if len(orders.Plugins) != 0 {
		t.Fatalf("orders.Plugins = %+v, want empty", orders.Plugins)
	}
}

func TestParse_MissingRequiredFieldsError(t *testing.T) {
	cases := map[string]string{
		"missing name": `
services:
  - path: "/catalog"
    origin_url: "http://0.0.0.0:3001"
`,
		"missing path": `
services:
  - name: "catalog"
    origin_url: "http://0.0.0.0:3001"
`,
		"missing origin_url": `
services:
  - name: "catalog"
    path: "/catalog"
`,
	}

	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(yaml)); err == nil {
				t.Fatalf("Parse() error = nil, want non-nil")
			}
		})
	}
}

func TestParse_InvalidYAMLErrors(t *testing.T) {
	if _, err := Parse([]byte("not: [valid: yaml")); err == nil {
		t.Fatal("Parse() error = nil, want non-nil for malformed yaml")
	}
}

func TestParse_EmptyPluginsIsFine(t *testing.T) {
	cfg, err := Parse([]byte(`
services:
  - name: "catalog"
    path: "/catalog"
    origin_url: "http://0.0.0.0:3001"
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Services[0].Plugins) != 0 {
		t.Fatalf("Plugins = %+v, want empty", cfg.Services[0].Plugins)
	}
}

func TestLoad_ReadsFileFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(fullExampleYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Services) != 3 {
		t.Fatalf("len(Services) = %d, want 3", len(cfg.Services))
	}
}

func TestLoad_MissingFileErrors(t *testing.T) {
	if _, err := Load("/nonexistent/path/config.yml"); err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
}
