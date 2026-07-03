package plugin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type cors struct {
	allowedOrigins   []string
	allowedMethods   []string
	allowedHeaders   []string
	allowCredentials bool
	maxAge           string
}

func newCORS(cfg map[string]any) (Plugin, error) {
	origins, err := toStringSlice(cfg, "allowed_origins")
	if err != nil {
		return nil, fmt.Errorf("cors: %w", err)
	}
	if len(origins) == 0 {
		return nil, fmt.Errorf("cors: config.allowed_origins is required")
	}

	methods, err := toStringSlice(cfg, "allowed_methods")
	if err != nil {
		return nil, fmt.Errorf("cors: %w", err)
	}

	headers, err := toStringSlice(cfg, "allowed_headers")
	if err != nil {
		return nil, fmt.Errorf("cors: %w", err)
	}

	allowCredentials, _ := cfg["allow_credentials"].(bool)
	if allowCredentials {
		for _, o := range origins {
			if o == "*" {
				return nil, fmt.Errorf("cors: config.allow_credentials cannot be true when config.allowed_origins includes \"*\"")
			}
		}
	}

	var maxAge string
	if raw, ok := cfg["max_age"]; ok {
		seconds, err := toInt64(raw)
		if err != nil {
			return nil, fmt.Errorf("cors: config.max_age: %w", err)
		}
		maxAge = strconv.FormatInt(seconds, 10)
	}

	return &cors{
		allowedOrigins:   origins,
		allowedMethods:   methods,
		allowedHeaders:   headers,
		allowCredentials: allowCredentials,
		maxAge:           maxAge,
	}, nil
}

func (p *cors) Name() string { return "cors" }

func (p *cors) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""

		if origin == "" {
			if preflight {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if p.originAllowed(origin) {
			w.Header().Add("Vary", "Origin")
			if p.allowAllOrigins() {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			if p.allowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if preflight {
				if len(p.allowedMethods) > 0 {
					w.Header().Set("Access-Control-Allow-Methods", strings.Join(p.allowedMethods, ", "))
				}
				if len(p.allowedHeaders) > 0 {
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(p.allowedHeaders, ", "))
				}
				if p.maxAge != "" {
					w.Header().Set("Access-Control-Max-Age", p.maxAge)
				}
			}
		}

		if preflight {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (p *cors) originAllowed(origin string) bool {
	for _, o := range p.allowedOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

func (p *cors) allowAllOrigins() bool {
	for _, o := range p.allowedOrigins {
		if o == "*" {
			return true
		}
	}
	return false
}

// toStringSlice reads cfg[key] as a yaml-decoded list of strings, returning
// (nil, nil) when the key is absent so callers can distinguish "not set"
// from "set but empty".
func toStringSlice(cfg map[string]any, key string) ([]string, error) {
	raw, ok := cfg[key]
	if !ok || raw == nil {
		return nil, nil
	}

	rawSlice, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("config.%s must be a list, got %T", key, raw)
	}

	out := make([]string, 0, len(rawSlice))
	for _, v := range rawSlice {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("config.%s entries must be strings, got %T", key, v)
		}
		out = append(out, s)
	}
	return out, nil
}
