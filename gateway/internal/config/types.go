package config

// Config is the root of the gateway configuration file.
type Config struct {
	Services []Service `yaml:"services"`
}

// Service describes one internal service the gateway can forward requests to.
type Service struct {
	Name        string             `yaml:"name"`
	Path        string             `yaml:"path"`
	OriginURL   string             `yaml:"origin_url"`
	OriginURLs  []string           `yaml:"origin_urls"`
	Methods     []string           `yaml:"methods"`
	Plugins     []PluginConfig     `yaml:"plugins"`
	HealthCheck *HealthCheckConfig `yaml:"health_check"`

	// RetryUnsafeMethods allows the gateway's retry-on-transient-failure
	// behavior to also cover non-idempotent methods (POST, PUT, PATCH,
	// DELETE) for this service. Off by default, since retrying a
	// non-idempotent request risks the origin executing it twice.
	RetryUnsafeMethods bool `yaml:"retry_unsafe_methods"`
}

// HealthCheckConfig enables active health probing for a service's
// origin(s). Path and Interval fall back to gateway-wide defaults when
// empty - see gateway.Options.
type HealthCheckConfig struct {
	Path     string `yaml:"path"`
	Interval string `yaml:"interval"`
}

// PluginConfig associates a named plugin with its own free-form configuration.
type PluginConfig struct {
	Name   string         `yaml:"name"`
	Config map[string]any `yaml:"config"`
}
