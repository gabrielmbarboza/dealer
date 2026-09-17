package plugin

import "fmt"

var builtins = map[string]Factory{
	"add_header":            newAddHeader,
	"http_log":              newHTTPLog,
	"request_size_limiting": newRequestSizeLimiting,
	"jwt_auth":              newJWTAuth,
	"cors":                  newCORS,
}

// Build constructs the named built-in plugin from its yaml config map.
// instanceID identifies this specific plugin declaration, stably across
// config reloads and across gateway processes loading the same config
// (e.g. "service_name:plugin_index" - see gateway.buildMux). Every
// built-in plugin except rate_limiting ignores it; rate_limiting needs it
// to namespace its "distributed" mode counters (see namespacedStore).
func Build(name string, cfg map[string]any, instanceID string) (Plugin, error) {
	if name == "rate_limiting" {
		return newRateLimiting(cfg, instanceID)
	}
	factory, ok := builtins[name]
	if !ok {
		return nil, fmt.Errorf("plugin: unknown plugin %q", name)
	}
	return factory(cfg)
}
