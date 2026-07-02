package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse decodes and validates gateway configuration YAML.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse yaml: %w", err)
	}

	for i, svc := range cfg.Services {
		if svc.Name == "" {
			return nil, fmt.Errorf("config: service[%d]: name is required", i)
		}
		if svc.Path == "" {
			return nil, fmt.Errorf("config: service %q: path is required", svc.Name)
		}
		if svc.OriginURL == "" && len(svc.OriginURLs) == 0 {
			return nil, fmt.Errorf("config: service %q: origin_url or origin_urls is required", svc.Name)
		}
		if svc.OriginURL != "" && len(svc.OriginURLs) > 0 {
			return nil, fmt.Errorf("config: service %q: origin_url and origin_urls are mutually exclusive", svc.Name)
		}
		for _, u := range svc.OriginURLs {
			if u == "" {
				return nil, fmt.Errorf("config: service %q: origin_urls entries must not be empty", svc.Name)
			}
		}
	}

	return &cfg, nil
}

// Load reads and parses the gateway configuration file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(data)
}
