package plugin

import "fmt"

var builtins = map[string]Factory{
	"add_header":            newAddHeader,
	"http_log":              newHTTPLog,
	"request_size_limiting": newRequestSizeLimiting,
	"jwt_auth":              newJWTAuth,
	"rate_limiting":         newRateLimiting,
}

// Build constructs the named built-in plugin from its yaml config map.
func Build(name string, cfg map[string]any) (Plugin, error) {
	factory, ok := builtins[name]
	if !ok {
		return nil, fmt.Errorf("plugin: unknown plugin %q", name)
	}
	return factory(cfg)
}
