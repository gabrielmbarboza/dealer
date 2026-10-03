package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// hostPattern is a lowercase DNS host name: dot-separated labels of letters,
// digits and inner hyphens.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// Hosts returns the distinct host names the services are restricted to,
// sorted.
func (c *Config) Hosts() []string {
	seen := map[string]bool{}
	for _, svc := range c.Services {
		if svc.Host != "" {
			seen[svc.Host] = true
		}
	}
	hosts := make([]string, 0, len(seen))
	for host := range seen {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

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
		if svc.Host != "" && !hostPattern.MatchString(svc.Host) {
			return nil, fmt.Errorf("config: service %q: host %q must be a lowercase host name, without scheme, port or path", svc.Name, svc.Host)
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
		if svc.HealthCheck != nil && svc.HealthCheck.Interval != "" {
			if _, err := time.ParseDuration(svc.HealthCheck.Interval); err != nil {
				return nil, fmt.Errorf("config: service %q: health_check.interval: %w", svc.Name, err)
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
