package config

// Config is the root of the gateway configuration file.
type Config struct {
	Services []Service `yaml:"services"`
}

// Service describes one internal service the gateway can forward requests to.
type Service struct {
	Name       string         `yaml:"name"`
	Path       string         `yaml:"path"`
	OriginURL  string         `yaml:"origin_url"`
	OriginURLs []string       `yaml:"origin_urls"`
	Methods    []string       `yaml:"methods"`
	Plugins    []PluginConfig `yaml:"plugins"`
}

// PluginConfig associates a named plugin with its own free-form configuration.
type PluginConfig struct {
	Name   string         `yaml:"name"`
	Config map[string]any `yaml:"config"`
}
