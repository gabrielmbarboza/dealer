// Package plugin defines the gateway's plugin contract and the built-in
// plugins (add_header, http_log, request_size_limiting, jwt_auth).
package plugin

import "net/http"

// Plugin wraps an http.Handler. To mutate a request, modify r and then call
// next.ServeHTTP. To block a request, write the response directly and do
// NOT call next.ServeHTTP.
type Plugin interface {
	Name() string
	Wrap(next http.Handler) http.Handler
}

// Factory builds a Plugin instance from its yaml `config:` map.
type Factory func(cfg map[string]any) (Plugin, error)

// Chain composes plugins in declaration order: plugins[0] runs first and is
// outermost. If every plugin calls next, final runs last.
func Chain(plugins []Plugin, final http.Handler) http.Handler {
	h := final
	for i := len(plugins) - 1; i >= 0; i-- {
		h = plugins[i].Wrap(h)
	}
	return h
}
